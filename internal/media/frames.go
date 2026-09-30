package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Frame is one frame of a video.
type Frame struct {
	At int64 // ms from the start
	Image
}

// MaxSamples is the most frames of one video the AI looks at.
const MaxSamples = 24

// SampleCount is how many frames of a video the AI looks at: one every five
// seconds, at most MaxSamples (so a long video is sampled more sparsely).
func SampleCount(duration int64) int { return int(max(1, min(MaxSamples, (duration+4999)/5000))) }

const (
	// framesTimeout bounds one pass over a video's keyframes (a whole phone
	// video takes about a second).
	framesTimeout = 2 * time.Minute
	// fullPassTimeout bounds decoding every frame of a video that has almost
	// no keyframes.
	fullPassTimeout = 5 * time.Minute
)

// Frames decodes up to n frames spread evenly over a video, upright, each at
// most side on its long edge (never enlarged), and hands them to fn one at
// a time, in order. Only keyframes are decoded, which makes it fast, so each
// frame is the first keyframe at or after its point in the video. A video
// with a single keyframe (some screen recorders write one) is decoded in
// full instead.
func (d Decoders) Frames(ctx context.Context, path string, n, side int, fn func(Frame) error) error {
	_, err := d.frames(ctx, path, n, side, fn)
	return err
}

// frames is Frames, also returning what ffprobe said about the video.
func (d Decoders) frames(ctx context.Context, path string, n, side int, fn func(Frame) error) (videoInfo, error) {
	if d.FFmpeg == "" {
		return videoInfo{}, ErrUnsupported
	}
	v, err := d.probe(ctx, path)
	if err != nil {
		return v, err
	}
	every := math.Inf(1) // unknown length: just the first frame
	if v.Duration > 0 {
		every = float64(v.Duration) / 1000 / float64(max(n, 1))
	}
	got, err := d.samplePass(ctx, path, v, side, every, true, false, fn)
	if err == nil && v.Duration > 0 && (got == 0 || (got == 1 && n > 1)) {
		_, err = d.samplePass(ctx, path, v, side, every, false, got == 1, fn)
	}
	return v, err
}

// VideoFramesVersion versions how Frames samples a video (SampleCount, the
// passes below): both AI stages' versions end with it, so a change re-runs
// them for videos.
const VideoFramesVersion = "+video-frames-2"

// samplePass runs ffmpeg once, selecting a frame every `every` seconds:
// keyframes only, or every frame decoded, from the start or, with
// skipFirst (a keyframe pass found just the first), from the second sample
// on. It returns how many frames it handed to fn.
func (d Decoders) samplePass(ctx context.Context, path string, v videoInfo, side int, every float64, keyframes, skipFirst bool,
	fn func(Frame) error) (int, error) {
	sel := fmt.Sprintf("isnan(prev_selected_t)+gte(t-prev_selected_t,%.3f)", every)
	timeout := framesTimeout
	var args []string
	if keyframes {
		args = append(args, "-skip_frame", "nokey")
	} else {
		if skipFirst {
			sel = fmt.Sprintf("gte(t,%.3f)*(%s)", v.start+every, sel) // t counts from the stream's start time
		}
		timeout = fullPassTimeout
	}
	if math.IsInf(every, 1) {
		sel = "isnan(prev_selected_t)"
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// "level" tags each log line with its severity, so the frame times
	// (showinfo, at info level) and errors can be told apart.
	args = append([]string{"-v", "level+info", "-nostats", "-hide_banner", "-nostdin"}, args...)
	args = append(args, "-i", path, "-map", "0:V:0", "-an", "-sn", "-dn",
		"-vf", "select='"+sel+"',showinfo,"+fitFilter(v, side, false),
		"-fps_mode", "passthrough", "-f", "image2pipe", "-c:v", "ppm", "-pix_fmt", "rgb24", "-")

	cmd := exec.CommandContext(ctx, d.FFmpeg, args...)
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("ffmpeg: %w", err)
	}
	log := readLog(stderr)
	got, err := readFrames(stdout, log, v, fn)
	if err != nil {
		cancel() // stop ffmpeg; its output is no longer read
	}
	_, _ = io.Copy(io.Discard, stdout) // let ffmpeg finish writing, so Wait returns
	<-log.finished                     // Wait closes the log pipe: read it to the end first
	if werr := toolError(ctx, d.FFmpeg, cmd.Wait(), log.errors()); err == nil {
		err = werr
	}
	return got, err
}

// readFrames reads the PPM frames ffmpeg writes, pairing each with its time
// from the log, and hands them to fn.
func readFrames(r io.Reader, log *ffmpegLog, v videoInfo, fn func(Frame) error) (int, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	for n := 0; ; n++ {
		pix, w, h, err := readPPM(br)
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		at := int64(-1)
		if t, ok := log.time(n); ok {
			at = max(0, int64(math.Round((t-v.start)*1000)))
		}
		if at < 0 {
			return n, errors.New("ffmpeg: frame without a time")
		}
		if err := fn(Frame{At: at, Image: Image{Pix: pix, W: w, H: h}}); err != nil {
			return n, err
		}
	}
}

// ffmpegLog collects what ffmpeg prints while it samples frames: the time
// of each frame it outputs (showinfo's pts_time), and its errors.
type ffmpegLog struct {
	mu       sync.Mutex
	ready    *sync.Cond
	times    []float64
	errs     []string
	done     bool
	finished chan struct{} // closed once the log is read to its end
}

var ptsTime = regexp.MustCompile(`\bpts_time:(-?[0-9.]+)`)

// maxLogLine is the longest ffmpeg log line read: longer ones end the log.
const maxLogLine = 4 << 20

// readLog starts collecting ffmpeg's log from r.
func readLog(r io.Reader) *ffmpegLog {
	l := &ffmpegLog{finished: make(chan struct{})}
	l.ready = sync.NewCond(&l.mu)
	go func() {
		defer close(l.finished)
		sc := bufio.NewScanner(r)
		// ffmpeg prints the file's tags at info level, and one can be long.
		sc.Buffer(make([]byte, 0, 64<<10), maxLogLine)
		for sc.Scan() {
			line := sc.Text()
			l.mu.Lock()
			if m := ptsTime.FindStringSubmatch(line); m != nil && strings.Contains(line, "[info]") {
				if t, err := strconv.ParseFloat(m[1], 64); err == nil {
					l.times = append(l.times, t)
				}
			} else if isErrorLine(line) && len(l.errs) < 8 {
				l.errs = append(l.errs, line)
			}
			l.mu.Unlock()
			l.ready.Broadcast()
		}
		_, _ = io.Copy(io.Discard, r) // after an overlong line: ffmpeg must never block on a full pipe
		l.mu.Lock()
		l.done = true
		l.mu.Unlock()
		l.ready.Broadcast()
	}()
	return l
}

func isErrorLine(line string) bool {
	return strings.Contains(line, "[error]") || strings.Contains(line, "[fatal]") || strings.Contains(line, "[panic]")
}

// time waits for the time of output frame n. ok is false if ffmpeg ended
// without logging it.
func (l *ffmpegLog) time(n int) (float64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for len(l.times) <= n && !l.done {
		l.ready.Wait()
	}
	if n < len(l.times) {
		return l.times[n], true
	}
	return 0, false
}

// errors is what ffmpeg reported as errors, without its level tags.
func (l *ffmpegLog) errors() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	for _, e := range l.errs {
		for _, tag := range []string{"[error] ", "[fatal] ", "[panic] "} {
			if i := strings.Index(e, tag); i >= 0 {
				e = e[i+len(tag):]
			}
		}
		b.WriteString(e + "\n")
	}
	return b.String()
}

// FrameAt decodes the video frame at ms (exactly, not the nearest keyframe),
// upright, at most side on its long edge.
func (d Decoders) FrameAt(ctx context.Context, path string, at int64, side int) (Image, error) {
	if d.FFmpeg == "" {
		return Image{}, ErrUnsupported
	}
	v, err := d.probe(ctx, path)
	if err != nil {
		return Image{}, err
	}
	out, err := runFFmpeg(ctx, d.FFmpeg, "-ss", ssTime(at), "-i", path,
		"-map", "0:V:0", "-vf", fitFilter(v, side, false))
	if err != nil {
		return Image{}, err
	}
	pix, w, h, err := parsePPM(out)
	return Image{Pix: pix, W: w, H: h}, err
}

// hdrToSDR tone-maps HDR (PQ or HLG) to ordinary sRGB-like BT.709, so
// frames of HDR phone videos don't come out washed out.
const hdrToSDR = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0," +
	"zscale=t=bt709:m=bt709:r=tv,format=yuv420p"

// fitFilter is the filter chain after ffmpeg has turned frames upright: it
// scales them to at most side on their long edge (never enlarged) with
// square pixels, and tone-maps HDR. even rounds the size to even numbers,
// as H.264 needs.
func fitFilter(v videoInfo, side int, even bool) string {
	var f string
	if v.Width > 0 && v.Height > 0 {
		s := min(1, float64(side)/float64(max(v.Width, v.Height)))
		w, h := max(2, int(math.Round(float64(v.Width)*s))), max(2, int(math.Round(float64(v.Height)*s)))
		if even {
			w, h = w&^1, h&^1
		}
		f = fmt.Sprintf("scale=%d:%d,setsar=1", w, h)
	} else {
		f = fmt.Sprintf("scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1",
			side, side)
	}
	if v.HDR {
		f += "," + hdrToSDR
	}
	return f
}

// readPPM reads one binary PPM (P6, 8-bit), the format ffmpeg writes frames
// in. It returns io.EOF when r holds no more images.
func readPPM(r *bufio.Reader) (pix []byte, w, h int, err error) {
	magic := make([]byte, 2)
	if _, err := io.ReadFull(r, magic); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, 0, 0, io.EOF
		}
		return nil, 0, 0, errors.New("ffmpeg: truncated PPM")
	}
	if !bytes.Equal(magic, []byte("P6")) {
		return nil, 0, 0, errors.New("ffmpeg: output is not a PPM image")
	}
	var fields [3]int
	for i := range fields {
		c, err := r.ReadByte()
		for err == nil && isSpace(c) {
			c, err = r.ReadByte()
		}
		n, digits := 0, 0
		for err == nil && c >= '0' && c <= '9' && n < 1<<24 {
			n, digits = n*10+int(c-'0'), digits+1
			c, err = r.ReadByte()
		}
		if err != nil || digits == 0 || !isSpace(c) {
			return nil, 0, 0, errors.New("ffmpeg: malformed PPM header")
		}
		fields[i] = n // the single whitespace after the last field was just read
	}
	w, h = fields[0], fields[1]
	if fields[2] != 255 || w <= 0 || h <= 0 || w*h > maxPixels {
		return nil, 0, 0, fmt.Errorf("ffmpeg: unsupported PPM (%dx%d, max %d)", w, h, fields[2])
	}
	pix = make([]byte, w*h*3)
	if _, err := io.ReadFull(r, pix); err != nil {
		return nil, 0, 0, fmt.Errorf("ffmpeg: PPM is %dx%d but its pixels are cut short", w, h)
	}
	return pix, w, h, nil
}

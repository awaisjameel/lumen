package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"lumen/internal/core"
)

// Videos are read with the bundled FFmpeg: ffprobe for what a file holds,
// ffmpeg for its frames (frames.go) and for converting it while it plays
// (transcode.go). Every run is a separate process, so a file that crashes
// FFmpeg can't take Lumen with it.

// probeTimeout bounds one ffprobe run.
const probeTimeout = 30 * time.Second

// videoInfo is what ffprobe says about a video.
type videoInfo struct {
	Duration      int64 // ms; 0 if unknown
	Width, Height int   // as displayed: rotation and pixel shape applied
	Codec         string
	Audio         string // "" = no sound
	HDR           bool   // PQ or HLG: frames are tone-mapped to show right
	TakenAt       time.Time
	Camera        string
	Lat, Lon      *float64
}

// ffprobeOutput is the part of ffprobe's JSON Lumen reads.
type ffprobeOutput struct {
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		CodecName   string            `json:"codec_name"`
		Width       int               `json:"width"`
		Height      int               `json:"height"`
		SAR         string            `json:"sample_aspect_ratio"`
		Transfer    string            `json:"color_transfer"`
		Duration    string            `json:"duration"`
		Disposition map[string]int    `json:"disposition"`
		Tags        map[string]string `json:"tags"`
		SideData    []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

// probe reads a video's streams and tags with ffprobe.
func (d Decoders) probe(ctx context.Context, path string) (videoInfo, error) {
	if d.FFprobe == "" {
		return videoInfo{}, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var out bytes.Buffer
	err := runTool(ctx, d.FFprobe, &out, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	if err != nil {
		return videoInfo{}, err
	}
	var p ffprobeOutput
	if err := json.Unmarshal(out.Bytes(), &p); err != nil {
		return videoInfo{}, fmt.Errorf("ffprobe: %w", err)
	}
	return parseProbe(p, time.Now())
}

// parseProbe turns ffprobe's output into videoInfo. A file without a
// picture (sound only, or not a video at all) is ErrUnsupported. now bounds
// plausible capture dates.
func parseProbe(p ffprobeOutput, now time.Time) (videoInfo, error) {
	var v videoInfo
	video, audio := -1, -1
	for i, s := range p.Streams {
		switch {
		case s.CodecType == "video" && s.Disposition["attached_pic"] == 0 && video < 0:
			video = i
		case s.CodecType == "audio" && audio < 0:
			audio = i
		}
	}
	if video < 0 {
		return v, ErrUnsupported
	}
	s := p.Streams[video]
	v.Codec = s.CodecName
	if audio >= 0 {
		v.Audio = p.Streams[audio].CodecName
	}
	v.HDR = s.Transfer == "smpte2084" || s.Transfer == "arib-std-b67"
	v.Width, v.Height = s.Width, s.Height
	if num, den, ok := ratio(s.SAR); ok && num != den {
		v.Width = int(math.Round(float64(v.Width) * float64(num) / float64(den)))
	}
	rotation := 0.0
	for _, sd := range s.SideData {
		if sd.Rotation != 0 {
			rotation = sd.Rotation
		}
	}
	if r, err := strconv.ParseFloat(lowerKeys(s.Tags)["rotate"], 64); err == nil && rotation == 0 {
		rotation = r
	}
	if int(math.Abs(math.Round(rotation)))%180 == 90 {
		v.Width, v.Height = v.Height, v.Width
	}

	v.Duration = seconds(p.Format.Duration)
	if v.Duration == 0 {
		v.Duration = seconds(s.Duration)
	}
	if v.Duration == 0 { // Matroska keeps it in a tag
		v.Duration = clockDuration(lowerKeys(s.Tags)["duration"])
	}

	tags, streamTags := lowerKeys(p.Format.Tags), lowerKeys(s.Tags)
	v.TakenAt = captureTime(tags, streamTags, now)
	v.Camera = cameraName(first(tags, "com.apple.quicktime.make", "com.android.manufacturer", "make"),
		first(tags, "com.apple.quicktime.model", "com.android.model", "model"))
	if lat, lon, ok := iso6709(first(tags, "com.apple.quicktime.location.iso6709", "location", "location-eng")); ok {
		v.Lat, v.Lon = &lat, &lon
	}
	return v, nil
}

// captureTime is when the video was recorded, as its local clock read. Apple
// writes the local time with its offset; everyone else writes UTC, which is
// read as this computer's local time (the best guess without a time zone).
// Implausible dates (unset clocks, see core.PlausibleAt) are ignored.
func captureTime(format, stream map[string]string, now time.Time) time.Time {
	plausible := func(t time.Time) bool { return core.PlausibleAt(t, now) }
	if t, err := time.Parse("2006-01-02T15:04:05-0700", format["com.apple.quicktime.creationdate"]); err == nil && plausible(t) {
		return t
	}
	for _, tags := range []map[string]string{format, stream} {
		if t, err := time.Parse(time.RFC3339Nano, tags["creation_time"]); err == nil && plausible(t) {
			return t.Local()
		}
	}
	return time.Time{}
}

var iso6709Re = regexp.MustCompile(`^([+-]\d+(?:\.\d+)?)([+-]\d+(?:\.\d+)?)`)

// iso6709 reads the latitude and longitude from an ISO 6709 location such as
// "+29.6075+073.1398/" or "+37.3349-122.0090+010.000/".
func iso6709(s string) (lat, lon float64, ok bool) {
	m := iso6709Re.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	lat, err1 := strconv.ParseFloat(m[1], 64)
	lon, err2 := strconv.ParseFloat(m[2], 64)
	return lat, lon, err1 == nil && err2 == nil && validGPS(lat, lon)
}

// ratio parses "num:den" with both parts positive.
func ratio(s string) (num, den int, ok bool) {
	a, b, found := strings.Cut(s, ":")
	num, err1 := strconv.Atoi(a)
	den, err2 := strconv.Atoi(b)
	return num, den, found && err1 == nil && err2 == nil && num > 0 && den > 0
}

// seconds parses a duration in seconds ("53.653333") as ms, 0 if it isn't one.
func seconds(s string) int64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) || f > float64(1<<63-1)/1000 {
		return 0
	}
	return int64(math.Round(f * 1000))
}

// clockDuration parses "HH:MM:SS.fraction" as ms, 0 if it isn't one.
func clockDuration(s string) int64 {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0
	}
	return int64(h)*3_600_000 + int64(m)*60_000 + seconds(parts[2])
}

// lowerKeys copies tags with lower-case keys: containers disagree on case.
func lowerKeys(tags map[string]string) map[string]string {
	out := make(map[string]string, len(tags))
	for k, v := range tags {
		out[strings.ToLower(k)] = strings.TrimSpace(v)
	}
	return out
}

func first(tags map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := tags[k]; v != "" {
			return v
		}
	}
	return ""
}

// runTool runs one of the bundled FFmpeg programs without a console window,
// in the background (see hideWindow), writing its standard output to stdout.
// A failure carries the first line of what the program printed about it.
func runTool(ctx context.Context, exe string, stdout io.Writer, args ...string) error {
	return runToolAs(ctx, hideWindow, exe, stdout, args...)
}

// runToolAs is runTool, with start setting how the program runs.
func runToolAs(ctx context.Context, start func(*exec.Cmd), exe string, stdout io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, exe, args...)
	start(cmd)
	stderr := &headWriter{max: 64 << 10} // a damaged file can make a long run print without end
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return toolError(ctx, exe, cmd.Run(), stderr.String())
}

// headWriter keeps the first max bytes written to it and drops the rest.
type headWriter struct {
	bytes.Buffer
	max int
}

func (w *headWriter) Write(p []byte) (int, error) {
	if room := w.max - w.Len(); room > 0 {
		w.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// toolError describes a finished run of an FFmpeg program: nil if it
// succeeded, ctx's error if that ended it, else what the program said went
// wrong.
func toolError(ctx context.Context, exe string, err error, stderr string) error {
	name := strings.TrimSuffix(filepath.Base(exe), ".exe")
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return fmt.Errorf("%s: %w", name, ctx.Err())
	}
	if msg := strings.TrimSpace(stderr); msg != "" {
		return fmt.Errorf("%s: %s", name, firstLine(msg))
	}
	return fmt.Errorf("%s: %w", name, err)
}

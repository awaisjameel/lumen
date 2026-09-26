package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ffmpegTimeout bounds one decode, so a pathological file can't stall a worker.
const ffmpegTimeout = time.Minute

// ffmpegDecode decodes an image with FFmpeg into 8-bit RGB, scaled so its
// long edge is at most maxSide (never upscaled). Rotation is not applied.
func ffmpegDecode(exe, path string, maxSide int) (pix []byte, w, h int, err error) {
	scale := fmt.Sprintf("scale='min(iw,%d)':'min(ih,%d)':force_original_aspect_ratio=decrease", maxSide, maxSide)
	// iPhone photos are a grid of tiles that FFmpeg assembles as a stream
	// group; other files are a single stream. Try the grid first.
	out, err := runFFmpeg(exe, "-noautorotate", "-i", path, "-filter_complex", "[0:g:0]"+scale+"[o]", "-map", "[o]")
	if err != nil {
		out, err = runFFmpeg(exe, "-noautorotate", "-i", path, "-map", "0:v:0", "-vf", scale)
	}
	if err != nil {
		return nil, 0, 0, err
	}
	return parsePPM(out)
}

// runFFmpeg runs ffmpeg with args plus "write one frame as PPM to stdout".
func runFFmpeg(exe string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ffmpegTimeout)
	defer cancel()
	args = append([]string{"-v", "error", "-nostdin", "-hide_banner"}, args...)
	args = append(args, "-frames:v", "1", "-f", "image2pipe", "-c:v", "ppm", "-pix_fmt", "rgb24", "-")
	cmd := exec.CommandContext(ctx, exe, args...)
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("ffmpeg: %s", firstLine(msg))
		}
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	return stdout.Bytes(), nil
}

// parsePPM reads a binary PPM (P6, 8-bit), the format FFmpeg writes above.
func parsePPM(b []byte) (pix []byte, w, h int, err error) {
	var fields []int
	i := 2
	if !bytes.HasPrefix(b, []byte("P6")) {
		return nil, 0, 0, errors.New("ffmpeg: output is not a PPM image")
	}
	for len(fields) < 3 {
		for i < len(b) && isSpace(b[i]) {
			i++
		}
		j := i
		for j < len(b) && b[j] >= '0' && b[j] <= '9' {
			j++
		}
		n, err := strconv.Atoi(string(b[i:j]))
		if err != nil {
			return nil, 0, 0, errors.New("ffmpeg: malformed PPM header")
		}
		fields = append(fields, n)
		i = j
	}
	w, h = fields[0], fields[1]
	if fields[2] != 255 || i >= len(b) || !isSpace(b[i]) {
		return nil, 0, 0, errors.New("ffmpeg: unsupported PPM")
	}
	pix = b[i+1:]
	if w <= 0 || h <= 0 || len(pix) != w*h*3 {
		return nil, 0, 0, fmt.Errorf("ffmpeg: PPM is %dx%d but holds %d bytes", w, h, len(pix))
	}
	return pix, w, h, nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\r' || c == '\t' }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

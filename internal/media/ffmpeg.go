package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ffmpegTimeout bounds one decode, so a pathological file can't stall a worker.
const ffmpegTimeout = time.Minute

// ffmpegDecode decodes an image with FFmpeg into 8-bit RGB, scaled so its
// long edge is at most maxSide (never upscaled). Rotation is not applied.
func ffmpegDecode(ctx context.Context, exe, path string, maxSide int) (pix []byte, w, h int, err error) {
	scale := fmt.Sprintf("scale='min(iw,%d)':'min(ih,%d)':force_original_aspect_ratio=decrease", maxSide, maxSide)
	// iPhone photos are a grid of tiles that FFmpeg assembles as a stream
	// group; other files are a single stream. Try the grid first.
	out, err := runFFmpeg(ctx, exe, "-noautorotate", "-i", path, "-filter_complex", "[0:g:0]"+scale+"[o]", "-map", "[o]")
	if err != nil {
		out, err = runFFmpeg(ctx, exe, "-noautorotate", "-i", path, "-map", "0:v:0", "-vf", scale)
	}
	if err != nil {
		return nil, 0, 0, err
	}
	return parsePPM(out)
}

// runFFmpeg runs ffmpeg with args plus "write one frame as PPM to stdout".
func runFFmpeg(ctx context.Context, exe string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()
	args = append([]string{"-v", "error", "-nostdin", "-hide_banner"}, args...)
	args = append(args, "-frames:v", "1", "-f", "image2pipe", "-c:v", "ppm", "-pix_fmt", "rgb24", "-")
	var stdout bytes.Buffer
	if err := runTool(ctx, exe, &stdout, args...); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// parsePPM reads a buffer holding exactly one PPM image.
func parsePPM(b []byte) (pix []byte, w, h int, err error) {
	r := bufio.NewReader(bytes.NewReader(b))
	pix, w, h, err = readPPM(r)
	switch {
	case errors.Is(err, io.EOF):
		return nil, 0, 0, errors.New("ffmpeg: no image")
	case err != nil:
		return nil, 0, 0, err
	}
	if _, err := r.ReadByte(); err == nil {
		return nil, 0, 0, errors.New("ffmpeg: more than one image")
	}
	return pix, w, h, nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\r' || c == '\t' }

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

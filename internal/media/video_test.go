package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"lumen/internal/core"
	"lumen/internal/testenv"
)

// Exercise the bundled FFmpeg build, not a system installation: this catches
// missing encoders, frame-time parsing and sprite output together.
func TestVideoPipeline(t *testing.T) {
	ffmpeg := testenv.File(t, "ffmpeg", "ffmpeg.exe")
	ffprobe := testenv.File(t, "ffmpeg", "ffprobe.exe")
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "sample.mp4")
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i",
		"testsrc2=size=320x180:rate=5", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "8", "-c:v", "libopenh264", "-b:v", "500k", "-g", "10", "-c:a", "aac", src)
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make sample: %v: %s", err, out)
	}
	d := Decoders{FFmpeg: ffmpeg, FFprobe: ffprobe}
	m, err := d.Metadata(ctx, src, core.KindVideo)
	if err != nil || m.Width != 320 || m.Height != 180 || m.Duration != 8000 || m.VideoCodec != "h264" || m.AudioCodec != "aac" {
		t.Fatalf("metadata: %+v, %v", m, err)
	}
	var frames []Frame
	if err := d.Frames(ctx, src, 4, 320, func(f Frame) error { frames = append(frames, f); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(frames) < 3 || len(frames) > 4 {
		t.Fatalf("frames: got %d, want 3-4", len(frames))
	}
	for i, f := range frames {
		if f.W != 320 || f.H != 180 || (i > 0 && f.At <= frames[i-1].At) {
			t.Fatalf("frame %d: %dx%d at %d", i, f.W, f.H, f.At)
		}
	}
	stop := errors.New("consumer stopped")
	calls := 0
	if err := d.Frames(ctx, src, 4, 320, func(Frame) error { calls++; return stop }); !errors.Is(err, stop) || !strings.Contains(err.Error(), src) || calls != 1 {
		t.Fatalf("stopped consumer: %d calls, %v", calls, err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	calls = 0
	if err := d.Frames(cancelled, src, 4, 320, func(Frame) error { calls++; cancel(); return nil }); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancelled sampler: %d calls, %v", calls, err)
	}
	cancel()
	thumb, err := d.Thumbnail(ctx, src, core.KindVideo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(thumb.Small) == 0 || len(thumb.Large) == 0 || len(thumb.Sprite) == 0 {
		t.Fatal("missing video poster or sprite")
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(thumb.Sprite)); err != nil || cfg.Width == 0 || cfg.Height == 0 {
		t.Fatalf("sprite JPEG: %+v, %v", cfg, err)
	}
	frame, err := d.FrameAt(ctx, src, 3000, 320)
	if err != nil || frame.W != 320 || frame.H != 180 {
		t.Fatalf("frame at 3s: %+v, %v", frame, err)
	}
	convert, err := d.ConvertMP4(ctx, src, 2000)
	if err != nil {
		t.Fatal(err)
	}
	var converted bytes.Buffer
	if err := convert(&converted); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ConvertMP4(ctx, filepath.Join(t.TempDir(), "missing.mp4"), 0); err == nil {
		t.Fatal("converting a missing video did not fail before writing")
	}
	if converted.Len() == 0 {
		t.Fatal("empty converted video")
	}
	out := filepath.Join(t.TempDir(), "converted.mp4")
	if err := os.WriteFile(out, converted.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	convertedMeta, err := d.Metadata(ctx, out, core.KindVideo)
	if err != nil || convertedMeta.VideoCodec != "h264" || convertedMeta.AudioCodec != "aac" {
		t.Fatalf("converted metadata: %+v, %v", convertedMeta, err)
	}
}

func TestVideoFrameTimesAreRelativeToPlaybackStart(t *testing.T) {
	ffmpeg := testenv.File(t, "ffmpeg", "ffmpeg.exe")
	ffprobe := testenv.File(t, "ffmpeg", "ffprobe.exe")
	for _, interval := range []string{"5", "300"} {
		t.Run(interval, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "offset.ts")
			cmd := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-f", "lavfi", "-i",
				"testsrc2=size=64x48:rate=5", "-t", "6", "-c:v", "mpeg2video", "-g", interval, "-f", "mpegts", path)
			hideWindow(cmd)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("make offset sample: %v: %s", err, out)
			}
			d := Decoders{FFmpeg: ffmpeg, FFprobe: ffprobe}
			var out bytes.Buffer
			if err := runTool(t.Context(), ffprobe, &out, "-v", "error", "-show_entries", "format=start_time", "-of", "default=nw=1:nk=1", path); err != nil {
				t.Fatal(err)
			}
			if start, err := strconv.ParseFloat(strings.TrimSpace(out.String()), 64); err != nil || start <= 0 {
				t.Fatalf("offset sample: start %v, %v", start, err)
			}
			var frames []Frame
			if err := d.Frames(t.Context(), path, 3, 64, func(f Frame) error { frames = append(frames, f); return nil }); err != nil {
				t.Fatal(err)
			}
			if len(frames) != 3 || frames[0].At != 0 || frames[1].At != 2000 || frames[2].At != 4000 {
				t.Fatalf("offset video frame times: %v", frameTimes(frames))
			}
		})
	}
}

func TestLongVideosAreSampledBySeekingToEachPoint(t *testing.T) {
	ffmpeg := testenv.File(t, "ffmpeg", "ffmpeg.exe")
	ffprobe := testenv.File(t, "ffmpeg", "ffprobe.exe")
	d := Decoders{FFmpeg: ffmpeg, FFprobe: ffprobe}
	for _, tc := range []struct {
		name      string
		seconds   string
		keyframes string // frames between keyframes, at 5 a second
		format    string
		want      []int64
	}{
		// Three points 66.7 s apart: seeking in an indexed file takes the
		// keyframe at or before each (a pass over the keyframes would take
		// 70 s and 140 s).
		{"keyframes every 10 s", "200", "50", "matroska", []int64{0, 60_000, 130_000}},
		// Every seek finds the one keyframe: the rest are decoded in full.
		// MPEG-TS starts its clock at 1.4 s, which the times leave out.
		{"one keyframe", "100", "600", "mpegts", []int64{0, 33_400, 66_800}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "long")
			cmd := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-f", "lavfi", "-i",
				"testsrc2=size=64x48:rate=5", "-t", tc.seconds, "-c:v", "mpeg4", "-g", tc.keyframes, "-f", tc.format, path)
			hideWindow(cmd)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("make long sample: %v: %s", err, out)
			}
			var frames []Frame
			if err := d.Frames(t.Context(), path, 3, 64, func(f Frame) error { frames = append(frames, f); return nil }); err != nil {
				t.Fatal(err)
			}
			if got := frameTimes(frames); !slices.Equal(got, tc.want) {
				t.Fatalf("frame times %v, want %v", got, tc.want)
			}
		})
	}
}

// A recording streamed to disk as it was made (a browser's MediaRecorder)
// doesn't say how long it is: there is nothing to seek to, only the start.
func TestVideoOfUnknownLengthGivesItsFirstFrame(t *testing.T) {
	ffmpeg := testenv.File(t, "ffmpeg", "ffmpeg.exe")
	ffprobe := testenv.File(t, "ffmpeg", "ffprobe.exe")
	d := Decoders{FFmpeg: ffmpeg, FFprobe: ffprobe}
	path := filepath.Join(t.TempDir(), "recording.mkv")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	cmd := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-f", "lavfi", "-i",
		"testsrc2=size=64x48:rate=5", "-t", "4", "-c:v", "mpeg4", "-g", "5", "-f", "matroska", "-live", "1", "pipe:1")
	cmd.Stdout = out
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		t.Fatalf("make recording: %v", err)
	}
	if v, err := d.probe(t.Context(), path); err != nil || v.Duration != 0 {
		t.Fatalf("probe = %+v, %v; want no length", v, err)
	}
	var frames []Frame
	if err := d.Frames(t.Context(), path, 3, 64, func(f Frame) error { frames = append(frames, f); return nil }); err != nil {
		t.Fatal(err)
	}
	if got := frameTimes(frames); !slices.Equal(got, []int64{0}) {
		t.Fatalf("frame times %v, want the first", got)
	}
}

func frameTimes(frames []Frame) []int64 {
	times := make([]int64, len(frames))
	for i, f := range frames {
		times[i] = f.At
	}
	return times
}

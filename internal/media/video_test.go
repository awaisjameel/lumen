package media

import (
	"bytes"
	"context"
	"image"
	"os"
	"os/exec"
	"path/filepath"
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

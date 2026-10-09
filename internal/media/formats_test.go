package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"os/exec"
	"path/filepath"
	"testing"

	"lumen/internal/core"
	"lumen/internal/testenv"
)

// makeSample has the bundled FFmpeg write a test picture or clip.
func makeSample(t *testing.T, ffmpeg, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	cmd := exec.CommandContext(t.Context(), ffmpeg, append(append([]string{"-v", "error", "-y"}, args...), path)...)
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make %s: %v: %s", name, err, out)
	}
	return path
}

// Each container is accepted with the codecs such files really hold, read by
// the bundled FFmpeg: its size and length, a poster and sprite, and frames
// at the times asked for. None plays in the WebView as it is.
func TestTheNewerVideoContainersAreIndexedAndConverted(t *testing.T) {
	ffmpeg := testenv.File(t, "ffmpeg", "ffmpeg.exe")
	d := Decoders{FFmpeg: ffmpeg, FFprobe: testenv.File(t, "ffmpeg", "ffprobe.exe")}
	picture := []string{"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=25", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "4"}
	for _, c := range []struct {
		name, video, audio string
		codecs             []string
	}{
		{"film.mpg", "mpeg1video", "mp2", []string{"-c:v", "mpeg1video", "-c:a", "mp2"}},
		{"dvd.mpeg", "mpeg2video", "mp2", []string{"-c:v", "mpeg2video", "-c:a", "mp2", "-f", "vob"}},
		{"recording.ts", "mpeg2video", "mp2", []string{"-c:v", "mpeg2video", "-c:a", "mp2", "-f", "mpegts"}},
		{"clip.ogv", "theora", "vorbis", []string{"-c:v", "libtheora", "-q:v", "6", "-c:a", "libvorbis"}},
		{"old.asf", "wmv2", "wmav2", []string{"-c:v", "wmv2", "-c:a", "wmav2"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := t.Context()
			path := makeSample(t, ffmpeg, c.name, append(picture, c.codecs...)...)
			if core.KindOf(path) != core.KindVideo {
				t.Fatalf("%s is not taken for a video", c.name)
			}
			m, err := d.Metadata(ctx, path, core.KindVideo)
			if err != nil || m.Width != 160 || m.Height != 120 || m.VideoCodec != c.video || m.AudioCodec != c.audio {
				t.Fatalf("metadata: %+v, %v", m, err)
			}
			if m.Duration < 3500 || m.Duration > 4500 {
				t.Errorf("duration = %d ms, want about 4000", m.Duration)
			}
			if play := core.PlaybackOf(filepath.Ext(path), m.VideoCodec, m.AudioCodec); play != core.PlayConvert {
				t.Errorf("playback = %s, want convert", play)
			}
			th, err := d.Thumbnail(ctx, path, core.KindVideo, 1)
			if err != nil {
				t.Fatal(err)
			}
			if cfg, _, err := image.DecodeConfig(bytes.NewReader(th.Large)); err != nil || cfg.Width != 160 || cfg.Height != 120 || len(th.Sprite) == 0 {
				t.Errorf("poster %dx%d, sprite %d bytes, %v", cfg.Width, cfg.Height, len(th.Sprite), err)
			}
			// Seeking lands where it was asked to: the frame two seconds in.
			if im, err := d.FrameAt(ctx, path, 2000, 160); err != nil || im.W != 160 || im.H != 120 {
				t.Errorf("frame at 2 s: %dx%d, %v", im.W, im.H, err)
			}
		})
	}
}

func TestJPEGXLIsDecodedByTheBundledCodec(t *testing.T) {
	ffmpeg := testenv.File(t, "ffmpeg", "ffmpeg.exe")
	d := Decoders{FFmpeg: ffmpeg, FFprobe: testenv.File(t, "ffmpeg", "ffprobe.exe"), JXL: testenv.File(t, "compress", "djxl.exe")}
	ctx := context.Background()
	// Red on the left, blue on the right, 300x200, lossless.
	path := makeSample(t, ffmpeg, "picture.jxl", "-f", "lavfi", "-i", "color=red:size=150x200,format=rgb24", "-f", "lavfi", "-i",
		"color=blue:size=150x200,format=rgb24", "-filter_complex", "hstack", "-frames:v", "1", "-c:v", "libjxl", "-distance", "0")

	m, err := d.Metadata(ctx, path, core.KindPhoto)
	if err != nil || m.Width != 300 || m.Height != 200 || m.Orientation != 1 {
		t.Fatalf("metadata: %+v, %v", m, err)
	}
	im, err := d.Decode(ctx, path, core.KindPhoto, m.Orientation, 150)
	if err != nil {
		t.Fatal(err)
	}
	if im.W != 150 || im.H != 100 {
		t.Fatalf("decoded %dx%d, want 150x100", im.W, im.H)
	}
	if r, _, b := im.at(2, 5); r < 200 || b > 50 {
		t.Errorf("left is not red: r=%d b=%d", r, b)
	}
	if r, _, b := im.at(8, 5); b < 200 || r > 50 {
		t.Errorf("right is not blue: r=%d b=%d", r, b)
	}
	th, err := d.Thumbnail(ctx, path, core.KindPhoto, m.Orientation)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(th.Large)); err != nil || cfg.Width != 300 || cfg.Height != 200 {
		t.Errorf("thumbnail %dx%d, %v", cfg.Width, cfg.Height, err)
	}
	if core.WebNative(".jxl") {
		t.Error("the WebView is offered a JPEG XL it cannot show")
	}
	// Without its native decoder it is skipped, like a HEIC nothing here decodes.
	if _, err := (Decoders{}).Decode(ctx, path, core.KindPhoto, 1, 150); !errors.Is(err, ErrUnsupported) {
		t.Errorf("no FFmpeg: %v, want ErrUnsupported", err)
	}
}

// An animated PNG is one picture to the index: its first frame. The WebView
// plays the original.
func TestAnimatedPNGGivesItsFirstFrame(t *testing.T) {
	ffmpeg := testenv.File(t, "ffmpeg", "ffmpeg.exe")
	path := makeSample(t, ffmpeg, "spinner.apng", "-f", "lavfi", "-i", "testsrc2=size=120x80:rate=5", "-t", "1", "-plays", "0", "-f", "apng")
	if !core.WebNative(path) || core.ContentType(".apng") != "image/apng" {
		t.Fatal("the WebView is not sent an animated PNG as it is")
	}
	m, err := ReadMetadata(path, core.KindPhoto)
	if err != nil || m.Width != 120 || m.Height != 80 {
		t.Fatalf("metadata: %+v, %v", m, err)
	}
	th, err := Decoders{}.Thumbnail(context.Background(), path, core.KindPhoto, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(th.Large)); err != nil || cfg.Width != 120 || cfg.Height != 80 {
		t.Errorf("thumbnail %dx%d, %v", cfg.Width, cfg.Height, err)
	}
}

func TestATIFFsLaterPagesAreDecodedOneAtATime(t *testing.T) {
	ctx := context.Background()
	red, green, blue := [3]byte{220, 0, 0}, [3]byte{0, 220, 0}, [3]byte{0, 0, 220}
	path := write(t, "scan.tiff", testenv.TIFF(red, green, blue))
	info, err := Decoders{}.Inspect(ctx, path, core.KindPhoto)
	if err != nil || info.Pages != 3 {
		t.Fatalf("pages = %d, %v; want 3", info.Pages, err)
	}
	for page, want := range [][3]byte{red, green, blue} {
		im, err := Decoders{}.DecodePage(ctx, path, core.KindPhoto, 1, 64, page)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if r, g, b := im.at(5, 5); im.W != 40 || im.H != 30 || [3]byte{byte(r), byte(g), byte(b)} != want {
			t.Errorf("page %d: %dx%d of %d,%d,%d, want %v", page, im.W, im.H, r, g, b, want)
		}
	}
	// The index reads the first page; a page the file does not hold fails.
	if th, err := (Decoders{}).Thumbnail(ctx, path, core.KindPhoto, 1); err != nil || len(th.Large) == 0 {
		t.Errorf("thumbnail: %v", err)
	}
	if _, err := (Decoders{}).DecodePage(ctx, path, core.KindPhoto, 1, 64, 3); err == nil {
		t.Error("a fourth page was decoded from a file of three")
	}
	// Only a TIFF has later pages: an animation's frames are not pages.
	gif := filepath.Join(t.TempDir(), "a.gif")
	if _, err := (Decoders{}).DecodePage(ctx, gif, core.KindPhoto, 1, 64, 1); !errors.Is(err, ErrUnsupported) {
		t.Errorf("page 1 of a GIF: %v, want ErrUnsupported", err)
	}
}

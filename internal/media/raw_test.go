package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/testenv"
)

func rawDecoders(t *testing.T) Decoders {
	t.Helper()
	return Decoders{Raw: testenv.File(t, "lumen-raw.exe")}
}

// at is the colour of a decoded picture at a point given in tenths of its size.
func (im Image) at(x, y int) (r, g, b int) {
	i := 3 * ((im.H*y/10)*im.W + im.W*x/10)
	return int(im.Pix[i]), int(im.Pix[i+1]), int(im.Pix[i+2])
}

func TestRawIsDevelopedFromItsSensorDataAndTurnedUpright(t *testing.T) {
	d := rawDecoders(t)
	ctx := context.Background()
	p := write(t, "turned.dng", testenv.DNG{Orientation: 6}.Bytes(t)) // the camera was held a quarter turn clockwise

	m, err := d.Metadata(ctx, p, core.KindPhoto)
	if err != nil {
		t.Fatal(err)
	}
	if m.Width != testenv.DNGHeight || m.Height != testenv.DNGWidth || m.Orientation != 1 {
		t.Errorf("metadata = %dx%d orientation %d, want %dx%d upright", m.Width, m.Height, m.Orientation, testenv.DNGHeight, testenv.DNGWidth)
	}
	if m.Camera != "Lumen Test Camera" || m.TakenAt.Format(time.DateTime) != "2024-03-05 10:20:30" {
		t.Errorf("camera %q taken %s", m.Camera, m.TakenAt.Format(time.DateTime))
	}

	im, err := d.Decode(ctx, p, core.KindPhoto, m.Orientation, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if im.W != testenv.DNGHeight || im.H != testenv.DNGWidth || im.Preview {
		t.Fatalf("decoded %dx%d preview=%t, want %dx%d developed", im.W, im.H, im.Preview, testenv.DNGHeight, testenv.DNGWidth)
	}
	// The sensor's left half was red; turned upright that is the top.
	if r, _, b := im.at(5, 2); r < 2*b {
		t.Errorf("top is not red: r=%d b=%d", r, b)
	}
	if r, _, b := im.at(5, 8); b < 2*r {
		t.Errorf("bottom is not blue: r=%d b=%d", r, b)
	}

	th, err := d.Thumbnail(ctx, p, core.KindPhoto, m.Orientation)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(th.Large)); err != nil || cfg.Width >= cfg.Height {
		t.Errorf("thumbnail is not upright: %dx%d, %v", cfg.Width, cfg.Height, err)
	}
}

func TestRawThatCannotBeDevelopedShowsItsEmbeddedPreview(t *testing.T) {
	d := rawDecoders(t)
	p := write(t, "cut.dng", testenv.DNG{Cut: true}.Bytes(t))
	im, err := d.Decode(context.Background(), p, core.KindPhoto, 1, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !im.Preview || im.W != 160 || im.H != 120 {
		t.Fatalf("decoded %dx%d preview=%t, want the 160x120 preview", im.W, im.H, im.Preview)
	}
	if r, g, b := im.at(5, 5); g < 150 || r > 60 || b > 60 {
		t.Errorf("preview is not green: %d %d %d", r, g, b)
	}
}

func TestRawThatLibRawCannotReadIsUnsupportedNotFailed(t *testing.T) {
	d := rawDecoders(t)
	ctx := context.Background()
	junk := write(t, "junk.cr2", bytes.Repeat([]byte("not a camera file "), 400))
	if _, err := d.Metadata(ctx, junk, core.KindPhoto); !errors.Is(err, ErrUnsupported) {
		t.Errorf("metadata: %v, want ErrUnsupported", err)
	}
	if _, err := d.Thumbnail(ctx, junk, core.KindPhoto, 1); !errors.Is(err, ErrUnsupported) {
		t.Errorf("thumbnail: %v, want ErrUnsupported", err)
	}
	// Without the worker (removed, quarantined) every RAW is skipped the same way.
	p := write(t, "a.dng", testenv.DNG{}.Bytes(t))
	if _, err := (Decoders{}).Decode(ctx, p, core.KindPhoto, 1, 256); !errors.Is(err, ErrUnsupported) {
		t.Errorf("no worker: %v, want ErrUnsupported", err)
	}
}

func TestRawWorkerStopsWhenItsRequestGoesAway(t *testing.T) {
	d := rawDecoders(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Decode(ctx, write(t, "a.dng", testenv.DNG{}.Bytes(t)), core.KindPhoto, 1, 256); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestRenderOfAnEmbeddedPreviewSaysSo(t *testing.T) {
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, halves(8, 8, color.White, color.Black), nil); err != nil {
		t.Fatal(err)
	}
	if markedPreview(plain.Bytes()) {
		t.Fatal("an unmarked render reads as a preview")
	}
	marked := markPreview(plain.Bytes())
	if !markedPreview(marked) {
		t.Fatal("the mark does not read back")
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(marked)); err != nil || format != "jpeg" || cfg.Width != 8 {
		t.Errorf("the marked render is no longer a JPEG: %v", err)
	}
}

func TestExposureTimeReadsAsEXIFWritesIt(t *testing.T) {
	for seconds, want := range map[float64]string{0.004: "1/250", 1.0 / 60: "1/60", 0.5: "5/10", 1.3: "13/10", 2: "2/1", 30: "30/1"} {
		if got := exposureTime(seconds); got != want {
			t.Errorf("exposureTime(%v) = %s, want %s", seconds, got, want)
		}
	}
}

// TestRealCameraFiles runs over the camera files in LUMEN_RAW_SAMPLES
// (docs/phase9-formats-2026-10-07.md lists the set it was accepted on).
// Every file must give its size, a thumbnail and pixels of the same shape:
// the developed picture, or for a file LibRaw cannot develop its preview.
func TestRealCameraFiles(t *testing.T) {
	d := rawDecoders(t)
	ctx := context.Background()
	for _, p := range testenv.RawSamples(t) {
		t.Run(filepath.Base(p), func(t *testing.T) {
			m, err := d.Metadata(ctx, p, core.KindPhoto)
			if strings.Contains(strings.ToLower(filepath.Base(p)), "lossy") {
				// A DNG whose sensor data is lossy JPEG needs a JPEG library,
				// which the worker is built without: it is skipped, with a reason.
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("err = %v, want ErrUnsupported", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.Width < 600 || m.Height < 600 || m.Camera == "" {
				t.Errorf("metadata: %dx%d camera %q", m.Width, m.Height, m.Camera)
			}
			th, err := d.Thumbnail(ctx, p, core.KindPhoto, m.Orientation)
			if err != nil {
				t.Fatal(err)
			}
			im, err := d.Decode(ctx, p, core.KindPhoto, m.Orientation, 2048)
			if err != nil {
				t.Fatal(err)
			}
			if im.Preview {
				t.Fatalf("only the embedded preview was read")
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(th.Large))
			if err != nil {
				t.Fatal(err)
			}
			// One development serves all three, so they have one shape.
			for name, got := range map[string]float64{"thumbnail": float64(cfg.Width) / float64(cfg.Height), "pixels": float64(im.W) / float64(im.H)} {
				if want := float64(m.Width) / float64(m.Height); got < want*0.99 || got > want*1.01 {
					t.Errorf("%s shape %.3f, metadata says %.3f", name, got, want)
				}
			}
			if max(im.W, im.H) != min(2048, max(m.Width, m.Height)) {
				t.Errorf("pixels are %dx%d of %dx%d", im.W, im.H, m.Width, m.Height)
			}
			if stat, err := os.Stat(p); err != nil || stat.Size() == 0 {
				t.Errorf("the original changed: %v", err)
			}
		})
	}
}

package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
	"lumen/internal/testenv"
)

func encodeJXLFixture(t *testing.T, source string) (string, Decoders) {
	t.Helper()
	encoder := testenv.File(t, "compress", "cjxl.exe")
	d := Decoders{JXL: testenv.File(t, "compress", "djxl.exe"), FFprobe: testenv.File(t, "ffmpeg", "ffprobe.exe")}
	source, err := filepath.Abs(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "copy.jxl")
	args := []string{source, path, "--distance=0", "--effort=3", "--num_threads=1", "--keep_invisible=1", "--premultiply=0"}
	if err := RunTool(t.Context(), encoder, "", args...); err != nil {
		t.Fatal(err)
	}
	return path, d
}

func TestJPEGXLUsesTheOriginalColourProfileAndStraightAlpha(t *testing.T) {
	alpha := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			alpha.SetNRGBA(x, y, color.NRGBA{R: 125, G: 33, B: 171, A: uint8((x % 3) * 127)})
		}
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, alpha); err != nil {
		t.Fatal(err)
	}
	alphaPath := filepath.Join(t.TempDir(), "alpha.png")
	if err := os.WriteFile(alphaPath, pngData.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{filepath.Join("testdata", "red-p3.jpg"), alphaPath} {
		t.Run(filepath.Base(source), func(t *testing.T) {
			path, d := encodeJXLFixture(t, source)
			a, err := d.Decode(t.Context(), source, core.KindPhoto, 1, 64)
			if err != nil {
				t.Fatal(err)
			}
			b, err := d.Decode(t.Context(), path, core.KindPhoto, 1, 64)
			if err != nil {
				t.Fatal(err)
			}
			if a.W != b.W || a.H != b.H || len(a.Pix) != len(b.Pix) {
				t.Fatalf("dimensions changed: %dx%d -> %dx%d", a.W, a.H, b.W, b.H)
			}
			for i, sample := range a.Pix {
				// The fixture's JPEG inverse-DCT rounding varies by decoder;
				// unconverted P3 differs by tens of levels. PNG samples are exact.
				tolerance := 0
				if core.FamilyOf(source) == core.FamilyJPEG {
					tolerance = 8
				}
				if delta := int(sample) - int(b.Pix[i]); delta < -tolerance || delta > tolerance {
					t.Fatalf("colour/alpha changed at sample %d: %d -> %d", i, sample, b.Pix[i])
				}
			}
		})
	}
}

func TestJPEGXLReadsCaptureMetadataFromCompressedEXIFBoxes(t *testing.T) {
	source := filepath.Join("testdata", "gopro.jpg")
	path, d := encodeJXLFixture(t, source)
	a, err := d.Metadata(t.Context(), source, core.KindPhoto)
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.Metadata(t.Context(), path, core.KindPhoto)
	if err != nil {
		t.Fatal(err)
	}
	if a.TakenAt.IsZero() || a.Camera == "" || a.TakenAt != b.TakenAt || a.Camera != b.Camera || a.Width != b.Width || a.Height != b.Height {
		t.Fatalf("capture metadata changed: %+v -> %+v", a, b)
	}
}

func TestJPEGXLUsesTheSameCameraNamingAsJPEG(t *testing.T) {
	var pixels bytes.Buffer
	if err := jpeg.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 64, 48)), nil); err != nil {
		t.Fatal(err)
	}
	// An unfamiliar make exercises the primary parser's naming rules rather
	// than the fallback used by the real GoPro fixture.
	exif := append([]byte("Exif\x00\x00"), (testenv.DNG{Orientation: 6}).Bytes(t)...)
	data := append([]byte{0xff, 0xd8, 0xff, 0xe1}, binary.BigEndian.AppendUint16(nil, uint16(len(exif)+2))...)
	data = append(data, exif...)
	data = append(data, pixels.Bytes()[2:]...)
	source := filepath.Join(t.TempDir(), "camera.jpg")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	path, d := encodeJXLFixture(t, source)
	a, err := d.Metadata(t.Context(), source, core.KindPhoto)
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.Metadata(t.Context(), path, core.KindPhoto)
	if err != nil {
		t.Fatal(err)
	}
	if a.Camera == "" || a.Camera != b.Camera || a.TakenAt != b.TakenAt || a.Width != b.Width || a.Height != b.Height {
		t.Fatalf("camera interpretation changed: %+v -> %+v", a, b)
	}
}

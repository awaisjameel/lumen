package media

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
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

// djxl cannot shrink while it decodes, and its PNG is held in memory under a
// bound. A picture over it (tens of megapixels) is still shown, by FFmpeg at
// the size asked for, as every JPEG XL was before djxl read them.
func TestAJPEGXLTooLargeForTheBoundedDecodeIsStillShown(t *testing.T) {
	d := Decoders{JXL: testenv.File(t, "compress", "djxl.exe"), FFmpeg: testenv.File(t, "ffmpeg", "ffmpeg.exe")}
	// Noise does not compress: the PNG is as large as its 72 MB of samples.
	const w, h = 6000, 4000
	header := fmt.Sprintf("P6\n%d %d\n255\n", w, h)
	noise := make([]byte, len(header)+w*h*3)
	copy(noise, header)
	if _, err := rand.Read(noise[len(header):]); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source, path := filepath.Join(dir, "noise.ppm"), filepath.Join(dir, "noise.jxl")
	if err := os.WriteFile(source, noise, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runContainedToolAs(t.Context(), hideWindow, testenv.File(t, "compress", "cjxl.exe"), "", io.Discard, renderMemory,
		source, path, "--distance=1", "--effort=1", "--num_threads=2"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.jxlPNG(t.Context(), path); !errors.Is(err, errOutputBudget) {
		t.Fatalf("the fixture's PNG no longer exceeds the budget: %v", err)
	}
	img, err := d.Decode(t.Context(), path, core.KindPhoto, 1, 300)
	if err != nil {
		t.Fatal(err)
	}
	if img.W != 300 || img.H != 200 {
		t.Fatalf("decoded %dx%d, want 300x200", img.W, img.H)
	}
	if _, err := d.Inspect(t.Context(), path, core.KindPhoto); err != nil {
		t.Fatalf("inspecting it: %v", err)
	}
	d.FFmpeg = ""
	if _, err := d.Decode(t.Context(), path, core.KindPhoto, 1, 300); !errors.Is(err, errOutputBudget) {
		t.Fatalf("without FFmpeg: %v, want the budget's refusal", err)
	}
}

// os/exec copies a tool's output with io.Copy, which prefers a writer's
// ReadFrom: the cap must hold on that path too, not only for a plain Write.
func TestCappedOutputRefusesMoreThanTheBudgetHoweverItIsCopied(t *testing.T) {
	var within cappedBuffer
	if n, err := io.Copy(&within, io.LimitReader(rand.Reader, renderBytes)); err != nil || n != renderBytes || within.full {
		t.Fatalf("output within the budget: %d bytes, %v", n, err)
	}
	var over cappedBuffer
	if _, err := io.Copy(&over, io.LimitReader(rand.Reader, renderBytes+1)); !errors.Is(err, errOutputBudget) || !over.full {
		t.Fatalf("output over the budget: %v, full %t", err, over.full)
	}
	if len(over.Bytes()) > renderBytes {
		t.Fatalf("kept %d bytes, over the budget", len(over.Bytes()))
	}
}

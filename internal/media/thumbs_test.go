package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"

	"lumen/internal/core"
	"lumen/internal/testenv"
)

// halves is a w x h image: left half a, right half b.
func halves(w, h int, a, b color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := a
			if x >= w/2 {
				c = b
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// withOrientation inserts a minimal EXIF segment holding only an orientation.
func withOrientation(jpg []byte, o uint16) []byte {
	var tiff bytes.Buffer
	tiff.WriteString("MM\x00\x2a")
	binary.Write(&tiff, binary.BigEndian, uint32(8)) // first IFD
	binary.Write(&tiff, binary.BigEndian, uint16(1)) // one entry
	binary.Write(&tiff, binary.BigEndian, []uint16{0x0112, 3})
	binary.Write(&tiff, binary.BigEndian, uint32(1)) // count
	binary.Write(&tiff, binary.BigEndian, []uint16{o, 0})
	binary.Write(&tiff, binary.BigEndian, uint32(0)) // no next IFD
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append([]byte{0xFF, 0xD8}, seg...)
	out = append(out, payload...)
	return append(out, jpg[2:]...) // the original minus its SOI
}

func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func decodeJPEG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// near reports whether c is within 40 of (r, g, b) on every channel.
func near(c color.Color, r, g, b uint8) bool {
	cr, cg, cb, _ := c.RGBA()
	d := func(x uint32, y uint8) bool { v := int(x>>8) - int(y); return v > -40 && v < 40 }
	return d(cr, r) && d(cg, g) && d(cb, b)
}

var ctx = context.Background()

var (
	red  = color.RGBA{220, 30, 30, 255}
	blue = color.RGBA{30, 30, 220, 255}
)

func TestThumbnailAppliesEXIFOrientation(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, halves(400, 200, red, blue), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	// 6 = shown rotated 90° clockwise: the left (red) half ends up on top.
	p := write(t, "rotated.jpg", withOrientation(buf.Bytes(), 6))
	th, err := Decoders{}.Thumbnail(ctx, p, core.KindPhoto, 6)
	if err != nil {
		t.Fatal(err)
	}
	img := decodeJPEG(t, th.Large)
	if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 400 {
		t.Fatalf("large is %v, want 200x400 (upright, never upscaled)", b)
	}
	if !near(img.At(100, 50), 220, 30, 30) || !near(img.At(100, 350), 30, 30, 220) {
		t.Errorf("not rotated upright: top %v, bottom %v", img.At(100, 50), img.At(100, 350))
	}
	if b := decodeJPEG(t, th.Small).Bounds(); b.Dx() != 128 || b.Dy() != 256 {
		t.Errorf("small is %v, want 128x256", b)
	}
}

func TestThumbnailFlattensTransparencyOntoWhite(t *testing.T) {
	img := halves(300, 300, color.RGBA{}, red) // left half fully transparent
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	th, err := Decoders{}.Thumbnail(ctx, write(t, "alpha.png", buf.Bytes()), core.KindPhoto, 1)
	if err != nil {
		t.Fatal(err)
	}
	out := decodeJPEG(t, th.Small) // 256x256
	if !near(out.At(20, 128), 255, 255, 255) || !near(out.At(236, 128), 220, 30, 30) {
		t.Errorf("got %v and %v, want white and red", out.At(20, 128), out.At(236, 128))
	}
}

func TestThumbnailBMPAndHash(t *testing.T) {
	var a, b bytes.Buffer
	if err := bmp.Encode(&a, halves(64, 48, red, blue)); err != nil {
		t.Fatal(err)
	}
	if err := bmp.Encode(&b, halves(64, 48, blue, red)); err != nil {
		t.Fatal(err)
	}
	ta, err := Decoders{}.Thumbnail(ctx, write(t, "a.bmp", a.Bytes()), core.KindPhoto, 1)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := Decoders{}.Thumbnail(ctx, write(t, "b.bmp", b.Bytes()), core.KindPhoto, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeJPEG(t, ta.Large).Bounds(); got.Dx() != 64 || got.Dy() != 48 {
		t.Errorf("large is %v, want 64x48", got)
	}
	again, _ := Decoders{}.Thumbnail(ctx, write(t, "a2.bmp", a.Bytes()), core.KindPhoto, 1)
	if ta.PHash != again.PHash || ta.PHash == tb.PHash {
		t.Errorf("hashes: %x %x %x (want first two equal, third different)", ta.PHash, again.PHash, tb.PHash)
	}
}

func TestThumbnailRejectsVideosAndBrokenFiles(t *testing.T) {
	if _, err := (Decoders{}).Thumbnail(ctx, "clip.mp4", core.KindVideo, 1); err != ErrUnsupported {
		t.Errorf("video: got %v, want ErrUnsupported", err)
	}
	if _, err := (Decoders{}).Thumbnail(ctx, write(t, "junk.jpg", []byte("not an image")), core.KindPhoto, 1); err == nil {
		t.Error("junk: want an error")
	}
	if _, err := (Decoders{}).Thumbnail(ctx, filepath.Join(t.TempDir(), "gone.png"), core.KindPhoto, 1); err == nil {
		t.Error("missing file: want an error")
	}
}

func TestHEIC(t *testing.T) {
	samples := testenv.HEICSamples(t)
	ff := testenv.File(t, "ffmpeg", "ffmpeg.exe")
	for _, p := range samples {
		t.Run(filepath.Base(p), func(t *testing.T) {
			th, err := Decoders{FFmpeg: ff}.Thumbnail(ctx, p, core.KindPhoto, 1)
			if err != nil {
				t.Fatal(err)
			}
			big := decodeJPEG(t, th.Large).Bounds()
			if max(big.Dx(), big.Dy()) > core.ThumbLarge {
				t.Errorf("large is %v", big)
			}
			// FFmpeg alone (the fallback when WIC can't) must agree on the shape.
			pix, w, h, err := ffmpegDecode(ctx, ff, p, core.ThumbLarge)
			if err != nil {
				t.Fatal(err)
			}
			if len(pix) != w*h*3 || abs(w*big.Dy()-h*big.Dx()) > max(w, h) {
				t.Errorf("FFmpeg gives %dx%d, WIC path %v", w, h, big)
			}
		})
	}
}

func TestParsePPM(t *testing.T) {
	pix, w, h, err := parsePPM(append([]byte("P6\n2 1\n255\n"), 1, 2, 3, 4, 5, 6))
	if err != nil || w != 2 || h != 1 || !bytes.Equal(pix, []byte{1, 2, 3, 4, 5, 6}) {
		t.Errorf("got %v %d %d %v", pix, w, h, err)
	}
	for _, bad := range []string{"", "P5\n1 1\n255\n\x00", "P6\n2 2\n255\n\x00\x00\x00", "P6\nx 1\n255\n"} {
		if _, _, _, err := parsePPM([]byte(bad)); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func abs(x int) int { return max(x, -x) }

package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/bits"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"

	"lumen/internal/core"
	"lumen/internal/media/vips"
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
	be := binary.BigEndian
	tiff := []byte("MM\x00\x2a")
	tiff = be.AppendUint32(tiff, 8)                          // first IFD
	tiff = be.AppendUint16(tiff, 1)                          // one entry
	tiff = be.AppendUint16(be.AppendUint16(tiff, 0x0112), 3) // orientation, a SHORT
	tiff = be.AppendUint32(tiff, 1)                          // count
	tiff = be.AppendUint16(be.AppendUint16(tiff, o), 0)
	tiff = be.AppendUint32(tiff, 0) // no next IFD
	payload := append([]byte("Exif\x00\x00"), tiff...)
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
	if _, err := (Decoders{}).Thumbnail(ctx, "clip.mp4", core.KindVideo, 1); !errors.Is(err, ErrUnsupported) {
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
			// FFmpeg leaves a Display P3 photo in P3: converting from its
			// profile must bring it nearer what WIC makes (sRGB).
			icc, err := vips.Profile(p)
			if err != nil || icc == nil || isSRGB(icc) {
				return
			}
			raw := vips.Pixels{Pix: pix, W: w, H: h, Bands: 3, Orientation: 1}
			unmanaged, err := vips.DecodePixels(raw, core.ThumbSmall)
			if err != nil {
				t.Fatal(err)
			}
			raw.ICC = icc
			managed, err := vips.DecodePixels(raw, core.ThumbSmall)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(managed.Pix, unmanaged.Pix) {
				t.Fatal("the photo's profile changed nothing")
			}
			wic, err := Decoders{}.Decode(ctx, p, core.KindPhoto, 1, core.ThumbSmall)
			if err != nil || wic.W != managed.W || wic.H != managed.H {
				return // no HEVC extension here, or another size: nothing to compare with
			}
			if m, u := meanDiff(managed.Pix, wic.Pix), meanDiff(unmanaged.Pix, wic.Pix); m >= u {
				t.Errorf("converted pixels differ from WIC's by %.2f, unconverted by %.2f", m, u)
			}
		})
	}
}

// meanDiff is the mean absolute difference of two equally long pixel buffers.
func meanDiff(a, b []byte) float64 {
	var sum int
	for i := range a {
		sum += abs(int(a[i]) - int(b[i]))
	}
	return float64(sum) / float64(len(a))
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

// Decode must give the same upright picture as the thumbnails, so face boxes
// found in it line up with them.
func TestDecodeMatchesThumbnail(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, halves(400, 200, red, blue), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	p := write(t, "rotated.jpg", withOrientation(buf.Bytes(), 6))
	im, err := Decoders{}.Decode(ctx, p, core.KindPhoto, 6, 300)
	if err != nil {
		t.Fatal(err)
	}
	if im.W != 150 || im.H != 300 || len(im.Pix) != im.W*im.H*3 {
		t.Fatalf("decoded %dx%d (%d bytes), want 150x300 upright", im.W, im.H, len(im.Pix))
	}
	at := func(x, y int) color.RGBA {
		o := (y*im.W + x) * 3
		return color.RGBA{im.Pix[o], im.Pix[o+1], im.Pix[o+2], 255}
	}
	if !near(at(75, 40), 220, 30, 30) || !near(at(75, 260), 30, 30, 220) {
		t.Errorf("not upright: top %v, bottom %v", at(75, 40), at(75, 260))
	}
	// Never enlarged.
	if im, _ := (Decoders{}).Decode(ctx, p, core.KindPhoto, 6, 4000); im.W != 200 || im.H != 400 {
		t.Errorf("side 4000 gave %dx%d, want the original 200x400", im.W, im.H)
	}
	// Pixels decoded outside libvips (BMP here) take the same route.
	var b bytes.Buffer
	if err := bmp.Encode(&b, halves(64, 48, red, blue)); err != nil {
		t.Fatal(err)
	}
	if im, err := (Decoders{}).Decode(ctx, write(t, "a.bmp", b.Bytes()), core.KindPhoto, 1, 32); err != nil || im.W != 32 || im.H != 24 {
		t.Errorf("bmp decoded %dx%d (%v), want 32x24", im.W, im.H, err)
	}
}

// A photo with a wide-gamut profile (Display P3, as iPhones take) must come
// out as the same colours in sRGB, not its raw values read as sRGB (duller).
// testdata/red-p3.jpg is pure red stored in P3: raw values about (234, 51, 35).
func TestWideGamutPhotosAreConvertedToSRGB(t *testing.T) {
	var d Decoders
	th, err := d.Thumbnail(ctx, filepath.Join("testdata", "red-p3.jpg"), core.KindPhoto, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c := decodeJPEG(t, th.Small).At(8, 8); !nearly(c, 255, 0, 0) {
		t.Errorf("thumbnail of P3 red is %v, want sRGB red", c)
	}
	im, err := d.Decode(ctx, filepath.Join("testdata", "red-p3.jpg"), core.KindPhoto, 1, 64)
	if err != nil {
		t.Fatal(err)
	}
	if px := im.Pix[(8*im.W+8)*3:]; !nearly(color.RGBA{px[0], px[1], px[2], 255}, 255, 0, 0) {
		t.Errorf("decoded P3 red is %v, want sRGB red", px[:3])
	}
}

// nearly reports whether c is within 12 of (r, g, b) on every channel.
func nearly(c color.Color, r, g, b uint8) bool {
	cr, cg, cb, _ := c.RGBA()
	d := func(x uint32, y uint8) bool { v := int(x>>8) - int(y); return v > -12 && v < 12 }
	return d(cr, r) && d(cg, g) && d(cb, b)
}

// Thumbnails made from a photo decoded for the faces stage are the ones the
// file itself gives: same sizes, the same way up, nearly the same hash.
func TestThumbsFromImageMatchThumbnail(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	for y := range 2000 {
		for x := range 3000 {
			img.Set(x, y, color.RGBA{uint8(x * 255 / 3000), uint8(y * 255 / 2000), uint8((x ^ y) >> 4), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	p := write(t, "big.jpg", withOrientation(buf.Bytes(), 6))
	var d Decoders
	want, err := d.Thumbnail(ctx, p, core.KindPhoto, 6)
	if err != nil {
		t.Fatal(err)
	}
	im, err := d.Decode(ctx, p, core.KindPhoto, 6, 2048)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ThumbsFromImage(im)
	if err != nil {
		t.Fatal(err)
	}
	size := func(b []byte) image.Point {
		c, err := jpeg.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return image.Pt(c.Width, c.Height)
	}
	for _, c := range []struct {
		name      string
		got, want []byte
	}{{"large", got.Large, want.Large}, {"small", got.Small, want.Small}} {
		if g, w := size(c.got), size(c.want); g != w {
			t.Errorf("%s thumbnail is %v, want %v", c.name, g, w)
		}
	}
	if dist := bits.OnesCount64(got.PHash ^ want.PHash); dist > 4 {
		t.Errorf("hashes differ in %d bits", dist)
	}
}

// Pixels decoded elsewhere in another profile (as FFmpeg decodes HEIC) are
// converted from it as libvips converts a file in that profile.
func TestPixelsConvertFromTheirProfile(t *testing.T) {
	path := filepath.Join("testdata", "red-p3.jpg")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f) // Go leaves the pixels in the file's profile
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	raw := make([]byte, 0, b.Dx()*b.Dy()*3)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			raw = append(raw, byte(r>>8), byte(g>>8), byte(bl>>8))
		}
	}
	icc, err := vips.Profile(path)
	if err != nil || icc == nil {
		t.Fatalf("profile: %d bytes, %v", len(icc), err)
	}
	want, err := vips.DecodeFile(path, core.ThumbSmall)
	if err != nil {
		t.Fatal(err)
	}
	p := vips.Pixels{Pix: raw, W: b.Dx(), H: b.Dy(), Bands: 3, Orientation: 1}
	unmanaged, err := vips.DecodePixels(p, core.ThumbSmall)
	if err != nil {
		t.Fatal(err)
	}
	p.ICC = icc
	managed, err := vips.DecodePixels(p, core.ThumbSmall)
	if err != nil {
		t.Fatal(err)
	}
	if managed.W != want.W || managed.H != want.H {
		t.Fatalf("sizes %dx%d and %dx%d", managed.W, managed.H, want.W, want.H)
	}
	if d := meanDiff(managed.Pix, want.Pix); d > 2 {
		t.Errorf("converted pixels differ from the file's by %.2f", d)
	}
	if d := meanDiff(unmanaged.Pix, want.Pix); d <= 2 {
		t.Errorf("the profile made no difference (%.2f): the test photo shows nothing", d)
	}
}

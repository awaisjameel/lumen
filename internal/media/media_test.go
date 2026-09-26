package media

import (
	"image"
	"image/color"
	"testing"
	"time"

	"lumen/internal/core"
)

func TestDateFromFilename(t *testing.T) {
	cases := []struct {
		name string
		want string // "" = no date
	}{
		{"IMG_20230514_184502.jpg", "2023-05-14 18:45:02"},
		{"PXL_20230514_184502123.jpg", "2023-05-14 00:00:00"}, // millis glued on: date only
		{"IMG-20230514-WA0001.jpg", "2023-05-14 00:00:00"},
		{"Screenshot_2023-05-14-18-45-02.png", "2023-05-14 18:45:02"},
		{"2023-05-14 18.45.02.jpg", "2023-05-14 18:45:02"},
		{"VID_20230514_184502.mp4", "2023-05-14 18:45:02"},
		{"holiday.jpg", ""},
		{"DSC01234.JPG", ""},
		{"IMG_20230231_101010.jpg", ""}, // 31 February
		{"scan_20991231.jpg", ""},       // the future
	}
	for _, c := range cases {
		got, ok := DateFromFilename(c.name)
		switch {
		case c.want == "" && ok:
			t.Errorf("%s: got %v, want no date", c.name, got)
		case c.want != "" && !ok:
			t.Errorf("%s: no date, want %s", c.name, c.want)
		case ok && got.Format(time.DateTime) != c.want:
			t.Errorf("%s: got %s, want %s", c.name, got.Format(time.DateTime), c.want)
		}
	}
}

func TestOrientSwapsAndMovesPixels(t *testing.T) {
	// 3x2 image with a marker in the top-left corner.
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})
	cases := []struct {
		o          int
		w, h, x, y int // expected size and marker position
	}{
		{1, 3, 2, 0, 0},
		{2, 3, 2, 2, 0},
		{3, 3, 2, 2, 1},
		{4, 3, 2, 0, 1},
		{5, 2, 3, 0, 0},
		{6, 2, 3, 1, 0}, // rotate 90° clockwise: top-left goes top-right
		{7, 2, 3, 1, 2},
		{8, 2, 3, 0, 2}, // rotate 90° anticlockwise: top-left goes bottom-left
	}
	for _, c := range cases {
		dst := orient(src, c.o)
		if dst.Rect.Dx() != c.w || dst.Rect.Dy() != c.h {
			t.Errorf("o%d: size %dx%d, want %dx%d", c.o, dst.Rect.Dx(), dst.Rect.Dy(), c.w, c.h)
			continue
		}
		if r, _, _, _ := dst.At(c.x, c.y).RGBA(); r>>8 != 255 {
			t.Errorf("o%d: marker not at (%d,%d)", c.o, c.x, c.y)
		}
	}
}

func TestResizeYCbCrKeepsAspectAndColour(t *testing.T) {
	src := image.NewYCbCr(image.Rect(0, 0, 6000, 4000), image.YCbCrSubsampleRatio420)
	for i := range src.Y {
		src.Y[i] = 200
	}
	for i := range src.Cb {
		src.Cb[i], src.Cr[i] = 128, 128
	}
	dst := resize(src, 1024)
	if dst.Rect.Dx() != 1024 || dst.Rect.Dy() != 682 {
		t.Fatalf("got %v, want 1024x682", dst.Rect)
	}
	r, g, b, _ := dst.At(500, 300).RGBA()
	if r>>8 != 200 || g>>8 != 200 || b>>8 != 200 {
		t.Errorf("colour drifted: %d %d %d", r>>8, g>>8, b>>8)
	}
}

// testdata/gopro.jpg is JPEG.jpg from github.com/evanoberholster/imagemeta
// (MIT): a real GoPro HERO4 photo that imagemeta v1.1.0 parses as empty.
func TestReadMetadataFallsBackForGoPro(t *testing.T) {
	m, err := ReadMetadata("testdata/gopro.jpg", core.KindPhoto)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.TakenAt.Format(time.DateTime); got != "2016-10-11 16:59:50" {
		t.Errorf("date = %s, want 2016-10-11 16:59:50", got)
	}
	if m.Camera != "GoPro HERO4 Silver" {
		t.Errorf("camera = %q", m.Camera)
	}
	if m.Width != 1000 || m.Height != 563 {
		t.Errorf("size = %dx%d", m.Width, m.Height)
	}
}

func TestFitNeverUpscales(t *testing.T) {
	if w, h := fit(800, 600, 1024); w != 800 || h != 600 {
		t.Errorf("got %dx%d", w, h)
	}
	if w, h := fit(3000, 6000, 1024); w != 512 || h != 1024 {
		t.Errorf("got %dx%d", w, h)
	}
}

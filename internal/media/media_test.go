package media

import (
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

package media

import (
	"testing"
	"time"

	"lumen/internal/core"
)

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

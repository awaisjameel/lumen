package index

import (
	"testing"

	"lumen/internal/media"
)

func TestDecodedPhotosAreTakenOnceAndBounded(t *testing.T) {
	var d decoded
	im := media.Image{Pix: make([]byte, 3), W: 1, H: 1}
	d.put(1, 6, im)
	if _, ok := d.take(1, 1); ok {
		t.Fatal("a photo turned upright by another orientation was taken")
	}
	d.put(1, 6, im)
	if _, ok := d.take(1, 6); !ok {
		t.Fatal("kept photo not taken")
	}
	if _, ok := d.take(1, 6); ok {
		t.Fatal("photo taken twice")
	}
	for id := range int64(decodedMax + 5) {
		d.put(id+10, 1, im)
	}
	if n := len(d.ids()); n != decodedMax || d.room() {
		t.Fatalf("%d photos kept, room %v; want %d and no room", n, d.room(), decodedMax)
	}
	d.clear()
	if !d.room() || len(d.ids()) != 0 {
		t.Fatal("clear left photos")
	}
}

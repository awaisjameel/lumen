package index

import (
	"sync"
	"time"

	"lumen/internal/media"
)

// The thumbnail stage keeps up to decodedMax photos it decoded at faceSide
// (about 9 MB each) for the faces stage, which would otherwise decode each
// photo again: a third of a photo's decoding work. Photos the faces stage
// hasn't taken within decodedTTL are dropped.
const (
	decodedMax = 16
	decodedTTL = 2 * time.Minute
)

// decoded holds those photos, by asset.
type decoded struct {
	mu     sync.Mutex
	photos map[int64]decodedPhoto
}

type decodedPhoto struct {
	im          media.Image
	orientation int // the orientation it was turned upright by
	at          time.Time
}

// expire drops photos kept too long. Call with mu held.
func (d *decoded) expire() {
	for id, p := range d.photos {
		if time.Since(p.at) > decodedTTL {
			delete(d.photos, id)
		}
	}
}

// room reports whether another photo may be kept.
func (d *decoded) room() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expire()
	return len(d.photos) < decodedMax
}

// put keeps a photo, dropping the oldest if the stage's workers together
// overshot decodedMax.
func (d *decoded) put(id int64, orientation int, im media.Image) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.photos == nil {
		d.photos = map[int64]decodedPhoto{}
	}
	d.photos[id] = decodedPhoto{im: im, orientation: orientation, at: time.Now()}
	for len(d.photos) > decodedMax {
		var oldest int64
		for id, p := range d.photos {
			if oldest == 0 || p.at.Before(d.photos[oldest].at) {
				oldest = id
			}
		}
		delete(d.photos, oldest)
	}
}

// take removes and returns asset id's photo, if it is kept and was turned
// upright by orientation.
func (d *decoded) take(id int64, orientation int) (media.Image, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.photos[id]
	delete(d.photos, id)
	if !ok || p.orientation != orientation || time.Since(p.at) > decodedTTL {
		return media.Image{}, false
	}
	return p.im, true
}

// ids lists the kept photos.
func (d *decoded) ids() []int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expire()
	ids := make([]int64, 0, len(d.photos))
	for id := range d.photos {
		ids = append(ids, id)
	}
	return ids
}

// clear drops every photo (the faces stage stopped).
func (d *decoded) clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	clear(d.photos)
}

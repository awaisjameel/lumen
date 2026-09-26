// Package media reads what is inside media files: metadata, pixels and
// thumbnails. It never writes to a user's original files.
package media

import (
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/evanoberholster/imagemeta"
	goexif "github.com/rwcarlsen/goexif/exif"

	"lumen/internal/core"
	"lumen/internal/media/vips"
)

// Metadata is what the metadata stage extracts from one file.
type Metadata struct {
	TakenAt     time.Time // zero if the file has no capture date
	Width       int       // as displayed, i.e. after orientation
	Height      int
	Orientation int // EXIF 1..8
	Camera      string
	Lat, Lon    *float64
}

// ReadMetadata reads capture date, size, orientation, camera and GPS. A file
// without EXIF is not an error: the result is simply mostly empty, and the
// width and height come from the image header when Go can decode it.
func ReadMetadata(path string, kind core.Kind) (Metadata, error) {
	m := Metadata{Orientation: 1}
	if kind != core.KindPhoto {
		return m, nil // video metadata arrives with FFmpeg (Phase 4)
	}
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()

	ex, exErr := imagemeta.Decode(f)
	if exErr == nil {
		m.TakenAt = ex.SelectedDate()
		if o := int(ex.IFD0.Orientation); o >= 1 && o <= 8 {
			m.Orientation = o
		}
		m.Width, m.Height = int(ex.ExifIFD.PixelXDimension), int(ex.ExifIFD.PixelYDimension)
		if m.Width == 0 || m.Height == 0 {
			m.Width, m.Height = int(ex.IFD0.ImageWidth), int(ex.IFD0.ImageHeight)
		}
		m.Camera = cameraName(strings.TrimSpace(ex.CameraMake()), strings.TrimSpace(ex.IFD0.Model))
		if lat, lon := ex.GPS.Latitude(), ex.GPS.Longitude(); validGPS(lat, lon) {
			m.Lat, m.Lon = &lat, &lon
		}
	}

	// imagemeta (needed for HEIC) silently returns nothing for some real JPEGs,
	// e.g. GoPro files. goexif reads those, so it backs it up for JPEG/TIFF.
	if m.TakenAt.IsZero() && m.Camera == "" && goexifFormats[strings.ToLower(filepath.Ext(path))] {
		if _, err := f.Seek(0, 0); err == nil {
			fillFromGoexif(f, &m)
		}
	}

	// No size in the EXIF: read the image header, with Go for the common
	// formats and libvips for the rest (AVIF, HEIC).
	if m.Width == 0 || m.Height == 0 {
		if _, err := f.Seek(0, 0); err == nil {
			if cfg, _, err := image.DecodeConfig(f); err == nil {
				m.Width, m.Height = cfg.Width, cfg.Height
			}
		}
	}
	if m.Width == 0 || m.Height == 0 {
		if w, h, err := vips.Size(path); err == nil {
			m.Width, m.Height = w, h
		}
	}
	if m.Orientation >= 5 { // 5..8 are rotated a quarter turn
		m.Width, m.Height = m.Height, m.Width
	}
	if m.TakenAt.Year() < 1900 { // clearly bogus camera clocks
		m.TakenAt = time.Time{}
	}
	return m, nil
}

var goexifFormats = map[string]bool{".jpg": true, ".jpeg": true, ".jpe": true, ".tif": true, ".tiff": true}

// fillFromGoexif fills whatever m is missing from a second EXIF parser.
func fillFromGoexif(r io.Reader, m *Metadata) {
	x, err := goexif.Decode(r)
	if err != nil {
		return
	}
	if t, err := x.DateTime(); err == nil && m.TakenAt.IsZero() {
		m.TakenAt = t
	}
	str := func(n goexif.FieldName) string {
		if tag, err := x.Get(n); err == nil {
			if s, err := tag.StringVal(); err == nil {
				return strings.TrimSpace(strings.TrimRight(s, "\x00"))
			}
		}
		return ""
	}
	num := func(n goexif.FieldName) int {
		if tag, err := x.Get(n); err == nil {
			if v, err := tag.Int(0); err == nil {
				return v
			}
		}
		return 0
	}
	if m.Camera == "" {
		m.Camera = cameraName(str(goexif.Make), str(goexif.Model))
	}
	if o := num(goexif.Orientation); o >= 1 && o <= 8 && m.Orientation == 1 {
		m.Orientation = o
	}
	if m.Width == 0 || m.Height == 0 {
		m.Width, m.Height = num(goexif.PixelXDimension), num(goexif.PixelYDimension)
	}
	if m.Lat == nil {
		if lat, lon, err := x.LatLong(); err == nil && validGPS(lat, lon) {
			m.Lat, m.Lon = &lat, &lon
		}
	}
}

func validGPS(lat, lon float64) bool {
	if math.IsNaN(lat) || math.IsNaN(lon) || (lat == 0 && lon == 0) {
		return false
	}
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// cameraName joins make and model without repeating the make ("Canon Canon EOS R6").
func cameraName(make, model string) string {
	switch {
	case model == "":
		return make
	case make == "" || strings.HasPrefix(strings.ToLower(model), strings.ToLower(make)):
		return model
	}
	return make + " " + model
}

// Filenames that carry a date: IMG_20230514_184502.jpg, PXL_20230514_...,
// IMG-20230514-WA0001.jpg (WhatsApp), Screenshot_2023-05-14-18-45-02.png,
// 2023-05-14 18.45.02.jpg (Dropbox), VID_20230514_184502.mp4 ...
var filenameDate = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)\d{2})[-_.]?(0[1-9]|1[0-2])[-_.]?(0[1-9]|[12]\d|3[01])(?:[-_ .T]?([01]\d|2[0-3])[-_.:]?([0-5]\d)[-_.:]?([0-5]\d))?(?:[^0-9]|$)`)

// DateFromFilename guesses a capture date from common camera and messenger
// file names. ok is false if the name holds no plausible date.
func DateFromFilename(name string) (t time.Time, ok bool) {
	m := filenameDate.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	t = time.Date(n(m[1]), time.Month(n(m[2])), n(m[3]), n(m[4]), n(m[5]), n(m[6]), 0, time.UTC)
	if t.Day() != n(m[3]) || t.After(time.Now().AddDate(1, 0, 0)) { // 31 Feb, or the future
		return time.Time{}, false
	}
	return t, true
}

// GuessDate is the scanner's first guess at when a file was taken, used until
// the metadata stage reads EXIF: a date in the file name, else the modified time.
func GuessDate(name string, mtime time.Time) (wallClock int64, from core.DateSource) {
	if t, ok := DateFromFilename(name); ok {
		return t.Unix(), core.DateFromFilename // already wall-clock (built in UTC)
	}
	return core.WallClock(mtime.Local()), core.DateFromFile
}

// String helps when logging.
func (m Metadata) String() string {
	return fmt.Sprintf("%s %dx%d o%d %q", m.TakenAt.Format(time.DateTime), m.Width, m.Height, m.Orientation, m.Camera)
}

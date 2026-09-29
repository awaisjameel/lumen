// Package media reads what is inside media files: metadata, pixels and
// thumbnails. It never writes to a user's original files.
package media

import (
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
	Modified    time.Time // when its EXIF says it was last saved (not a capture date); zero if unknown
	Width       int       // as displayed, i.e. after orientation
	Height      int
	Orientation int // EXIF 1..8
	Camera      string
	Lat, Lon    *float64
	Duration    int64  // a video's length, ms
	VideoCodec  string // a video's codecs, as FFmpeg names them
	AudioCodec  string // "" = silent
}

// ReadMetadata reads capture date, size, orientation, camera and GPS. A file
// without EXIF is not an error: the result is simply mostly empty, and the
// width and height come from the image header when Go can decode it.
func ReadMetadata(path string, kind core.Kind) (Metadata, error) {
	m := Metadata{Orientation: 1}
	if kind != core.KindPhoto {
		return m, nil // videos: see Decoders.Metadata
	}
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()

	// The pixel size from the image itself: EXIF's can be the size before
	// an editor cropped it. Go reads the common formats' headers.
	if cfg, _, err := image.DecodeConfig(f); err == nil {
		m.Width, m.Height = cfg.Width, cfg.Height
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return m, err
	}

	ex, exErr := imagemeta.Decode(f)
	if exErr == nil {
		// Only the dates of taking the photo; SelectedDate would fall back to
		// the last time it was saved.
		m.TakenAt = ex.ExifIFD.GetDateTimeOriginal()
		if m.TakenAt.IsZero() {
			m.TakenAt = ex.ExifIFD.GetCreateDate()
		}
		m.Modified = ex.ModifyDate()
		if o := int(ex.IFD0.Orientation); o >= 1 && o <= 8 {
			m.Orientation = o
		}
		if m.Width == 0 || m.Height == 0 {
			m.Width, m.Height = int(ex.ExifIFD.PixelXDimension), int(ex.ExifIFD.PixelYDimension)
		}
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
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			fillFromGoexif(f, &m)
		}
	}

	// Neither Go nor the EXIF knows the size: libvips reads the header of
	// the rest (AVIF, HEIC).
	if m.Width == 0 || m.Height == 0 {
		if w, h, err := vips.Size(path); err == nil {
			m.Width, m.Height = w, h
		}
	}
	if m.Orientation >= 5 { // 5..8 are rotated a quarter turn
		m.Width, m.Height = m.Height, m.Width
	}
	if !Plausible(m.TakenAt) {
		m.TakenAt = time.Time{}
	}
	if !Plausible(m.Modified) {
		m.Modified = time.Time{}
	}
	return m, nil
}

// Plausible reports whether a date read from a file can be when it was
// taken: from 1900 until a couple of days from now (time zones), not a
// camera clock that was never set or set wrong.
func Plausible(t time.Time) bool {
	return t.Year() >= 1900 && t.Before(time.Now().AddDate(0, 0, 2))
}

var goexifFormats = map[string]bool{".jpg": true, ".jpeg": true, ".jpe": true, ".tif": true, ".tiff": true}

// fillFromGoexif fills whatever m is missing from a second EXIF parser.
func fillFromGoexif(r io.Reader, m *Metadata) {
	x, err := goexif.Decode(r)
	if err != nil {
		return
	}
	date := func(n goexif.FieldName) time.Time {
		if tag, err := x.Get(n); err == nil {
			if s, err := tag.StringVal(); err == nil {
				t, _ := time.Parse("2006:01:02 15:04:05", strings.TrimSpace(strings.TrimRight(s, "\x00")))
				return t // wall-clock time, as imagemeta returns it
			}
		}
		return time.Time{}
	}
	if m.TakenAt.IsZero() {
		if m.TakenAt = date(goexif.DateTimeOriginal); m.TakenAt.IsZero() {
			m.TakenAt = date(goexif.DateTimeDigitized)
		}
	}
	if m.Modified.IsZero() {
		m.Modified = date(goexif.DateTime)
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
func cameraName(maker, model string) string {
	switch {
	case model == "":
		return maker
	case maker == "" || strings.HasPrefix(strings.ToLower(model), strings.ToLower(maker)):
		return model
	}
	return maker + " " + model
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
	if t.Day() != n(m[3]) || !Plausible(t) { // 31 February, or the future
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

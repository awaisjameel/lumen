package core

import (
	"regexp"
	"strconv"
	"time"
)

// Plausible reports whether a date read from a file can be when it was
// taken (see PlausibleAt), as of now.
func Plausible(t time.Time) bool { return PlausibleAt(t, time.Now()) }

// PlausibleAt reports whether a date read from a file can be when it was
// taken, as of now: from 1900 (scanned prints carry old dates) until a
// couple of days from now (time zones), and not the zero of a clock that
// was never set: the Unix or QuickTime epoch.
func PlausibleAt(t, now time.Time) bool {
	y, m, d := t.Date()
	h, mi, sec := t.Clock()
	epoch := (y == 1970 || y == 1904) && m == time.January && d == 1 && h == 0 && mi == 0 && sec == 0
	return y >= 1900 && t.Before(now.AddDate(0, 0, 2)) && !epoch
}

// Filenames that carry a date: IMG_20230514_184502.jpg, PXL_20230514_...,
// IMG-20230514-WA0001.jpg (WhatsApp), Screenshot_2023-05-14-18-45-02.png,
// 2023-05-14 18.45.02.jpg (Dropbox), VID_20230514_184502.mp4 ...
var filenameDate = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)\d{2})[-_.]?(0[1-9]|1[0-2])[-_.]?(0[1-9]|[12]\d|3[01])(?:[-_ .T]?([01]\d|2[0-3])[-_.:]?([0-5]\d)[-_.:]?([0-5]\d))?(?:[^0-9]|$)`)

// FilenameDate guesses a capture date from common camera and messenger
// file names. ok is false if the name holds no plausible date.
func FilenameDate(name string) (t time.Time, ok bool) {
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
func GuessDate(name string, mtime time.Time) (wallClock int64, from DateSource) {
	if t, ok := FilenameDate(name); ok {
		return t.Unix(), DateFromFilename // already wall-clock (built in UTC)
	}
	return WallClock(mtime.Local()), DateFromFile
}

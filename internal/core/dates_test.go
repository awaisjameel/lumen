package core

import (
	"testing"
	"time"
)

func TestPlausibleAt(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		t    time.Time
		want bool
	}{
		{time.Date(2024, 5, 1, 9, 30, 0, 0, time.UTC), true},
		{time.Date(1965, 7, 4, 15, 0, 0, 0, time.UTC), true},   // a scanned print
		{time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), false},   // a clock never set
		{time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC), false},   // QuickTime's zero
		{time.Date(1970, 1, 1, 0, 0, 1, 0, time.UTC), true},    // not exactly the zero
		{time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC), false}, // before photography was common
		{now.AddDate(0, 0, 1), true},                           // a time zone ahead
		{now.AddDate(0, 0, 3), false},                          // the future
	} {
		if got := PlausibleAt(c.t, now); got != c.want {
			t.Errorf("PlausibleAt(%v) = %v, want %v", c.t, got, c.want)
		}
	}
}

func TestFilenameDate(t *testing.T) {
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
		got, ok := FilenameDate(c.name)
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

package media

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

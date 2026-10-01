package vips

import "testing"

func TestPixelValidationRejectsOverflowingDimensions(t *testing.T) {
	if err := checkPixels(make([]byte, 12), 1<<62+1, 4, 3); err == nil {
		t.Fatal("overflowing dimensions accepted a short pixel buffer")
	}
}

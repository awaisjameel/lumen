package media

import (
	"image"
	"image/color"
	"testing"

	"lumen/internal/core"
)

func TestAdjustTurnsClockwiseThenFlipsAndLeavesAnUnadjustedPictureAlone(t *testing.T) {
	red, blue := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, red)
	src.Set(1, 0, blue)
	if got := Adjust(src, core.DisplayAdjustment{}); got != image.Image(src) {
		t.Fatal("an unadjusted picture was copied")
	}
	for _, c := range []struct {
		name         string
		a            core.DisplayAdjustment
		w, h         int
		redX, redY   int
		blueX, blueY int
	}{
		{"quarter turn", core.DisplayAdjustment{Rotation: 1}, 1, 2, 0, 0, 0, 1},
		{"half turn", core.DisplayAdjustment{Rotation: 2}, 2, 1, 1, 0, 0, 0},
		{"three quarter turns", core.DisplayAdjustment{Rotation: 3}, 1, 2, 0, 1, 0, 0},
		{"flip", core.DisplayAdjustment{Flip: true}, 2, 1, 1, 0, 0, 0},
		{"half turn then flip", core.DisplayAdjustment{Rotation: 2, Flip: true}, 2, 1, 0, 0, 1, 0},
	} {
		got := Adjust(src, c.a)
		if b := got.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("%s: %dx%d, want %dx%d", c.name, b.Dx(), b.Dy(), c.w, c.h)
			continue
		}
		if got.At(c.redX, c.redY) != color.Color(red) || got.At(c.blueX, c.blueY) != color.Color(blue) {
			t.Errorf("%s: red at (%d,%d) = %v, blue at (%d,%d) = %v", c.name,
				c.redX, c.redY, got.At(c.redX, c.redY), c.blueX, c.blueY, got.At(c.blueX, c.blueY))
		}
	}
}

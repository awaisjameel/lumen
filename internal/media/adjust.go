package media

import (
	"bytes"
	"image"
	"image/jpeg"

	"lumen/internal/core"
)

// Adjust applies a display rotation and flip to presentation pixels only.
// Model thumbnails and face crops keep their canonical coordinates and never
// pass through this.
func Adjust(im image.Image, a core.DisplayAdjustment) image.Image {
	if a.Rotation == 0 && !a.Flip {
		return im
	}
	w, h := im.Bounds().Dx(), im.Bounds().Dy()
	ow, oh := w, h
	if a.Rotation%2 != 0 {
		ow, oh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	for y := range h {
		for x := range w {
			tx, ty := x, y
			switch a.Rotation {
			case 1:
				tx, ty = h-1-y, x
			case 2:
				tx, ty = w-1-x, h-1-y
			case 3:
				tx, ty = y, w-1-x
			}
			if a.Flip {
				tx = ow - 1 - tx
			}
			out.Set(tx, ty, im.At(x+im.Bounds().Min.X, y+im.Bounds().Min.Y))
		}
	}
	return out
}

// AdjustJPEG is Adjust for a cached JPEG thumbnail.
func AdjustJPEG(data []byte, a core.DisplayAdjustment) ([]byte, error) {
	if a.Rotation == 0 && !a.Flip {
		return data, nil
	}
	im, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, Adjust(im, a), &jpeg.Options{Quality: 92})
	return buf.Bytes(), err
}

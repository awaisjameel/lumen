package media

import "lumen/internal/media/vips"

// EncodeImage encodes trusted, bounded pixels from the frame decoder.
func EncodeImage(im Image, quality int) ([]byte, error) {
	return vips.JPEG(im.Pix, im.W, im.H, quality)
}

package media

// Go's image decoders, registered (by importing them for their side
// effect) for image.DecodeConfig, sizes when a file has no EXIF, and
// image.Decode, BMP thumbnails.
import (
	_ "image/gif"  // registers GIF
	_ "image/jpeg" // registers JPEG
	_ "image/png"  // registers PNG

	_ "golang.org/x/image/bmp"  // registers BMP
	_ "golang.org/x/image/tiff" // registers TIFF
	_ "golang.org/x/image/webp" // registers WebP
)

package media

// Go's image decoders, registered for image.DecodeConfig (sizes when a file
// has no EXIF) and image.Decode (BMP thumbnails).
import (
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

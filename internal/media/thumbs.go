package media

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanoberholster/imagemeta/imagehash"

	"lumen/internal/core"
	"lumen/internal/media/vips"
	"lumen/internal/media/wic"
)

// Versions recorded with each finished job of the stages this package
// serves. Change one when that stage's output would change (a better decoder,
// a fix); the indexer then re-runs just that stage.
const (
	MetadataVersion  = "imagemeta-1.1+goexif+vips-1"
	ThumbnailVersion = "vips-8.18-1"
)

// ErrUnsupported means this machine cannot decode the file: a video frame
// before video support, or HEIC with neither the HEVC extension nor FFmpeg.
// Jobs that hit it are marked skipped, not failed, and re-run when the
// thumbnail stage's version changes.
var ErrUnsupported = errors.New("format not supported")

// maxPixels guards the in-memory decoders against decompression bombs.
// (libvips streams, so it needs no such limit.)
const maxPixels = 100_000_000

// hashSide is the square the perceptual hash is computed from.
const hashSide = 64

var sizes = vips.Sizes{Large: core.ThumbLarge, Small: core.ThumbSmall, Square: hashSide}

// Thumbs is one decode's worth of output.
type Thumbs struct {
	Small, Large []byte // JPEG, longest edge core.ThumbSmall / core.ThumbLarge
	PHash        uint64 // 64-bit perceptual hash of the upright image
}

// Thumbnailer turns an original file into upright thumbnails.
type Thumbnailer interface {
	Thumbnail(path string, kind core.Kind, orientation int) (Thumbs, error)
}

// Decoders is the production Thumbnailer. Everything ends in the same libvips
// pipeline (resize, JPEG, hash square); only decoding differs by format:
//
//   - HEIC/HEIF: Windows' own decoder (WIC), else FFmpeg
//   - BMP: Go (the libvips build ships without ImageMagick)
//   - everything else (JPEG, PNG, WebP, GIF, TIFF, AVIF): libvips, shrinking on load
type Decoders struct {
	FFmpeg string // ffmpeg.exe, for HEIC when WIC can't decode it; "" = none
}

func (d Decoders) Thumbnail(path string, kind core.Kind, orientation int) (Thumbs, error) {
	if kind != core.KindPhoto {
		return Thumbs{}, ErrUnsupported // video frames arrive with FFmpeg video support
	}
	var t vips.Thumbs
	var err error
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".heic", ".heif":
		t, err = d.heif(path, orientation)
	case ".bmp":
		t, err = goDecode(path)
	default:
		t, err = vips.ThumbnailFile(path, sizes)
	}
	if err != nil {
		return Thumbs{}, err
	}
	ph, err := perceptualHash(t.Square)
	if err != nil {
		return Thumbs{}, err
	}
	return Thumbs{Small: t.Small, Large: t.Large, PHash: ph}, nil
}

// heif decodes HEIC/HEIF. Neither decoder applies the file's rotation, so
// the EXIF orientation (which matches it in camera files) is applied here.
func (d Decoders) heif(path string, orientation int) (vips.Thumbs, error) {
	pix, w, h, werr := wic.Decode(path, core.ThumbLarge)
	if werr == nil {
		return vips.ThumbnailPixels(pix, w, h, 4, orientation, sizes)
	}
	if d.FFmpeg == "" {
		return vips.Thumbs{}, ErrUnsupported
	}
	pix, w, h, ferr := ffmpegDecode(d.FFmpeg, path, core.ThumbLarge)
	if ferr != nil {
		return vips.Thumbs{}, fmt.Errorf("decode HEIC: %v; %w", werr, ferr)
	}
	return vips.ThumbnailPixels(pix, w, h, 3, orientation, sizes)
}

// goDecode reads formats libvips lacks with Go's image packages. They carry
// no orientation.
func goDecode(path string) (vips.Thumbs, error) {
	f, err := os.Open(path)
	if err != nil {
		return vips.Thumbs{}, err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 256<<10)
	cfg, _, err := image.DecodeConfig(br)
	if err != nil {
		return vips.Thumbs{}, fmt.Errorf("read image header: %w", err)
	}
	if cfg.Width*cfg.Height > maxPixels {
		return vips.Thumbs{}, fmt.Errorf("image is too large (%dx%d)", cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return vips.Thumbs{}, err
	}
	br.Reset(f)
	src, _, err := image.Decode(br)
	if err != nil {
		return vips.Thumbs{}, fmt.Errorf("decode: %w", err)
	}
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	return vips.ThumbnailPixels(rgba.Pix, b.Dx(), b.Dy(), 4, 1, sizes)
}

// perceptualHash is the 64-bit pHash of a hashSide x hashSide RGB square.
func perceptualHash(rgb []byte) (uint64, error) {
	img := image.NewRGBA(image.Rect(0, 0, hashSide, hashSide))
	for i, j := 0, 0; i+2 < len(rgb); i, j = i+3, j+4 {
		img.Pix[j], img.Pix[j+1], img.Pix[j+2], img.Pix[j+3] = rgb[i], rgb[i+1], rgb[i+2], 255
	}
	ph, err := imagehash.NewPHash64(img)
	if err != nil {
		return 0, fmt.Errorf("perceptual hash: %w", err)
	}
	return uint64(ph), nil
}

// ModelInput returns an image file squashed to side x side 8-bit RGB, the
// input image models expect. It is meant for thumbnails, which are already
// upright JPEGs, so it is fast.
func ModelInput(path string, side int) ([]byte, error) { return vips.Square(path, side) }

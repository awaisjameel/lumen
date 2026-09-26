package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
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

// Image is decoded 8-bit RGB pixels, row by row.
type Image struct {
	Pix  []byte // W x H x 3
	W, H int
}

// Decoder turns original files into upright thumbnails, and into pixels for
// the models. ctx cancels a slow external decoder.
type Decoder interface {
	Thumbnail(ctx context.Context, path string, kind core.Kind, orientation int) (Thumbs, error)
	Decode(ctx context.Context, path string, kind core.Kind, orientation, side int) (Image, error)
}

// Decoders is the production Decoder.
// Everything ends in the same libvips pipeline (rotate, resize, sRGB); only
// decoding differs by format:
//
//   - HEIC/HEIF: Windows' own decoder (WIC), else FFmpeg
//   - BMP: Go (the libvips build ships without ImageMagick)
//   - everything else (JPEG, PNG, WebP, GIF, TIFF, AVIF): libvips, shrinking on load
//
// Thumbnails and Decode share that routing, so a face box found in a Decode
// image lands on the same spot of the thumbnail.
type Decoders struct {
	FFmpeg string // ffmpeg.exe, for HEIC when WIC can't decode it; "" = none
}

func (d Decoders) Thumbnail(ctx context.Context, path string, kind core.Kind, orientation int) (Thumbs, error) {
	src, err := d.open(ctx, path, kind, orientation, core.ThumbLarge)
	if err != nil {
		return Thumbs{}, err
	}
	var t vips.Thumbs
	if src.file != "" {
		t, err = vips.ThumbnailFile(src.file, sizes)
	} else {
		t, err = vips.ThumbnailPixels(src.pix, src.w, src.h, src.bands, src.orientation, sizes)
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

// Decode returns a photo upright as 8-bit RGB, shrunk so its long edge is at
// most side (never enlarged), for the models that need more detail than the
// thumbnails hold.
func (d Decoders) Decode(ctx context.Context, path string, kind core.Kind, orientation, side int) (Image, error) {
	src, err := d.open(ctx, path, kind, orientation, side)
	if err != nil {
		return Image{}, err
	}
	var im vips.RGB
	if src.file != "" {
		im, err = vips.DecodeFile(src.file, side)
	} else {
		im, err = vips.DecodePixels(src.pix, src.w, src.h, src.bands, src.orientation, side)
	}
	return Image{Pix: im.Pix, W: im.W, H: im.H}, err
}

// source is a photo ready for libvips: a file it decodes itself (shrinking
// on load and applying the EXIF orientation), or pixels decoded elsewhere
// with the orientation still to apply.
type source struct {
	file        string
	pix         []byte // 8-bit, bands per pixel
	w, h, bands int
	orientation int
}

// open picks the decoder for path. Pixels decoded outside libvips are at
// most maxSide on their long edge where the decoder can shrink.
func (d Decoders) open(ctx context.Context, path string, kind core.Kind, orientation, maxSide int) (source, error) {
	if kind != core.KindPhoto {
		return source{}, ErrUnsupported // video frames arrive with FFmpeg video support
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".heic", ".heif":
		return d.heif(ctx, path, orientation, maxSide)
	case ".bmp":
		return goDecode(path)
	}
	return source{file: path}, nil
}

// heif decodes HEIC/HEIF. Neither decoder applies the file's rotation, so
// the EXIF orientation (which matches it in camera files) is applied after.
func (d Decoders) heif(ctx context.Context, path string, orientation, maxSide int) (source, error) {
	pix, w, h, werr := wic.Decode(path, maxSide)
	if werr == nil {
		return source{pix: pix, w: w, h: h, bands: 4, orientation: orientation}, nil
	}
	if d.FFmpeg == "" {
		return source{}, ErrUnsupported
	}
	pix, w, h, ferr := ffmpegDecode(ctx, d.FFmpeg, path, maxSide)
	if ferr != nil {
		return source{}, fmt.Errorf("decode HEIC: %v; %w", werr, ferr)
	}
	return source{pix: pix, w: w, h: h, bands: 3, orientation: orientation}, nil
}

// goDecode reads formats libvips lacks with Go's image packages. They carry
// no orientation.
func goDecode(path string) (source, error) {
	f, err := os.Open(path)
	if err != nil {
		return source{}, err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 256<<10)
	cfg, _, err := image.DecodeConfig(br)
	if err != nil {
		return source{}, fmt.Errorf("read image header: %w", err)
	}
	if cfg.Width*cfg.Height > maxPixels {
		return source{}, fmt.Errorf("image is too large (%dx%d)", cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return source{}, err
	}
	br.Reset(f)
	img, _, err := image.Decode(br)
	if err != nil {
		return source{}, fmt.Errorf("decode: %w", err)
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	return source{pix: rgba.Pix, w: b.Dx(), h: b.Dy(), bands: 4, orientation: 1}, nil
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

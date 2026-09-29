package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"
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
// Photos and videos have their own, as different code serves them.
const (
	MetadataVersion       = "imagemeta-1.1+goexif+vips-3" // capture dates only (not modify dates); header sizes
	ThumbnailVersion      = "vips-8.18-2"                 // colour profiles converted to sRGB
	VideoMetadataVersion  = "ffprobe-9.0-1"
	VideoThumbnailVersion = "ffmpeg-9.0+vips-8.18-1"
)

// ErrUnsupported means this machine cannot decode the file: HEIC with
// neither the HEVC extension nor FFmpeg, a video without FFmpeg, or a
// "video" with no picture. Jobs that hit it are marked skipped, not failed,
// and re-run when their stage's version changes.
var ErrUnsupported = errors.New("format not supported")

// maxPixels guards the in-memory decoders against decompression bombs.
// (libvips streams, so it needs no such limit.)
const maxPixels = 100_000_000

// hashSide is the square the perceptual hash is computed from.
const hashSide = 64

var sizes = vips.Sizes{Large: core.ThumbLarge, Small: core.ThumbSmall, Square: hashSide}

// JPEG qualities of what Go encodes: video frames as the large thumbnail is
// (vips.go encodes that at 85, the small one at 80), sprites a little lower,
// as they only flash by while hovering.
const (
	jpegQuality   = 85
	spriteQuality = 75
)

// Thumbs is one decode's worth of output.
type Thumbs struct {
	Small, Large []byte // JPEG, longest edge core.ThumbSmall / core.ThumbLarge
	PHash        uint64 // 64-bit perceptual hash of the upright image
	Sprite       []byte // a video's hover preview (see core.SpriteCols); nil for photos
}

// Image is decoded 8-bit RGB pixels, row by row.
type Image struct {
	Pix  []byte // W x H x 3
	W, H int
}

// Decoder reads original files: what they say about themselves, upright
// thumbnails, and pixels for the models. ctx cancels a slow external decoder.
type Decoder interface {
	// Metadata reads capture date, size, place and the like.
	Metadata(ctx context.Context, path string, kind core.Kind) (Metadata, error)
	// Thumbnail makes both thumbnail sizes and the perceptual hash, and a
	// video's sprite.
	Thumbnail(ctx context.Context, path string, kind core.Kind, orientation int) (Thumbs, error)
	// Decode returns a photo upright, at most side on its long edge.
	Decode(ctx context.Context, path string, kind core.Kind, orientation, side int) (Image, error)
	// Frames samples up to n frames spread over a video (see Decoders.Frames).
	Frames(ctx context.Context, path string, n, side int, fn func(Frame) error) error
	// FrameAt returns the video frame at ms, at most side on its long edge.
	FrameAt(ctx context.Context, path string, at int64, side int) (Image, error)
}

// Decoders is the production Decoder.
// Everything ends in the same libvips pipeline (rotate, resize, sRGB); only
// decoding differs by format:
//
//   - HEIC/HEIF: Windows' own decoder (WIC), else FFmpeg
//   - BMP: Go (the libvips build ships without ImageMagick)
//   - videos: FFmpeg, which also turns them upright
//   - everything else (JPEG, PNG, WebP, GIF, TIFF, AVIF): libvips, shrinking on load
//
// Thumbnails and Decode share that routing, so a face box found in a Decode
// image lands on the same spot of the thumbnail.
type Decoders struct {
	FFmpeg  string // ffmpeg.exe, for videos, and HEIC when WIC can't decode it; "" = none
	FFprobe string // ffprobe.exe, for videos; "" = none
}

// Metadata reads what a file says about itself: EXIF for photos, the
// container's tags for videos.
func (d Decoders) Metadata(ctx context.Context, path string, kind core.Kind) (Metadata, error) {
	if kind != core.KindVideo {
		return ReadMetadata(path, kind)
	}
	v, err := d.probe(ctx, path)
	if err != nil {
		return Metadata{Orientation: 1}, err
	}
	return Metadata{
		TakenAt: v.TakenAt, Width: v.Width, Height: v.Height, Orientation: 1, Camera: v.Camera, Lat: v.Lat, Lon: v.Lon,
		Duration: v.Duration, VideoCodec: v.Codec, AudioCodec: v.Audio,
	}, nil
}

// Thumbnail decodes a file once into both thumbnail sizes and its
// perceptual hash, upright. A video's come from its first clear frame, and
// it also gets a sprite.
func (d Decoders) Thumbnail(ctx context.Context, path string, kind core.Kind, orientation int) (Thumbs, error) {
	if kind == core.KindVideo {
		return d.videoThumbnail(ctx, path)
	}
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
// thumbnails hold. Videos are read with Frames instead.
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
		return source{}, ErrUnsupported // videos: see Frames
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
		return source{}, fmt.Errorf("decode HEIC: %w; %w", werr, ferr)
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
	// Straight (not premultiplied) alpha, as libvips flattens it and WIC
	// delivers it: premultiplying first would darken see-through pixels twice.
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
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

// A video's poster is its first frame with at least posterContrast (the
// standard deviation of its brightness, 0..255): black or faded openings are
// skipped. A video with none uses its most contrasted frame.
const posterContrast = 16

// videoThumbnail makes a video's thumbnails from one pass over its frames:
// the poster, and the sprite of core.SpriteCols x core.SpriteRows frames.
func (d Decoders) videoThumbnail(ctx context.Context, path string) (Thumbs, error) {
	var tiles []tile
	var poster Image
	best, found := -1.0, false
	v, err := d.frames(ctx, path, core.SpriteCols*core.SpriteRows, core.ThumbLarge, func(f Frame) error {
		small, err := vips.DecodePixels(f.Pix, f.W, f.H, 3, 1, core.ThumbSmall)
		if err != nil {
			return err
		}
		tiles = append(tiles, tile{at: f.At, RGB: small})
		if c := contrast(small); !found && c > best {
			poster, best, found = f.Image, c, c >= posterContrast
		}
		return nil
	})
	if err != nil {
		return Thumbs{}, err
	}
	if len(tiles) == 0 {
		return Thumbs{}, errors.New("ffmpeg: the video has no frames")
	}
	t, err := vips.ThumbnailPixels(poster.Pix, poster.W, poster.H, 3, 1, sizes)
	if err != nil {
		return Thumbs{}, err
	}
	ph, err := perceptualHash(t.Square)
	if err != nil {
		return Thumbs{}, err
	}
	sprite, err := makeSprite(tiles, v.Duration)
	if err != nil {
		return Thumbs{}, err
	}
	return Thumbs{Small: t.Small, Large: t.Large, PHash: ph, Sprite: sprite}, nil
}

// tile is one small frame of a video, for its sprite.
type tile struct {
	at int64 // ms
	vips.RGB
}

// contrast is the standard deviation of an image's brightness.
func contrast(im vips.RGB) float64 {
	var sum, sq float64
	n := 0
	for i := 0; i+2 < len(im.Pix); i += 3 {
		y := 0.299*float64(im.Pix[i]) + 0.587*float64(im.Pix[i+1]) + 0.114*float64(im.Pix[i+2])
		sum += y
		sq += y * y
		n++
	}
	if n == 0 {
		return 0
	}
	mean := sum / float64(n)
	return math.Sqrt(max(0, sq/float64(n)-mean*mean))
}

// makeSprite lays frames out as a video's sprite: each cell shows the frame
// nearest its point in the video (cells are spread evenly over duration), so
// a video with few keyframes repeats some. All tiles have one size, as they
// come from one pass.
func makeSprite(tiles []tile, duration int64) ([]byte, error) {
	cols, rows := core.SpriteCols, core.SpriteRows
	tw, th := tiles[0].W, tiles[0].H
	w := cols * tw
	pix := make([]byte, w*rows*th*3)
	for cell := range cols * rows {
		at := (2*int64(cell) + 1) * duration / int64(2*cols*rows)
		t := tiles[0]
		for _, c := range tiles[1:] {
			if c.W == tw && c.H == th && abs64(c.at-at) < abs64(t.at-at) {
				t = c
			}
		}
		x0, y0 := (cell%cols)*tw, (cell/cols)*th
		for y := range th {
			copy(pix[((y0+y)*w+x0)*3:], t.Pix[y*tw*3:(y+1)*tw*3])
		}
	}
	return vips.JPEG(pix, w, rows*th, spriteQuality)
}

func abs64(x int64) int64 { return max(x, -x) }

// ModelInput returns an image file squashed to side x side 8-bit RGB, the
// input image models expect. It is meant for thumbnails, which are already
// upright JPEGs, so it is fast.
func ModelInput(path string, side int) ([]byte, error) { return vips.Square(path, side) }

// ModelInputPixels does the same for pixels, e.g. a video frame.
func ModelInputPixels(im Image, side int) ([]byte, error) {
	return vips.SquarePixels(im.Pix, im.W, im.H, side)
}

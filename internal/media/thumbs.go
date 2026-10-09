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
	"strconv"

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
	MetadataVersion       = "imagemeta-1.1+goexif+vips-5" // JPEG XL's EXIF is extracted by the bundled decoder.
	ThumbnailVersion      = "vips-8.18-4"                 // JPEG XL keeps colour profiles and straight alpha.
	VideoMetadataVersion  = "ffprobe-9.0-2"
	VideoThumbnailVersion = "ffmpeg-9.0+vips-8.18-2" // frame times are relative to playback, including offset containers
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
	// Preview: these are a RAW file's embedded preview, as its sensor data
	// could not be developed.
	Preview bool
}

// RGBA is the picture as Go's image package reads it.
func (im Image) RGBA() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, im.W, im.H))
	for i, j := 0, 0; i+2 < len(im.Pix) && j+3 < len(img.Pix); i, j = i+3, j+4 {
		img.Pix[j], img.Pix[j+1], img.Pix[j+2], img.Pix[j+3] = im.Pix[i], im.Pix[i+1], im.Pix[i+2], 255
	}
	return img
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

// Inspector is a Decoder that can also read what a file holds beyond the
// indexed metadata: its streams, lens, exposure and colour profile.
type Inspector interface {
	Inspect(ctx context.Context, path string, kind core.Kind) (core.MediaInspection, error)
}

// Subtitler is a Decoder that can also turn a video's text subtitle stream
// into WebVTT, starting at ms.
type Subtitler interface {
	Subtitles(ctx context.Context, path string, index int, at int64, out io.Writer) error
}

// Decoders is the production Decoder.
// Everything ends in the same libvips pipeline (rotate, resize, sRGB); only
// decoding differs by format:
//
//   - HEIC/HEIF: Windows' own decoder (WIC), else FFmpeg
//   - camera RAW: the LibRaw worker (see raw.go)
//   - JPEG XL: djxl to PNG, then the common libvips pipeline (FFmpeg for one too large for that)
//   - BMP: Go (the libvips build ships without ImageMagick)
//   - videos: FFmpeg, which also turns them upright
//   - everything else (JPEG, PNG, WebP, GIF, TIFF, AVIF): libvips, shrinking on load
//
// Thumbnails and Decode share that routing, so a face box found in a Decode
// image lands on the same spot of the thumbnail.
type Decoders struct {
	FFmpeg  string // ffmpeg.exe, for videos, HEIC when WIC can't decode it, and a JPEG XL too large for djxl's bounded output; "" = none
	FFprobe string // ffprobe.exe, for videos and the size of a JPEG XL; "" = none
	Raw     string // lumen-raw.exe, for camera RAW; "" = none
	JXL     string // djxl.exe, preserving JPEG XL's profile, alpha and metadata; "" = none

	// foreground runs the tools at normal priority: a viewer is waiting.
	foreground bool
}

// Metadata reads what a file says about itself: EXIF for photos, the
// container's tags for videos.
func (d Decoders) Metadata(ctx context.Context, path string, kind core.Kind) (Metadata, error) {
	if kind != core.KindVideo {
		switch core.FamilyOf(path) {
		case core.FamilyRAW:
			return d.rawMetadata(ctx, path)
		case core.FamilyJXL:
			return d.jxlMetadata(ctx, path)
		default:
			return ReadMetadata(path, kind)
		}
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
		t, err = vips.ThumbnailPixels(src.pixels, sizes)
	}
	if err != nil {
		return Thumbs{}, fmt.Errorf("thumbnail %s: %w", path, err)
	}
	ph, err := perceptualHash(t.Square)
	if err != nil {
		return Thumbs{}, err
	}
	return Thumbs{Small: t.Small, Large: t.Large, PHash: ph}, nil
}

// ThumbsFromImage makes both thumbnail sizes and the perceptual hash from a
// photo Decode returned (upright sRGB, at least core.ThumbLarge where the
// original is), so a photo also wanted as pixels is decoded once.
func ThumbsFromImage(im Image) (Thumbs, error) {
	t, err := vips.ThumbnailPixels(vips.RGBPixels(im.Pix, im.W, im.H), sizes)
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
	return d.DecodePage(ctx, path, kind, orientation, side, 0)
}

// DecodePage is Decode for one page of a file, counted from 0. Only a TIFF
// has pages after its first; the index and the models read the first.
func (d Decoders) DecodePage(ctx context.Context, path string, kind core.Kind, orientation, side, page int) (Image, error) {
	src, err := d.open(ctx, path, kind, orientation, side)
	if err != nil {
		return Image{}, err
	}
	if page != 0 {
		if page < 0 || src.file == "" || core.FamilyOf(path) != core.FamilyTIFF {
			return Image{}, ErrUnsupported
		}
		// libvips takes a loader's options after the file's name.
		src.file += "[page=" + strconv.Itoa(page) + "]"
	}
	var im vips.RGB
	if src.file != "" {
		im, err = vips.DecodeFile(src.file, side)
	} else {
		im, err = vips.DecodePixels(src.pixels, side)
	}
	if err != nil {
		return Image{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return Image{Pix: im.Pix, W: im.W, H: im.H, Preview: src.preview}, nil
}

// source is a photo ready for libvips: a file it decodes itself (shrinking
// on load and applying the EXIF orientation), or pixels decoded elsewhere
// with the orientation still to apply.
type source struct {
	file    string
	pixels  vips.Pixels
	preview bool // a RAW file's embedded preview (see raw.go)
}

// open picks the decoder for path. Pixels decoded outside libvips are at
// most maxSide on their long edge where the decoder can shrink.
func (d Decoders) open(ctx context.Context, path string, kind core.Kind, orientation, maxSide int) (source, error) {
	if kind != core.KindPhoto {
		return source{}, ErrUnsupported // videos: see Frames
	}
	switch core.FamilyOf(path) {
	case core.FamilyHEIF:
		return d.heif(ctx, path, orientation, maxSide)
	case core.FamilyRAW:
		return d.rawSource(ctx, path, maxSide)
	case core.FamilyJXL:
		return d.jxl(ctx, path, maxSide)
	case core.FamilyBMP:
		return goDecode(path)
	default:
		return source{file: path}, nil
	}
}

// heif decodes HEIC/HEIF. Neither decoder applies the file's rotation, so
// the EXIF orientation (which matches it in camera files) is applied after.
func (d Decoders) heif(ctx context.Context, path string, orientation, maxSide int) (source, error) {
	pix, w, h, werr := wic.Decode(path, maxSide)
	if werr == nil {
		return source{pixels: vips.Pixels{Pix: pix, W: w, H: h, Bands: 4, Orientation: orientation}}, nil // WIC converts to sRGB
	}
	if d.FFmpeg == "" {
		return source{}, ErrUnsupported
	}
	pix, w, h, ferr := ffmpegDecode(ctx, d.FFmpeg, path, maxSide)
	if ferr != nil {
		return source{}, fmt.Errorf("decode HEIC: %w; %w", werr, ferr)
	}
	// FFmpeg leaves the pixels in the file's profile (iPhones: Display P3),
	// which libvips reads from the header and converts from.
	icc, err := vips.Profile(path)
	if err != nil {
		icc = nil // as sRGB, as before profiles were read
	}
	return source{pixels: vips.Pixels{Pix: pix, W: w, H: h, Bands: 3, Orientation: orientation, ICC: icc}}, nil
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
	return source{pixels: straightPixels(img, 1)}, nil
}

// straightPixels is a picture Go decoded, for libvips, with the EXIF
// orientation still to apply.
func straightPixels(img image.Image, orientation int) vips.Pixels {
	b := img.Bounds()
	// Straight (not premultiplied) alpha, as libvips flattens it and WIC
	// delivers it: premultiplying first would darken see-through pixels twice.
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	return vips.Pixels{Pix: rgba.Pix, W: b.Dx(), H: b.Dy(), Bands: 4, Orientation: orientation}
}

// perceptualHash is the 64-bit pHash of a hashSide x hashSide RGB square.
func perceptualHash(rgb []byte) (uint64, error) {
	ph, err := imagehash.NewPHash64(Image{Pix: rgb, W: hashSide, H: hashSide}.RGBA())
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
		small, err := vips.DecodePixels(vips.RGBPixels(f.Pix, f.W, f.H), core.ThumbSmall)
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
	t, err := vips.ThumbnailPixels(vips.RGBPixels(poster.Pix, poster.W, poster.H), sizes)
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

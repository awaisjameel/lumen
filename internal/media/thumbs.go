package media

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"

	// Decoders registered with image.Decode.
	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	xdraw "golang.org/x/image/draw"

	"github.com/evanoberholster/imagemeta/imagehash"

	"lumen/internal/core"
)

// ErrUnsupported means this build cannot decode the format yet (HEIC and AVIF
// arrive with libvips/libheif, video frames with FFmpeg). Jobs that hit it are
// marked skipped, not failed, and re-run when a capable decoder ships.
var ErrUnsupported = errors.New("format not supported yet")

// maxPixels guards against decompression bombs and absurd panoramas.
const maxPixels = 200_000_000

// Thumbs is one decode's worth of output.
type Thumbs struct {
	Small, Large []byte // JPEG, longest edge core.ThumbSmall / core.ThumbLarge
	PHash        uint64 // 64-bit perceptual hash of the oriented image
}

// Thumbnailer turns an original file into thumbnails. The pure-Go version below
// is the Phase 1 baseline; a libvips version (shrink-on-load, HEIC) can replace
// it without touching the indexer.
type Thumbnailer interface {
	Thumbnail(path string, kind core.Kind, orientation int) (Thumbs, error)
}

// GoThumbnailer decodes with Go's image packages. JPEG, PNG, GIF, WebP, BMP, TIFF.
type GoThumbnailer struct{}

var goDecodable = map[string]bool{
	".jpg": true, ".jpeg": true, ".jpe": true, ".png": true, ".gif": true,
	".webp": true, ".bmp": true, ".tif": true, ".tiff": true,
}

func (GoThumbnailer) Thumbnail(path string, kind core.Kind, orientation int) (Thumbs, error) {
	if kind != core.KindPhoto || !goDecodable[strings.ToLower(filepath.Ext(path))] {
		return Thumbs{}, ErrUnsupported
	}
	f, err := os.Open(path)
	if err != nil {
		return Thumbs{}, err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 256<<10)

	cfg, _, err := image.DecodeConfig(br)
	if err != nil {
		return Thumbs{}, fmt.Errorf("read image header: %w", err)
	}
	if cfg.Width*cfg.Height > maxPixels {
		return Thumbs{}, fmt.Errorf("image is too large (%dx%d)", cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return Thumbs{}, err
	}
	br.Reset(f)
	src, _, err := image.Decode(br)
	if err != nil {
		return Thumbs{}, fmt.Errorf("decode: %w", err)
	}
	return makeThumbs(src, orientation)
}

func makeThumbs(src image.Image, orientation int) (Thumbs, error) {
	large := resize(src, core.ThumbLarge)
	small := resize(large, core.ThumbSmall)
	large, small = orient(large, orientation), orient(small, orientation)

	var t Thumbs
	var err error
	if t.Large, err = encodeJPEG(large, 85); err != nil {
		return t, err
	}
	if t.Small, err = encodeJPEG(small, 80); err != nil {
		return t, err
	}
	sq := image.NewRGBA(image.Rect(0, 0, 64, 64))
	xdraw.BiLinear.Scale(sq, sq.Bounds(), small, small.Bounds(), xdraw.Src, nil)
	ph, err := imagehash.NewPHash64(sq)
	if err != nil {
		return t, fmt.Errorf("perceptual hash: %w", err)
	}
	t.PHash = uint64(ph)
	return t, nil
}

// resize scales src so its longest edge is at most maxEdge, never upscaling.
// Big JPEGs are first box-shrunk by a whole factor in YCbCr space (cheap), then
// finished with Catmull-Rom (sharp), which is much faster than Catmull-Rom alone.
func resize(src image.Image, maxEdge int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	tw, th := fit(w, h, maxEdge)

	if ycc, ok := src.(*image.YCbCr); ok && b.Min == (image.Point{}) {
		if f := max(w, h) / (2 * maxEdge); f >= 2 {
			src = shrinkYCbCr(ycc, f)
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	if tw == w && th == h {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
		return dst
	}
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst
}

func fit(w, h, maxEdge int) (int, int) {
	if w <= maxEdge && h <= maxEdge {
		return w, h
	}
	if w >= h {
		return maxEdge, max(1, h*maxEdge/w)
	}
	return max(1, w*maxEdge/h), maxEdge
}

// shrinkYCbCr averages f×f blocks of each plane separately, keeping the
// chroma subsampling, so nothing is converted to RGB at full resolution.
func shrinkYCbCr(src *image.YCbCr, f int) *image.YCbCr {
	w, h := src.Rect.Dx()/f, src.Rect.Dy()/f
	dst := image.NewYCbCr(image.Rect(0, 0, w, h), src.SubsampleRatio)
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	shrinkPlane(dst.Y, dst.YStride, w, h, src.Y, src.YStride, sw, sh, f)
	scw, sch := chromaSize(src.SubsampleRatio, sw, sh)
	dcw, dch := chromaSize(dst.SubsampleRatio, w, h)
	shrinkPlane(dst.Cb, dst.CStride, dcw, dch, src.Cb, src.CStride, scw, sch, f)
	shrinkPlane(dst.Cr, dst.CStride, dcw, dch, src.Cr, src.CStride, scw, sch, f)
	return dst
}

func shrinkPlane(dst []byte, dstStride, dw, dh int, src []byte, srcStride, sw, sh, f int) {
	for y := 0; y < dh; y++ {
		y0, y1 := y*f, min((y+1)*f, sh)
		for x := 0; x < dw; x++ {
			x0, x1 := x*f, min((x+1)*f, sw)
			sum, n := 0, 0
			for yy := y0; yy < y1; yy++ {
				row := src[yy*srcStride:]
				for xx := x0; xx < x1; xx++ {
					sum += int(row[xx])
				}
				n += x1 - x0
			}
			if n > 0 {
				dst[y*dstStride+x] = byte((sum + n/2) / n)
			}
		}
	}
}

func chromaSize(r image.YCbCrSubsampleRatio, w, h int) (int, int) {
	switch r {
	case image.YCbCrSubsampleRatio422:
		return (w + 1) / 2, h
	case image.YCbCrSubsampleRatio420:
		return (w + 1) / 2, (h + 1) / 2
	case image.YCbCrSubsampleRatio440:
		return w, (h + 1) / 2
	case image.YCbCrSubsampleRatio411:
		return (w + 3) / 4, h
	case image.YCbCrSubsampleRatio410:
		return (w + 3) / 4, (h + 1) / 2
	}
	return w, h
}

// orient applies an EXIF orientation (1..8) so the image displays upright.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			si, di := src.PixOffset(x, y), dst.PixOffset(dx, dy)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ThumbPath is where a thumbnail lives in the cache, fanned out over 256
// sub-folders so no folder holds hundreds of thousands of files.
func ThumbPath(dir string, id int64, size int) string {
	return filepath.Join(dir, fmt.Sprint(size), fmt.Sprintf("%02x", id%256), fmt.Sprintf("%d.jpg", id))
}

// WriteThumbs saves both thumbnails for an asset, atomically per file.
func WriteThumbs(dir string, id int64, t Thumbs) error {
	for _, v := range []struct {
		size int
		data []byte
	}{{core.ThumbSmall, t.Small}, {core.ThumbLarge, t.Large}} {
		p := ThumbPath(dir, id, v.size)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		tmp := p + ".tmp"
		if err := os.WriteFile(tmp, v.data, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, p); err != nil {
			return err
		}
	}
	return nil
}

// Package vips wraps the few libvips operations Lumen uses. Each call is one
// C function that does the whole job (decode, resize, encode) so pixels never
// cross into Go except where Go needs them.
//
// libvips comes from deps/ (scripts/deps.sh): headers and the import library
// at build time, libvips-42.dll beside the executable at run time.
package vips

/*
#cgo CFLAGS: -I${SRCDIR}/../../../deps/vips/include -I${SRCDIR}/../../../deps/vips/include/glib-2.0 -I${SRCDIR}/../../../deps/vips/lib/glib-2.0/include
#cgo LDFLAGS: -L${SRCDIR}/../../../deps/vips/lib -l:libvips.lib
#include <stdlib.h>
#include <string.h>
#include <vips/vips.h>

typedef struct {
	void *large; size_t large_len; // JPEG
	void *small; size_t small_len; // JPEG
	void *hash;  size_t hash_len;  // side x side RGB
} lumen_thumbs;

static int lumen_init(void) {
	if (VIPS_INIT("lumen")) return -1;
	vips_concurrency_set(1); // the indexer runs one file per worker instead
	vips_cache_set_max(0);   // batch work never reuses an operation
	return 0;
}

// lumen_error takes libvips' error log and clears it in one step. The log is
// shared by every thread, so while several files fail at once one may take
// another's message and leave that one none (see lastError).
static char *lumen_error(void) {
	return vips_error_buffer_copy();
}

// to_srgb8 returns in as 8-bit sRGB with exactly 3 bands, transparency
// flattened onto white. An embedded colour profile (Display P3 from
// iPhones, Adobe RGB from cameras) is converted from, as the WebView and the
// AI read the result as sRGB. It always consumes in.
static VipsImage *to_srgb8(VipsImage *in) {
	VipsImage *t = NULL;
	if (vips_image_get_typeof(in, VIPS_META_ICC_NAME)) {
		if (vips_icc_transform(in, &t, "srgb", "embedded", TRUE, "intent", VIPS_INTENT_RELATIVE, NULL) == 0) {
			g_object_unref(in); in = t; t = NULL;
		} else {
			vips_error_clear(); // a broken profile: use the pixels as they are
		}
	}
	if (vips_image_get_interpretation(in) != VIPS_INTERPRETATION_sRGB) {
		if (vips_colourspace(in, &t, VIPS_INTERPRETATION_sRGB, NULL)) goto fail;
		g_object_unref(in); in = t; t = NULL;
	}
	if (vips_image_hasalpha(in)) {
		VipsArrayDouble *white = vips_array_double_newv(1, 255.0);
		int err = vips_flatten(in, &t, "background", white, NULL);
		vips_area_unref(VIPS_AREA(white));
		if (err) goto fail;
		g_object_unref(in); in = t; t = NULL;
	}
	if (vips_image_get_bands(in) > 3) {
		if (vips_extract_band(in, &t, 0, "n", 3, NULL)) goto fail;
		g_object_unref(in); in = t; t = NULL;
	}
	if (vips_image_get_format(in) != VIPS_FORMAT_UCHAR) {
		if (vips_cast_uchar(in, &t, NULL)) goto fail;
		g_object_unref(in); in = t; t = NULL;
	}
	return in;
fail:
	g_object_unref(in);
	return NULL;
}

static int save_jpeg(VipsImage *im, int q, void **buf, size_t *len) {
	return vips_jpegsave_buffer(im, buf, len, "Q", q, "optimize_coding", TRUE,
		"keep", VIPS_FOREIGN_KEEP_NONE, NULL);
}

// finish turns an upright image of at most large px into both JPEGs and the
// hash square. It consumes lazy.
static int finish(VipsImage *lazy, int small, int side, lumen_thumbs *o) {
	VipsImage *big = NULL, *little = NULL, *sq = NULL;
	int rc = -1;
	lazy = to_srgb8(lazy);
	if (!lazy) return -1;
	// Render once: the source is read sequentially, and two outputs derive from it.
	big = vips_image_copy_memory(lazy);
	g_object_unref(lazy);
	if (!big) goto done;
	if (save_jpeg(big, 85, &o->large, &o->large_len)) goto done;
	if (vips_thumbnail_image(big, &little, small, "height", small, "size", VIPS_SIZE_DOWN, NULL)) goto done;
	if (save_jpeg(little, 80, &o->small, &o->small_len)) goto done;
	if (vips_thumbnail_image(little, &sq, side, "height", side, "size", VIPS_SIZE_FORCE, NULL)) goto done;
	o->hash = vips_image_write_to_memory(sq, &o->hash_len);
	rc = o->hash ? 0 : -1;
done:
	if (big) g_object_unref(big);
	if (little) g_object_unref(little);
	if (sq) g_object_unref(sq);
	return rc;
}

// lumen_thumbs_file decodes path once, shrinking on load where the format
// allows it, and rotates it upright from its EXIF orientation.
static int lumen_thumbs_file(const char *path, int large, int small, int side, lumen_thumbs *o) {
	VipsImage *t = NULL;
	if (vips_thumbnail(path, &t, large, "height", large, "size", VIPS_SIZE_DOWN, NULL)) return -1;
	return finish(t, small, side, o);
}

// wrap_pixels wraps 8-bit RGB(A) pixels decoded elsewhere as an RGB image,
// in the colour profile icc (icc_len bytes; NULL: sRGB), which to_srgb8
// then converts from. The pixels must outlive the returned (lazy) image's use.
static VipsImage *wrap_pixels(const void *pix, size_t len, int w, int h, int bands, const void *icc, size_t icc_len) {
	VipsImage *in = NULL, *c = NULL;
	int err;
	in = vips_image_new_from_memory(pix, len, w, h, bands, VIPS_FORMAT_UCHAR);
	if (!in) return NULL;
	err = vips_copy(in, &c, "interpretation", VIPS_INTERPRETATION_sRGB, NULL);
	g_object_unref(in);
	if (err) return NULL;
	if (icc && icc_len) vips_image_set_blob_copy(c, VIPS_META_ICC_NAME, icc, icc_len);
	return c;
}

// from_pixels wraps pixels (see wrap_pixels) shrunk to at most side and
// turned upright from orientation (EXIF 1..8).
static VipsImage *from_pixels(const void *pix, size_t len, int w, int h, int bands, const void *icc, size_t icc_len,
		int orientation, int side) {
	VipsImage *c = NULL, *t = NULL;
	int err;
	c = wrap_pixels(pix, len, w, h, bands, icc, icc_len);
	if (!c) return NULL;
	vips_image_set_int(c, VIPS_META_ORIENTATION, orientation);
	err = vips_thumbnail_image(c, &t, side, "height", side, "size", VIPS_SIZE_DOWN, NULL);
	g_object_unref(c);
	return err ? NULL : t;
}

// lumen_thumbs_pixels does what lumen_thumbs_file does for pixels decoded
// elsewhere.
static int lumen_thumbs_pixels(const void *pix, size_t len, int w, int h, int bands, const void *icc, size_t icc_len,
		int orientation, int large, int small, int side, lumen_thumbs *o) {
	VipsImage *t = from_pixels(pix, len, w, h, bands, icc, icc_len, orientation, large);
	if (!t) return -1;
	return finish(t, small, side, o);
}

// to_rgb renders an image as packed 8-bit RGB into *buf (g_malloc'd). It
// consumes t.
static int to_rgb(VipsImage *t, void **buf, size_t *len, int *w, int *h) {
	t = to_srgb8(t);
	if (!t) return -1;
	*w = vips_image_get_width(t);
	*h = vips_image_get_height(t);
	*buf = vips_image_write_to_memory(t, len);
	g_object_unref(t);
	return *buf ? 0 : -1;
}

// lumen_rgb_file decodes path upright as RGB, shrinking on load so its long
// edge is at most side.
static int lumen_rgb_file(const char *path, int side, void **buf, size_t *len, int *w, int *h) {
	VipsImage *t = NULL;
	if (vips_thumbnail(path, &t, side, "height", side, "size", VIPS_SIZE_DOWN, NULL)) return -1;
	return to_rgb(t, buf, len, w, h);
}

// lumen_rgb_pixels does the same for pixels decoded elsewhere.
static int lumen_rgb_pixels(const void *pix, size_t len, int w, int h, int bands, const void *icc, size_t icc_len,
		int orientation, int side, void **buf, size_t *olen, int *ow, int *oh) {
	VipsImage *t = from_pixels(pix, len, w, h, bands, icc, icc_len, orientation, side);
	if (!t) return -1;
	return to_rgb(t, buf, olen, ow, oh);
}

// lumen_square decodes path squashed to exactly side x side RGB (aspect
// ignored), as image models expect.
static int lumen_square(const char *path, int side, void **buf, size_t *len, int *w, int *h) {
	VipsImage *t = NULL;
	if (vips_thumbnail(path, &t, side, "height", side, "size", VIPS_SIZE_FORCE, NULL)) return -1;
	return to_rgb(t, buf, len, w, h);
}

// lumen_square_pixels squashes RGB pixels to exactly side x side (aspect
// ignored), as lumen_square does for files.
static int lumen_square_pixels(const void *pix, size_t len, int w, int h, int side,
		void **buf, size_t *olen, int *ow, int *oh) {
	VipsImage *c = NULL, *t = NULL;
	int err;
	c = wrap_pixels(pix, len, w, h, 3, NULL, 0);
	if (!c) return -1;
	err = vips_thumbnail_image(c, &t, side, "height", side, "size", VIPS_SIZE_FORCE, NULL);
	g_object_unref(c);
	if (err) return -1;
	return to_rgb(t, buf, olen, ow, oh);
}

// lumen_jpeg_pixels encodes RGB pixels as a JPEG of quality q.
static int lumen_jpeg_pixels(const void *pix, size_t len, int w, int h, int q, void **buf, size_t *olen) {
	VipsImage *c = wrap_pixels(pix, len, w, h, 3, NULL, 0);
	int err;
	if (!c) return -1;
	err = save_jpeg(c, q, buf, olen);
	g_object_unref(c);
	return err;
}

// lumen_header reads only the header: the stored (not rotated) size.
static int lumen_header(const char *path, int *w, int *h) {
	VipsImage *im = vips_image_new_from_file(path, NULL);
	if (!im) return -1;
	*w = vips_image_get_width(im);
	*h = vips_image_get_height(im);
	g_object_unref(im);
	return 0;
}

// lumen_profile reads only the header: a copy of the embedded colour
// profile in *out (g_malloc'd; NULL if there is none).
static int lumen_profile(const char *path, void **out, size_t *n) {
	const void *data = NULL;
	size_t len = 0;
	VipsImage *im = vips_image_new_from_file(path, NULL);
	*out = NULL; *n = 0;
	if (!im) return -1;
	if (vips_image_get_typeof(im, VIPS_META_ICC_NAME) &&
		vips_image_get_blob(im, VIPS_META_ICC_NAME, &data, &len) == 0 && len > 0) {
		*out = g_malloc(len);
		memcpy(*out, data, len);
		*n = len;
	}
	g_object_unref(im);
	return 0;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"unsafe"
)

var initOnce = sync.OnceValue(func() error {
	if C.lumen_init() != 0 {
		return errors.New("libvips failed to start")
	}
	return nil
})

// Init starts libvips. Every other function calls it; calling it early just
// moves the one-off cost.
func Init() error { return initOnce() }

// Version is the linked libvips version.
func Version() string { return C.GoString(C.vips_version_string()) }

// Thumbs is one decode's output.
type Thumbs struct {
	Large, Small []byte // JPEG
	Square       []byte // side x side 8-bit RGB of the whole image, e.g. for a perceptual hash
}

// Sizes says what to make: the long edge of each JPEG (never upscaled) and
// the side of the RGB square.
type Sizes struct{ Large, Small, Square int }

// ThumbnailFile decodes a file, turned upright from its EXIF orientation.
func ThumbnailFile(path string, s Sizes) (Thumbs, error) {
	if err := Init(); err != nil {
		return Thumbs{}, err
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var o C.lumen_thumbs
	rc := C.lumen_thumbs_file(cpath, C.int(s.Large), C.int(s.Small), C.int(s.Square), &o)
	return collect(rc, &o, s.Square)
}

// Pixels is an image decoded outside libvips: 8-bit RGB (Bands 3) or RGBA
// (Bands 4), in the colour profile ICC (nil: sRGB), with the EXIF
// Orientation (1..8) still to apply.
type Pixels struct {
	Pix         []byte
	W, H, Bands int
	Orientation int
	ICC         []byte
}

// RGBPixels is upright sRGB pixels: what Decode, video frames and the rest
// of Lumen make.
func RGBPixels(pix []byte, w, h int) Pixels {
	return Pixels{Pix: pix, W: w, H: h, Bands: 3, Orientation: 1}
}

// cICC is p's profile for C: its address and length (nil, 0 for none).
func (p Pixels) cICC() (icc unsafe.Pointer, size C.size_t) {
	if len(p.ICC) == 0 {
		return nil, 0
	}
	return unsafe.Pointer(unsafe.SliceData(p.ICC)), C.size_t(len(p.ICC))
}

// ThumbnailPixels does what ThumbnailFile does for pixels decoded elsewhere,
// applying their orientation and converting from their profile.
func ThumbnailPixels(p Pixels, s Sizes) (Thumbs, error) {
	if err := Init(); err != nil {
		return Thumbs{}, err
	}
	if err := checkPixels(p.Pix, p.W, p.H, p.Bands); err != nil {
		return Thumbs{}, err
	}
	var o C.lumen_thumbs
	icc, iccLen := p.cICC()
	rc := C.lumen_thumbs_pixels(unsafe.Pointer(unsafe.SliceData(p.Pix)), C.size_t(len(p.Pix)), C.int(p.W), C.int(p.H),
		C.int(p.Bands), icc, iccLen, C.int(max(1, min(p.Orientation, 8))), C.int(s.Large), C.int(s.Small), C.int(s.Square), &o)
	return collect(rc, &o, s.Square)
}

// collect copies the C outputs into Go memory and frees them.
func collect(rc C.int, o *C.lumen_thumbs, side int) (Thumbs, error) {
	defer func() {
		for _, p := range []unsafe.Pointer{o.large, o.small, o.hash} {
			if p != nil {
				C.g_free(C.gpointer(p))
			}
		}
	}()
	if rc != 0 {
		return Thumbs{}, lastError()
	}
	t := Thumbs{
		Large:  C.GoBytes(o.large, C.int(o.large_len)),
		Small:  C.GoBytes(o.small, C.int(o.small_len)),
		Square: C.GoBytes(o.hash, C.int(o.hash_len)),
	}
	if len(t.Square) != side*side*3 {
		return Thumbs{}, errors.New("vips: unexpected square size")
	}
	return t, nil
}

// RGB is packed 8-bit RGB pixels.
type RGB struct {
	Pix  []byte // W x H x 3
	W, H int
}

// DecodeFile decodes a file upright (EXIF orientation applied), shrunk on
// load so its long edge is at most side (never enlarged).
func DecodeFile(path string, side int) (RGB, error) {
	if err := Init(); err != nil {
		return RGB{}, err
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var buf unsafe.Pointer
	var n C.size_t
	var w, h C.int
	rc := C.lumen_rgb_file(cpath, C.int(side), &buf, &n, &w, &h)
	return collectRGB(rc, buf, n, w, h)
}

// DecodePixels does what DecodeFile does for pixels decoded elsewhere,
// applying their orientation and converting from their profile.
func DecodePixels(p Pixels, side int) (RGB, error) {
	if err := Init(); err != nil {
		return RGB{}, err
	}
	if err := checkPixels(p.Pix, p.W, p.H, p.Bands); err != nil {
		return RGB{}, err
	}
	var buf unsafe.Pointer
	var n C.size_t
	var ow, oh C.int
	icc, iccLen := p.cICC()
	rc := C.lumen_rgb_pixels(unsafe.Pointer(unsafe.SliceData(p.Pix)), C.size_t(len(p.Pix)), C.int(p.W), C.int(p.H),
		C.int(p.Bands), icc, iccLen, C.int(max(1, min(p.Orientation, 8))), C.int(side), &buf, &n, &ow, &oh)
	return collectRGB(rc, buf, n, ow, oh)
}

// collectRGB copies a C RGB buffer into Go memory and frees it.
func collectRGB(rc C.int, buf unsafe.Pointer, n C.size_t, w, h C.int) (RGB, error) {
	if buf != nil {
		defer C.g_free(C.gpointer(buf))
	}
	if rc != 0 {
		return RGB{}, lastError()
	}
	if w <= 0 || h <= 0 || n > math.MaxInt32 || uint64(n) != uint64(w)*uint64(h)*3 {
		return RGB{}, errors.New("vips: unexpected image size")
	}
	return RGB{Pix: C.GoBytes(buf, C.int(n)), W: int(w), H: int(h)}, nil
}

func checkPixels(pix []byte, w, h, bands int) error {
	if w <= 0 || h <= 0 || w > math.MaxInt32 || h > math.MaxInt32 || (bands != 3 && bands != 4) ||
		len(pix)%bands != 0 || len(pix)/bands%h != 0 || len(pix)/bands/h != w {
		return fmt.Errorf("vips: bad pixel buffer (%dx%dx%d, %d bytes)", w, h, bands, len(pix))
	}
	return nil
}

// Square returns a file squashed to exactly side x side 8-bit RGB.
func Square(path string, side int) ([]byte, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var buf unsafe.Pointer
	var n C.size_t
	var w, h C.int
	rc := C.lumen_square(cpath, C.int(side), &buf, &n, &w, &h)
	im, err := collectRGB(rc, buf, n, w, h)
	if err == nil && (im.W != side || im.H != side) {
		err = errors.New("vips: unexpected square size")
	}
	return im.Pix, err
}

// SquarePixels squashes 8-bit RGB pixels to exactly side x side (aspect
// ignored), as Square does for a file.
func SquarePixels(pix []byte, w, h, side int) ([]byte, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	if err := checkPixels(pix, w, h, 3); err != nil {
		return nil, err
	}
	var buf unsafe.Pointer
	var n C.size_t
	var ow, oh C.int
	rc := C.lumen_square_pixels(unsafe.Pointer(unsafe.SliceData(pix)), C.size_t(len(pix)), C.int(w), C.int(h),
		C.int(side), &buf, &n, &ow, &oh)
	im, err := collectRGB(rc, buf, n, ow, oh)
	if err == nil && (im.W != side || im.H != side) {
		err = errors.New("vips: unexpected square size")
	}
	return im.Pix, err
}

// JPEG encodes 8-bit RGB pixels as a JPEG of the given quality (1..100).
func JPEG(pix []byte, w, h, quality int) ([]byte, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	if err := checkPixels(pix, w, h, 3); err != nil {
		return nil, err
	}
	var buf unsafe.Pointer
	var n C.size_t
	rc := C.lumen_jpeg_pixels(unsafe.Pointer(unsafe.SliceData(pix)), C.size_t(len(pix)), C.int(w), C.int(h),
		C.int(quality), &buf, &n)
	if buf != nil {
		defer C.g_free(C.gpointer(buf))
	}
	if rc != 0 {
		return nil, lastError()
	}
	return C.GoBytes(buf, C.int(n)), nil
}

// Size reads a file's stored pixel size from its header, without decoding.
func Size(path string) (w, h int, err error) {
	if err := Init(); err != nil {
		return 0, 0, err
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var cw, ch C.int
	if C.lumen_header(cpath, &cw, &ch) != 0 {
		return 0, 0, lastError()
	}
	return int(cw), int(ch), nil
}

// Profile returns the colour profile (ICC) embedded in a file, reading only
// its header; nil if it has none.
func Profile(path string) ([]byte, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var buf unsafe.Pointer
	var n C.size_t
	if C.lumen_profile(cpath, &buf, &n) != 0 {
		return nil, lastError()
	}
	if buf == nil {
		return nil, nil
	}
	defer C.g_free(C.gpointer(buf))
	return C.GoBytes(buf, C.int(n)), nil
}

func lastError() error {
	msg := C.lumen_error()
	defer C.g_free(C.gpointer(msg))
	// The first line of libvips' error log is the useful one.
	first, _, _ := strings.Cut(C.GoString(msg), "\n")
	if first == "" {
		// Another file failing at the same moment took the message.
		first = "the image could not be read"
	}
	return errors.New("vips: " + first)
}

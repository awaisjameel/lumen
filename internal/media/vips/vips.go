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

static char *lumen_error(void) {
	char *s = g_strdup(vips_error_buffer());
	vips_error_clear();
	return s;
}

// to_srgb8 returns in as 8-bit sRGB with exactly 3 bands, transparency
// flattened onto white. It always consumes in.
static VipsImage *to_srgb8(VipsImage *in) {
	VipsImage *t = NULL;
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

// lumen_thumbs_pixels does the same for 8-bit RGB(A) pixels decoded
// elsewhere, applying orientation (EXIF 1..8) itself.
static int lumen_thumbs_pixels(const void *pix, size_t len, int w, int h, int bands, int orientation,
		int large, int small, int side, lumen_thumbs *o) {
	VipsImage *in = NULL, *c = NULL, *t = NULL;
	int err;
	in = vips_image_new_from_memory(pix, len, w, h, bands, VIPS_FORMAT_UCHAR);
	if (!in) return -1;
	err = vips_copy(in, &c, "interpretation", VIPS_INTERPRETATION_sRGB, NULL);
	g_object_unref(in);
	if (err) return -1;
	vips_image_set_int(c, VIPS_META_ORIENTATION, orientation);
	err = vips_thumbnail_image(c, &t, large, "height", large, "size", VIPS_SIZE_DOWN, NULL);
	g_object_unref(c);
	if (err) return -1;
	return finish(t, small, side, o);
}

// lumen_square decodes path squashed to exactly side x side RGB (aspect
// ignored), as image models expect.
static int lumen_square(const char *path, int side, void **buf, size_t *len) {
	VipsImage *t = NULL;
	if (vips_thumbnail(path, &t, side, "height", side, "size", VIPS_SIZE_FORCE, NULL)) return -1;
	t = to_srgb8(t);
	if (!t) return -1;
	*buf = vips_image_write_to_memory(t, len);
	g_object_unref(t);
	return *buf ? 0 : -1;
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
*/
import "C"

import (
	"errors"
	"fmt"
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

// ThumbnailPixels does the same for 8-bit RGB (bands 3) or RGBA (bands 4)
// pixels, applying an EXIF orientation (1..8) first.
func ThumbnailPixels(pix []byte, w, h, bands, orientation int, s Sizes) (Thumbs, error) {
	if err := Init(); err != nil {
		return Thumbs{}, err
	}
	if w <= 0 || h <= 0 || (bands != 3 && bands != 4) || len(pix) != w*h*bands {
		return Thumbs{}, fmt.Errorf("vips: bad pixel buffer (%dx%dx%d, %d bytes)", w, h, bands, len(pix))
	}
	var o C.lumen_thumbs
	rc := C.lumen_thumbs_pixels(unsafe.Pointer(unsafe.SliceData(pix)), C.size_t(len(pix)), C.int(w), C.int(h),
		C.int(bands), C.int(max(1, min(orientation, 8))), C.int(s.Large), C.int(s.Small), C.int(s.Square), &o)
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

// Square returns a file squashed to exactly side x side 8-bit RGB.
func Square(path string, side int) ([]byte, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var buf unsafe.Pointer
	var n C.size_t
	if C.lumen_square(cpath, C.int(side), &buf, &n) != 0 {
		return nil, lastError()
	}
	defer C.g_free(C.gpointer(buf))
	if int(n) != side*side*3 {
		return nil, errors.New("vips: unexpected square size")
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

func lastError() error {
	msg := C.lumen_error()
	defer C.g_free(C.gpointer(msg))
	return errors.New("vips: " + trim(C.GoString(msg)))
}

// trim keeps the first line of libvips' error log, which is the useful one.
func trim(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

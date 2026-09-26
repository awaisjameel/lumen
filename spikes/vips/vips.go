// Package vips is a minimal cgo wrapper around libvips for the Phase 0
// thumbnail spike: one decode (shrink-on-load, auto-rotate) makes both sizes.
package vips

/*
#cgo CFLAGS: -I${SRCDIR}/../.cache/vips-dev-8.18/include -I${SRCDIR}/../.cache/vips-dev-8.18/include/glib-2.0 -I${SRCDIR}/../.cache/vips-dev-8.18/lib/glib-2.0/include
#cgo LDFLAGS: -L${SRCDIR}/../.cache/vips-dev-8.18/lib -l:libvips.lib -l:libglib-2.0.lib -l:libgobject-2.0.lib
#include <stdlib.h>
#include <vips/vips.h>

static int lumen_init(void) {
	if (VIPS_INIT("lumen")) return -1;
	vips_cache_set_max(0);  // batch work: never reuse operations
	return 0;
}

static int lumen_save(VipsImage *img, void **buf, size_t *len) {
	return vips_jpegsave_buffer(img, buf, len, "Q", 80, "keep", VIPS_FOREIGN_KEEP_NONE, NULL);
}

// lumen_thumbs decodes path once at <= large px (long edge) and derives the
// small size from that, so the expensive decode happens once.
static int lumen_thumbs(const char *path, int large, int small,
		void **lbuf, size_t *llen, void **sbuf, size_t *slen,
		int *w, int *h) {
	VipsImage *lazy = NULL, *big = NULL, *little = NULL;
	int rc = -1;
	if (vips_thumbnail(path, &lazy, large, "height", large, "size", VIPS_SIZE_DOWN, NULL)) goto done;
	// The file is read sequentially, so render once to memory and derive
	// both outputs from that.
	big = vips_image_copy_memory(lazy);
	g_object_unref(lazy);
	if (!big) goto done;
	*w = vips_image_get_width(big);
	*h = vips_image_get_height(big);
	if (lumen_save(big, lbuf, llen)) goto done;
	if (vips_thumbnail_image(big, &little, small, "height", small, "size", VIPS_SIZE_DOWN, NULL)) goto done;
	if (lumen_save(little, sbuf, slen)) goto done;
	rc = 0;
done:
	if (big) g_object_unref(big);
	if (little) g_object_unref(little);
	return rc;
}

// lumen_rgb decodes path squashed (aspect ignored) to exactly w x h sRGB
// pixels, as CLIP-style models expect; alpha is flattened onto white.
static int lumen_rgb(const char *path, int w, int h, int force, void **buf, size_t *len, int *ow, int *oh) {
	VipsImage *t = NULL, *c = NULL, *f = NULL;
	int rc = -1;
	if (vips_thumbnail(path, &t, w, "height", h, "size", force ? VIPS_SIZE_FORCE : VIPS_SIZE_DOWN, NULL)) goto done;
	if (vips_colourspace(t, &c, VIPS_INTERPRETATION_sRGB, NULL)) goto done;
	if (vips_image_hasalpha(c)) {
		VipsArrayDouble *white = vips_array_double_newv(1, 255.0);
		int err = vips_flatten(c, &f, "background", white, NULL);
		vips_area_unref(VIPS_AREA(white));
		if (err) goto done;
	} else {
		f = c; g_object_ref(f);
	}
	if (vips_image_get_bands(f) != 3 || vips_image_get_format(f) != VIPS_FORMAT_UCHAR) {
		VipsImage *x = NULL;
		if (vips_extract_band(f, &x, 0, "n", 3, NULL)) goto done;
		g_object_unref(f); f = x;
		if (vips_cast_uchar(f, &x, NULL)) goto done;
		g_object_unref(f); f = x;
	}
	*ow = vips_image_get_width(f);
	*oh = vips_image_get_height(f);
	*buf = vips_image_write_to_memory(f, len);
	rc = *buf ? 0 : -1;
done:
	if (t) g_object_unref(t);
	if (c) g_object_unref(c);
	if (f) g_object_unref(f);
	return rc;
}

static char *lumen_error(void) {
	char *s = g_strdup(vips_error_buffer());
	vips_error_clear();
	return s;
}
*/
import "C"

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"
)

var initOnce = sync.OnceValue(func() error {
	if C.lumen_init() != 0 {
		return errors.New("vips: init failed")
	}
	return nil
})

// Init starts libvips; safe to call more than once.
func Init() error { return initOnce() }

// SetConcurrency sets libvips' own worker threads per operation.
func SetConcurrency(n int) { C.vips_concurrency_set(C.int(n)) }

// Version reports the linked libvips version.
func Version() string { return C.GoString(C.vips_version_string()) }

// Thumbs returns JPEG thumbnails (long edge <= large and <= small) and the
// large one's size, with EXIF orientation already applied.
func Thumbs(path string, large, small int) (big, little []byte, w, h int, err error) {
	if err := Init(); err != nil {
		return nil, nil, 0, 0, err
	}
	runtime.LockOSThread() // keep libvips' per-thread state on one thread
	defer runtime.UnlockOSThread()

	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var lbuf, sbuf unsafe.Pointer
	var llen, slen C.size_t
	var cw, ch C.int
	if C.lumen_thumbs(cpath, C.int(large), C.int(small), &lbuf, &llen, &sbuf, &slen, &cw, &ch) != 0 {
		msg := C.lumen_error()
		defer C.g_free(C.gpointer(msg))
		if lbuf != nil {
			C.g_free(C.gpointer(lbuf))
		}
		return nil, nil, 0, 0, errors.New("vips: " + C.GoString(msg))
	}
	big = C.GoBytes(lbuf, C.int(llen))
	little = C.GoBytes(sbuf, C.int(slen))
	C.g_free(C.gpointer(lbuf))
	C.g_free(C.gpointer(sbuf))
	return big, little, int(cw), int(ch), nil
}

// RGB returns w*h*3 bytes of sRGB pixels, squashed to exactly w x h.
func RGB(path string, w, h int) ([]byte, error) {
	pix, _, _, err := rgb(path, w, h, true)
	return pix, err
}

// RGBFit returns sRGB pixels with the long edge at most maxEdge, aspect kept
// and EXIF orientation applied.
func RGBFit(path string, maxEdge int) (pix []byte, w, h int, err error) {
	return rgb(path, maxEdge, maxEdge, false)
}

func rgb(path string, w, h int, force bool) ([]byte, int, int, error) {
	if err := Init(); err != nil {
		return nil, 0, 0, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var buf unsafe.Pointer
	var n C.size_t
	var ow, oh C.int
	f := C.int(0)
	if force {
		f = 1
	}
	if C.lumen_rgb(cpath, C.int(w), C.int(h), f, &buf, &n, &ow, &oh) != 0 {
		msg := C.lumen_error()
		defer C.g_free(C.gpointer(msg))
		return nil, 0, 0, errors.New("vips: " + C.GoString(msg))
	}
	defer C.g_free(C.gpointer(buf))
	if int(n) != int(ow)*int(oh)*3 {
		return nil, 0, 0, errors.New("vips: unexpected pixel buffer size")
	}
	return C.GoBytes(buf, C.int(n)), int(ow), int(oh), nil
}

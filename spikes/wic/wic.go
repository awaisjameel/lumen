//go:build windows

// Package wic decodes images through the Windows Imaging Component, which
// covers HEIC when the (paid or OEM-preinstalled) HEVC Video Extension exists.
package wic

/*
#cgo LDFLAGS: -lole32 -lwindowscodecs -luuid
#define COBJMACROS
#include <stdlib.h>
#include <windows.h>
#include <wincodec.h>

static HRESULT wic_decode(const wchar_t *path, UINT maxSide,
		BYTE **out, UINT *ow, UINT *oh, UINT *sw, UINT *sh) {
	IWICImagingFactory *f = NULL;
	IWICBitmapDecoder *d = NULL;
	IWICBitmapFrameDecode *fr = NULL;
	IWICBitmapScaler *sc = NULL;
	IWICFormatConverter *cv = NULL;
	UINT w = 0, h = 0, tw, th;
	HRESULT hr = CoInitializeEx(NULL, COINIT_MULTITHREADED);
	if (FAILED(hr) && hr != RPC_E_CHANGED_MODE) return hr;

	hr = CoCreateInstance(&CLSID_WICImagingFactory, NULL, CLSCTX_INPROC_SERVER,
		&IID_IWICImagingFactory, (void **)&f);
	if (FAILED(hr)) goto done;
	hr = IWICImagingFactory_CreateDecoderFromFilename(f, path, NULL, GENERIC_READ,
		WICDecodeMetadataCacheOnDemand, &d);
	if (FAILED(hr)) goto done;
	hr = IWICBitmapDecoder_GetFrame(d, 0, &fr);
	if (FAILED(hr)) goto done;
	hr = IWICBitmapFrameDecode_GetSize(fr, &w, &h);
	if (FAILED(hr)) goto done;
	*sw = w; *sh = h;
	tw = w; th = h;
	if (w > maxSide || h > maxSide) {
		if (w >= h) { tw = maxSide; th = (UINT)((double)h * maxSide / w + 0.5); }
		else        { th = maxSide; tw = (UINT)((double)w * maxSide / h + 0.5); }
		if (tw < 1) tw = 1;
		if (th < 1) th = 1;
	}
	hr = IWICImagingFactory_CreateBitmapScaler(f, &sc);
	if (FAILED(hr)) goto done;
	hr = IWICBitmapScaler_Initialize(sc, (IWICBitmapSource *)fr, tw, th, WICBitmapInterpolationModeFant);
	if (FAILED(hr)) goto done;
	hr = IWICImagingFactory_CreateFormatConverter(f, &cv);
	if (FAILED(hr)) goto done;
	hr = IWICFormatConverter_Initialize(cv, (IWICBitmapSource *)sc, &GUID_WICPixelFormat32bppRGBA,
		WICBitmapDitherTypeNone, NULL, 0.0, WICBitmapPaletteTypeCustom);
	if (FAILED(hr)) goto done;
	*out = malloc((size_t)tw * th * 4);
	if (!*out) { hr = E_OUTOFMEMORY; goto done; }
	hr = IWICFormatConverter_CopyPixels(cv, NULL, tw * 4, tw * th * 4, *out);
	if (FAILED(hr)) { free(*out); *out = NULL; goto done; }
	*ow = tw; *oh = th;
done:
	if (cv) IWICFormatConverter_Release(cv);
	if (sc) IWICBitmapScaler_Release(sc);
	if (fr) IWICBitmapFrameDecode_Release(fr);
	if (d) IWICBitmapDecoder_Release(d);
	if (f) IWICImagingFactory_Release(f);
	return hr;
}
*/
import "C"

import (
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Decode returns the image scaled so its long edge is at most maxSide, and
// the source size. Orientation is NOT applied.
func Decode(path string, maxSide int) (img *image.RGBA, srcW, srcH int, err error) {
	runtime.LockOSThread() // COM apartment is per OS thread
	defer runtime.UnlockOSThread()
	wpath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, 0, 0, err
	}
	var buf *C.BYTE
	var w, h, sw, sh C.UINT
	hr := C.wic_decode((*C.wchar_t)(unsafe.Pointer(wpath)), C.UINT(maxSide), &buf, &w, &h, &sw, &sh)
	if hr < 0 {
		return nil, 0, 0, fmt.Errorf("wic: HRESULT 0x%08x", uint32(hr))
	}
	defer C.free(unsafe.Pointer(buf))
	img = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	copy(img.Pix, unsafe.Slice((*byte)(unsafe.Pointer(buf)), len(img.Pix)))
	return img, int(sw), int(sh), nil
}

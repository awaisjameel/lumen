// Package wic decodes images through the Windows Imaging Component. Lumen
// uses it for HEIC, which works when Microsoft's HEVC Video Extension is
// installed (bought, or preinstalled by the PC maker).
package wic

/*
#cgo LDFLAGS: -lole32 -lwindowscodecs -luuid
#define COBJMACROS
#include <stdlib.h>
#include <windows.h>
#include <wincodec.h>

// wic_decode decodes the first frame of path scaled so its long edge is at
// most max_side, as 8-bit RGBA. *out is malloc'd; the caller frees it.
static HRESULT wic_decode(const wchar_t *path, UINT max_side, BYTE **out, UINT *ow, UINT *oh) {
	IWICImagingFactory *f = NULL;
	IWICBitmapDecoder *d = NULL;
	IWICBitmapFrameDecode *fr = NULL;
	IWICBitmapScaler *sc = NULL;
	IWICFormatConverter *cv = NULL;
	UINT w = 0, h = 0, tw, th;
	HRESULT init = CoInitializeEx(NULL, COINIT_MULTITHREADED);
	HRESULT hr = init;
	if (FAILED(init) && init != RPC_E_CHANGED_MODE) return init;

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
	if (w == 0 || h == 0) { hr = E_FAIL; goto done; }
	tw = w; th = h;
	if (w > max_side || h > max_side) {
		if (w >= h) { tw = max_side; th = (UINT)((double)h * max_side / w + 0.5); }
		else        { th = max_side; tw = (UINT)((double)w * max_side / h + 0.5); }
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
	if (SUCCEEDED(init)) CoUninitialize();
	return hr;
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Decode returns 8-bit RGBA pixels of the image at path, scaled so its long
// edge is at most maxSide. EXIF/HEIF orientation is NOT applied.
func Decode(path string, maxSide int) (pix []byte, w, h int, err error) {
	runtime.LockOSThread() // COM initialisation belongs to the OS thread
	defer runtime.UnlockOSThread()
	wpath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, 0, 0, err
	}
	var buf *C.BYTE
	var cw, ch C.UINT
	if hr := C.wic_decode((*C.wchar_t)(unsafe.Pointer(wpath)), C.UINT(maxSide), &buf, &cw, &ch); hr < 0 {
		return nil, 0, 0, fmt.Errorf("wic: HRESULT 0x%08X", uint32(hr))
	}
	defer C.free(unsafe.Pointer(buf))
	w, h = int(cw), int(ch)
	return C.GoBytes(unsafe.Pointer(buf), C.int(w*h*4)), w, h, nil
}

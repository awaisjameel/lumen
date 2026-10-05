package fileops

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var openWithDialog = windows.NewLazySystemDLL("shell32.dll").NewProc("SHOpenWithDialog")

type openAsInfo struct {
	file, class *uint16
	flags       uint32
}

// Open hands an explicitly chosen media file to the Windows shell.
func Open(path string, choose bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		return err
	}
	defer windows.CoUninitialize()
	v, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if choose {
		info := openAsInfo{file: p, flags: 4} // OAIF_EXEC: opens this file; defaults are unchanged on Windows 10+.
		result, _, _ := openWithDialog.Call(0, uintptr(unsafe.Pointer(&info)))
		if int32(result) < 0 {
			return fmt.Errorf("Open with failed: HRESULT %#x", uint32(result))
		}
		return nil
	}
	return windows.ShellExecute(0, v, p, nil, nil, windows.SW_SHOWNORMAL)
}

package update

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ShowFailure leaves a visible repair route when no WebView can start.
func ShowFailure(err error) {
	text, encodeErr := windows.UTF16PtrFromString("Lumen could not finish the update or start safely. Close other Lumen windows and try again. If the problem remains, run the signed offline installer over this installation; keep your library.\n\n" + err.Error())
	if encodeErr != nil {
		return
	}
	title, encodeErr := windows.UTF16PtrFromString("Lumen needs attention")
	if encodeErr != nil {
		return
	}
	_, _, _ = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}

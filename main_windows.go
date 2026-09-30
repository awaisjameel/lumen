package main

import "golang.org/x/sys/windows"

// fatalDialog tells the user why Lumen can't start, when it has no window
// to say it in.
func fatalDialog(msg string) {
	text, err := windows.UTF16PtrFromString(msg)
	if err != nil {
		return
	}
	title, _ := windows.UTF16PtrFromString("Lumen")
	_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
}

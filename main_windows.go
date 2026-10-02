package main

import (
	"log/slog"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// lowerPriority runs Lumen below normal priority. Its indexing (libvips, the
// models' threads, FFmpeg) is background work that must give way to whatever
// the user is doing; at normal priority it froze the other programs. Lumen's
// own window keeps up because indexing leaves half the CPU free (see
// index.cpuSlots), and a playing video converts at normal priority.
func lowerPriority() {
	if err := windows.SetPriorityClass(windows.CurrentProcess(), windows.BELOW_NORMAL_PRIORITY_CLASS); err != nil {
		slog.Warn("lower process priority", "err", err)
	}
}

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

// raiseWebView puts the window's WebView2 processes back at normal priority:
// they start below normal with Lumen (see lowerPriority), and the window must
// keep up with the user. The WebView2 browser process starts the ones it adds
// later at its own, now normal, priority. A priority WebView2 chose itself
// (its GPU process runs above normal, a hidden page idle) stays.
func raiseWebView() {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		slog.Warn("list the window's processes", "err", err)
		return
	}
	defer func() { _ = windows.CloseHandle(snap) }()
	children := map[uint32][]windows.ProcessEntry32{}
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		children[e.ParentProcessID] = append(children[e.ParentProcessID], e)
	}
	for queue := []uint32{windows.GetCurrentProcessId()}; len(queue) > 0; queue = queue[1:] {
		for _, c := range children[queue[0]] {
			if strings.EqualFold(windows.UTF16ToString(c.ExeFile[:]), "msedgewebview2.exe") {
				raiseBelowNormal(c.ProcessID)
				queue = append(queue, c.ProcessID)
			}
		}
	}
}

// raiseBelowNormal sets a process running below normal priority to normal.
// One that exited meanwhile is left alone.
func raiseBelowNormal(pid uint32) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_SET_INFORMATION, false, pid)
	if err != nil {
		return // gone
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if p, err := windows.GetPriorityClass(h); err != nil || p != windows.BELOW_NORMAL_PRIORITY_CLASS {
		return
	}
	if err := windows.SetPriorityClass(h, windows.NORMAL_PRIORITY_CLASS); err != nil {
		slog.Warn("raise the window's priority", "pid", pid, "err", err)
	}
}

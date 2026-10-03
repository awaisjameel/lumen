//go:build !windows

package diag

import "runtime"

// Version is Lumen's version; only Windows builds carry one for now.
func Version() string { return devVersion }

// windowsVersion is empty off Windows.
func windowsVersion() string { return "" }

// System describes this PC; off Windows, only what Go knows.
func System() PC { return PC{Threads: runtime.NumCPU(), CPU: runtime.GOARCH} }

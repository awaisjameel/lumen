//go:build !windows

package index

func onBattery() bool { return false }

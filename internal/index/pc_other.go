//go:build !windows

package index

import "time"

// systemPC reads the real PC. Only Windows is read for now: elsewhere the PC
// is on mains power, with nothing in full screen, the user present and no
// load, so only a pause holds indexing.
type systemPC struct{}

// CountOwnPrograms does nothing: only Windows' processor load is read.
func CountOwnPrograms() {}

func (systemPC) onBattery() bool            { return false }
func (systemPC) fullScreen() bool           { return false }
func (systemPC) sinceInput() time.Duration  { return 0 }
func (systemPC) cpuTimes() (cpuTimes, bool) { return cpuTimes{}, false }

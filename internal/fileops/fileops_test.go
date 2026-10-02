package fileops

import "testing"

func TestTheSinkOnlyLetsTheShellDeleteWhatItWillRecycle(t *testing.T) {
	for _, c := range []struct {
		name  string
		flags uint32
		want  bool
	}{
		{"no flags: the shell would destroy it", 0, false},
		{"only unrelated flags", 0x1 | 0x2 | 0x100 | 0x400, false},
		{"the recycle flag", tsfDeleteRecycleIfPossible, true},
		{"the recycle flag among others", tsfDeleteRecycleIfPossible | 0x1 | 0x400, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := recyclable(c.flags); got != c.want {
				t.Fatalf("recyclable(%#x) = %v, want %v", c.flags, got, c.want)
			}
		})
	}
}

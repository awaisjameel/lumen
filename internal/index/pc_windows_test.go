package index

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// spinEnv makes the test binary, started again as a child, keep a processor
// busy for a moment and exit (TestSpinForAParent).
const spinEnv = "LUMEN_TEST_SPIN"

func TestSpinForAParent(t *testing.T) {
	if os.Getenv(spinEnv) == "" {
		t.Skip("only run as TestLumensTimeIncludesProgramsItStartedThatHaveExited's child")
	}
	for start := time.Now(); time.Since(start) < 400*time.Millisecond; {
	}
}

func TestLumensTimeIncludesProgramsItStartedThatHaveExited(t *testing.T) {
	before, ok := ownCPU()
	if !ok {
		t.Fatal("Lumen's processor time can't be read")
	}
	if ownJob() == 0 {
		t.Skip("Windows refused a job object here, so exited children can't be counted")
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSpinForAParent$")
	cmd.Env = append(os.Environ(), spinEnv+"=1")
	start := time.Now()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	ran := time.Since(start)
	after, _ := ownCPU()
	// The child spun for 400 ms: nearly all of that is processor time, which
	// only the job still counts once the child has gone.
	if got := after - before; got < 300*time.Millisecond {
		t.Fatalf("Lumen's time grew by %v over a %v child that spun for 400ms", got, ran)
	}
}

func TestTheRealPCCanBeRead(t *testing.T) {
	var p systemPC
	t1, ok := p.cpuTimes()
	if !ok {
		t.Fatal("processor times can't be read")
	}
	if t1.total <= 0 || t1.busy < 0 || t1.busy > t1.total || t1.own <= 0 {
		t.Fatalf("implausible processor times: %+v", t1)
	}
	if d := p.sinceInput(); d < 0 {
		t.Fatalf("time since input = %v", d)
	}
	_ = p.onBattery()
	_ = p.fullScreen()
}

func TestFullScreenBoundsExcludeAWindowedOrMaximisedStoreApp(t *testing.T) {
	monitor := windowRect{left: -1920, top: 0, right: 0, bottom: 1080}
	for _, c := range []struct {
		name string
		rect windowRect
		want bool
	}{
		{"windowed", windowRect{left: -1600, top: 100, right: -200, bottom: 900}, false},
		{"maximised with taskbar", windowRect{left: -1920, top: 0, right: 0, bottom: 1040}, false},
		{"full screen", monitor, true},
		{"full screen with window borders", windowRect{left: -1928, top: -8, right: 8, bottom: 1088}, true},
	} {
		if got := c.rect.covers(monitor); got != c.want {
			t.Errorf("%s covers monitor = %t, want %t", c.name, got, c.want)
		}
	}
}

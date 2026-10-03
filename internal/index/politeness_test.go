package index

import (
	"sync"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/store"
)

// fakePC is a PC a test sets up: on mains power, nothing in full screen, the
// user present and no load unless it says otherwise.
type fakePC struct {
	mu      sync.Mutex // guards the fields: the indexer's pcLoop reads them
	battery bool
	full    bool
	idle    time.Duration
	times   cpuTimes
	noTimes bool
}

func (p *fakePC) onBattery() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.battery }

func (p *fakePC) fullScreen() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.full }

func (p *fakePC) sinceInput() time.Duration { p.mu.Lock(); defer p.mu.Unlock(); return p.idle }

func (p *fakePC) cpuTimes() (cpuTimes, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.times, !p.noTimes
}

// quiet gives an indexer a PC that never holds it, so a test's indexing
// doesn't wait for whatever else the machine running it is doing.
func quiet(ix *Indexer) *Indexer {
	ix.pc = &fakePC{}
	return ix
}

// openTestStore opens a new library, closed when the test ends.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(core.PathsIn(t.TempDir()).DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// load is the next cumulative reading after last: elapsed processor time,
// of which others went to other programs and own to Lumen.
func load(last cpuTimes, total, others, own time.Duration) cpuTimes {
	return cpuTimes{total: last.total + total, busy: last.busy + others + own, own: last.own + own}
}

func TestBusyNeedsAWholeWindowOfLoadAndLastsUntilItFallsWellBelow(t *testing.T) {
	var m loadMeter
	var at cpuTimes
	step := func(othersShare float64) bool {
		at = load(at, time.Second, time.Duration(othersShare*float64(time.Second)), 0)
		m.add(at)
		return m.busy
	}
	step(0) // the first reading has nothing to compare with
	for i := range busyWindow - 1 {
		if step(1) {
			t.Fatalf("busy after %d looks at full load; it must wait for %d", i+1, busyWindow)
		}
	}
	if !step(1) {
		t.Fatal("a whole window at full load is not busy")
	}
	// The average falls below busyOn but stays above busyOff: still busy.
	for range 2 {
		if !step(0.4) {
			t.Fatal("busy ended before the load fell to busyOff")
		}
	}
	for range busyWindow {
		step(0.1)
	}
	if m.busy {
		t.Fatal("still busy after a whole window of light load")
	}
}

func TestLumensOwnWorkNeverMakesThePCBusy(t *testing.T) {
	var m loadMeter
	var at cpuTimes
	for range 3 * busyWindow {
		at = load(at, time.Second, 50*time.Millisecond, 950*time.Millisecond) // Lumen busy, others not
		m.add(at)
	}
	if m.busy {
		t.Fatal("Lumen's own load counted as other programs keeping the PC busy")
	}
}

func TestCountersGoingBackAreIgnored(t *testing.T) {
	var m loadMeter
	m.add(cpuTimes{total: 10 * time.Second, busy: 9 * time.Second})
	m.add(cpuTimes{total: 5 * time.Second, busy: 5 * time.Second})
	if m.n != 0 {
		t.Fatalf("a reading behind the last was counted: %d looks", m.n)
	}
}

func TestWaitingNamesTheFirstHoldTheUserAskedFor(t *testing.T) {
	all := pcState{pauseFullScreen: true, pauseBusy: true, idleOnly: true}
	for _, c := range []struct {
		name string
		o    pcObservation
		want core.WaitReason
	}{
		{"present, nothing in front", pcObservation{}, core.WaitIdle},
		{"away", pcObservation{away: true}, core.WaitNone},
		{"busy", pcObservation{busy: true, away: true}, core.WaitBusy},
		{"full screen and busy", pcObservation{fullScreen: true, busy: true}, core.WaitFullScreen},
	} {
		s := all
		s.pcObservation = c.o
		if got := s.waiting(); got != c.want {
			t.Errorf("%s: waiting = %q, want %q", c.name, got, c.want)
		}
	}
	if w := (pcState{pcObservation: pcObservation{fullScreen: true, busy: true}}).waiting(); w != core.WaitNone {
		t.Errorf("with every setting off, waiting = %q", w)
	}
}

func TestTheDefaultsGiveWayToFullScreenAndBusyButNotToThePresentUser(t *testing.T) {
	ix := New(openTestStore(t), core.Paths{}, nil, Models{}, &fakeEvents{})
	ix.pc = &fakePC{full: true}
	ix.loadPCSettings(t.Context())
	if w := ix.waiting(); w != core.WaitFullScreen {
		t.Fatalf("a game in front: waiting = %q, want %q", w, core.WaitFullScreen)
	}
	ix.observePC(pcObservation{busy: true})
	if w := ix.waiting(); w != core.WaitBusy {
		t.Fatalf("a busy PC: waiting = %q, want %q", w, core.WaitBusy)
	}
	ix.observePC(pcObservation{})
	if w := ix.waiting(); w != core.WaitNone {
		t.Fatalf("the user at a quiet PC: waiting = %q, want none (idle-only is off by default)", w)
	}
}

func TestAWaitingWorkerGoesOnOnceTheHoldLifts(t *testing.T) {
	for _, c := range []struct {
		name string
		hold func(*Indexer)
		lift func(*Indexer)
	}{
		{"the game closes", func(ix *Indexer) { ix.observePC(pcObservation{fullScreen: true}) },
			func(ix *Indexer) { ix.observePC(pcObservation{}) }},
		{"the user turns the setting off", func(ix *Indexer) { ix.observePC(pcObservation{busy: true}) },
			func(ix *Indexer) { ix.SetPauseBusy(false) }},
		{"the user goes away", func(ix *Indexer) { ix.SetIdleOnly(true) },
			func(ix *Indexer) { ix.observePC(pcObservation{away: true}) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			ix := quiet(New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}))
			ix.pcState = pcState{pauseFullScreen: true, pauseBusy: true}
			c.hold(ix)
			done := make(chan bool, 1)
			go func() { done <- ix.waitTurn(t.Context(), core.StageMetadata, nil) }()
			select {
			case <-done:
				t.Fatal("the worker went on while held")
			case <-time.After(50 * time.Millisecond):
			}
			c.lift(ix)
			select {
			case ok := <-done:
				if !ok {
					t.Fatal("waitTurn gave up")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the worker still waits after the hold lifted")
			}
		})
	}
}

func TestProgressSaysWhyIndexingWaits(t *testing.T) {
	ix := quiet(New(openTestStore(t), core.Paths{}, nil, Models{}, &fakeEvents{}))
	ix.pcState = pcState{pauseBusy: true}
	ix.observePC(pcObservation{busy: true})
	if p := ix.Progress(t.Context()); p.Waiting != core.WaitBusy {
		t.Fatalf("progress waiting = %q, want %q", p.Waiting, core.WaitBusy)
	}
}

func TestPCLoopFollowsThePC(t *testing.T) {
	ix := quiet(New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}))
	p := &fakePC{}
	ix.pc = p
	ix.pcState = pcState{pauseFullScreen: true}
	ix.run(func() { ix.pcLoop(t.Context()) })
	t.Cleanup(ix.wg.Wait)
	p.mu.Lock()
	p.full = true
	p.mu.Unlock()
	deadline := time.Now().Add(pcPoll + 5*time.Second)
	for {
		ix.mu.Lock()
		w := ix.waiting()
		ix.mu.Unlock()
		if w == core.WaitFullScreen {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the loop never saw the full-screen app")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

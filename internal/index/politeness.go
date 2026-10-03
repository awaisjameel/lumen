package index

import (
	"context"
	"log/slog"
	"time"

	"lumen/internal/core"
)

// How indexing gives way to the user (plan §6): it waits on battery (the AI
// only, unless allowed), while a game or full-screen app is in front, while
// other programs keep the processor busy, and, if the user asks, until they
// are away from the PC.
const (
	// pcPoll is how often the PC is looked at. Each look is a handful of
	// cheap system calls.
	pcPoll = 5 * time.Second
	// AwayAfter is how long without keyboard or mouse input counts as the
	// user being away. frontend/src/bridge mirrors it.
	AwayAfter = 5 * time.Minute
	// busyWindow is how many looks the processor load is averaged over (30 s
	// at pcPoll), so a moment's spike doesn't stop indexing.
	busyWindow = 6
	// Other programs keep the PC busy from busyOn of all its processor
	// time, until they fall to busyOff: the gap stops it flapping at a line.
	busyOn  = 0.70
	busyOff = 0.50
)

// pc is what the indexer reads about the computer to give way to its user.
// A test gives the indexer a fake one.
type pc interface {
	onBattery() bool
	// fullScreen reports whether a game, full-screen video or presentation
	// is in front. Lumen's own window never counts.
	fullScreen() bool
	// sinceInput is how long ago the user last used the keyboard or mouse.
	sinceInput() time.Duration
	// cpuTimes are the processor times so far; false if they can't be read.
	cpuTimes() (cpuTimes, bool)
}

// cpuTimes are cumulative processor times, summed over every core: the
// machine's total and busy time, and Lumen's own share of the busy time (its
// window and the FFmpeg runs it started included).
type cpuTimes struct {
	total, busy, own time.Duration
}

// pcObservation is one look at what the PC is doing.
type pcObservation struct {
	onBattery  bool
	fullScreen bool
	busy       bool // other programs keep the processor busy (see loadMeter)
	away       bool // no input for AwayAfter
}

// pcState is what the PC is doing and what the user lets indexing do
// meanwhile: everything but a pause that holds the stages back.
type pcState struct {
	pcObservation
	// What the user chose (the settings of the same names).
	aiOnBattery     bool
	pauseFullScreen bool
	pauseBusy       bool
	idleOnly        bool
}

// waiting is why every stage waits for the PC, WaitNone if nothing holds them.
func (s pcState) waiting() core.WaitReason {
	switch {
	case s.pauseFullScreen && s.fullScreen:
		return core.WaitFullScreen
	case s.pauseBusy && s.busy:
		return core.WaitBusy
	case s.idleOnly && !s.away:
		return core.WaitIdle
	}
	return core.WaitNone
}

// aiWaitsForMains reports whether the AI waits for mains power.
func (s pcState) aiWaitsForMains() bool { return s.onBattery && !s.aiOnBattery }

// observe looks at the PC once, adding its processor load to load.
func observe(p pc, load *loadMeter) pcObservation {
	o := pcObservation{onBattery: p.onBattery(), fullScreen: p.fullScreen(), away: p.sinceInput() >= AwayAfter}
	if t, ok := p.cpuTimes(); ok {
		load.add(t)
	}
	o.busy = load.busy
	return o
}

// loadMeter tells from successive cpuTimes whether other programs keep the
// processor busy, averaged over busyWindow looks.
type loadMeter struct {
	last   cpuTimes
	primed bool // last holds a look
	shares [busyWindow]float64
	n      int // looks in shares, up to busyWindow
	next   int // where the next one goes
	busy   bool
}

// add takes the next look.
func (m *loadMeter) add(t cpuTimes) {
	last, primed := m.last, m.primed
	m.last, m.primed = t, true
	total := t.total - last.total
	if !primed || total <= 0 {
		return // nothing to compare with, or the counters went back
	}
	others := (t.busy - last.busy) - (t.own - last.own)
	m.shares[m.next] = min(1, max(0, float64(others)/float64(total)))
	m.next = (m.next + 1) % busyWindow
	m.n = min(m.n+1, busyWindow)
	var sum float64
	for _, s := range m.shares[:m.n] {
		sum += s
	}
	switch avg := sum / float64(m.n); {
	case !m.busy && m.n == busyWindow && avg >= busyOn:
		m.busy = true
	case m.busy && avg <= busyOff:
		m.busy = false
	}
}

// pcLoop follows the PC, so the stages wait while it needs them to, and go
// on as soon as it doesn't.
func (ix *Indexer) pcLoop(ctx context.Context) {
	var load loadMeter
	t := time.NewTicker(pcPoll)
	defer t.Stop()
	for {
		ix.observePC(observe(ix.pc, &load))
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// observePC records a look at the PC, waking the waiting workers if it
// changes what holds them.
func (ix *Indexer) observePC(o pcObservation) {
	ix.mu.Lock()
	was := ix.pcState
	ix.mu.Unlock()
	if o == was.pcObservation {
		return
	}
	ix.setHold(func() { ix.pcObservation = o })
	if o.onBattery != was.onBattery {
		slog.Info("power source changed", "battery", o.onBattery)
	}
	now := was
	now.pcObservation = o
	if w := now.waiting(); w != was.waiting() {
		slog.Info("indexing gives way to the user", "waiting", string(w))
	}
}

// SetAIOnBattery lets the AI keep working on battery power, or not.
func (ix *Indexer) SetAIOnBattery(on bool) { ix.setHold(func() { ix.aiOnBattery = on }) }

// SetPauseFullScreen makes indexing wait while a game or full-screen app is
// in front, or not.
func (ix *Indexer) SetPauseFullScreen(on bool) { ix.setHold(func() { ix.pauseFullScreen = on }) }

// SetPauseBusy makes indexing wait while other programs keep the PC busy, or not.
func (ix *Indexer) SetPauseBusy(on bool) { ix.setHold(func() { ix.pauseBusy = on }) }

// SetIdleOnly makes indexing run only while the user is away, or whenever
// nothing else holds it.
func (ix *Indexer) SetIdleOnly(on bool) { ix.setHold(func() { ix.idleOnly = on }) }

// loadPCSettings reads what the user lets indexing do, and takes a first
// look at the PC, before any stage starts. A setting that can't be read
// keeps its default, and the failure is logged.
func (ix *Indexer) loadPCSettings(ctx context.Context) {
	read := func(key string) bool {
		on, err := ix.store.Bool(ctx, key, core.DefaultOn(key))
		if err != nil {
			slog.Error("read settings", "key", key, "err", err)
			return core.DefaultOn(key)
		}
		return on
	}
	s := pcState{
		pcObservation:   observe(ix.pc, &loadMeter{}),
		aiOnBattery:     read(core.SettingAIOnBattery),
		pauseFullScreen: read(core.SettingPauseFullScreen),
		pauseBusy:       read(core.SettingPauseBusy),
		idleOnly:        read(core.SettingIdleOnly),
	}
	ix.setHold(func() { ix.pcState = s })
}

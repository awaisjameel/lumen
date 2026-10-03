// Package diag keeps what Lumen records about itself for the user to look
// at, or to share if they choose: crash reports, and the problem report that
// gathers them with the log and a description of the PC. Nothing here is
// ever sent anywhere.
package diag

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"
)

// keepReports is how many crash reports are kept, newest first.
const keepReports = 20

// pendingName is the file a running Lumen hands to the Go runtime for its
// last words. A clean exit removes it, so one found at start means the last
// run ended abruptly.
const pendingName = "pending.txt"

// headerEnd ends what Lumen writes into the pending file itself; anything
// after it is the runtime's account of a crash.
const headerEnd = "---\n"

// ErrArmed is Start called while crash reports are already being taken: the
// runtime has one crash output per process.
var ErrArmed = errors.New("crash reports are already being taken")

// Reports are the crash reports in a data folder's crashes folder.
type Reports struct {
	dir     string
	version string

	mu        sync.Mutex // guards armed, started and lastCrash
	armed     bool
	started   time.Time // when this run started taking reports; zero before
	lastCrash string    // the report the last run left, "" if it ended cleanly
}

// New returns the reports kept under dataDir, made by Lumen version. It
// touches no file: Start does, once this copy of Lumen knows it is the only
// one using dataDir.
func New(dataDir, version string) *Reports {
	return &Reports{dir: filepath.Join(dataDir, "crashes"), version: version}
}

// Dir is the folder the reports are in.
func (r *Reports) Dir() string { return r.dir }

// Start keeps the report a crashed last run left, if any, then has the Go
// runtime write the account of a crash of this run. Call stop when
// Lumen exits cleanly.
func (r *Reports) Start(now time.Time) (stop func(), err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.armed {
		return nil, ErrArmed
	}
	if err := os.MkdirAll(r.dir, 0o750); err != nil {
		return nil, fmt.Errorf("create crash report folder: %w", err)
	}
	r.lastCrash = ""
	if err := r.keepLastCrashLocked(); err != nil {
		return nil, err // never truncate a crash report that could not be kept
	}
	return r.armLocked(now)
}

// LastCrash is the name of the report the last run left when it crashed, ""
// if it ended cleanly.
func (r *Reports) LastCrash() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastCrash
}

// keepLastCrashLocked turns a pending file with a crash in it into a report. One
// with only Lumen's header (the PC lost power, or Lumen was ended from Task
// Manager) has nothing to say and goes. Call with mu held.
func (r *Reports) keepLastCrashLocked() error {
	pending := filepath.Join(r.dir, pendingName)
	b, err := os.ReadFile(pending)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read last run's crash output: %w", err)
	}
	_, crash, header := bytes.Cut(b, []byte(headerEnd))
	if !header {
		crash = b // preserve output whose header was incomplete
	}
	if len(bytes.TrimSpace(crash)) == 0 {
		return os.Remove(pending)
	}
	at := time.Now()
	if fi, err := os.Stat(pending); err == nil {
		at = fi.ModTime() // when the runtime last wrote: the crash
	}
	name := reportName("crash", at)
	if err := os.Rename(pending, filepath.Join(r.dir, name)); err != nil {
		return fmt.Errorf("keep last run's crash report: %w", err)
	}
	slog.Warn("lumen crashed last time; report kept", "report", name)
	r.lastCrash = name
	r.prune()
	return nil
}

// armLocked creates the pending file with a header saying what was running, and
// hands it to the runtime. The runtime keeps its own handle, so the file is
// closed here; stop takes the handle back and removes the file. Call with mu held.
func (r *Reports) armLocked(now time.Time) (stop func(), err error) {
	pending := filepath.Join(r.dir, pendingName)
	f, err := os.OpenFile(pending, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create crash output: %w", err)
	}
	r.started = now
	_, err = fmt.Fprintf(f, "%s%s", r.headerLocked(), headerEnd)
	if err == nil {
		err = debug.SetCrashOutput(f, debug.CrashOptions{})
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(pending)
		return nil, fmt.Errorf("take crash reports: %w", err)
	}
	r.armed = true
	return sync.OnceFunc(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if err := debug.SetCrashOutput(nil, debug.CrashOptions{}); err != nil {
			slog.Warn("stop taking crash reports", "err", err)
			return
		}
		if err := os.Remove(pending); err != nil {
			slog.Warn("remove crash output", "err", err)
		}
		r.armed = false
	}), nil
}

// RecordPanic keeps a report of a panic that was recovered (Wails recovers
// one in a call from the UI, which then fails): Lumen runs on, but it is a
// bug worth reporting.
func (r *Reports) RecordPanic(err error, stack string, now time.Time) {
	name := reportName("panic", now)
	r.mu.Lock()
	header := r.headerLocked()
	r.mu.Unlock()
	text := fmt.Sprintf("%sRecovered %s\n%spanic: %v\n\n%s\n", header, now.Format(time.RFC3339), headerEnd, err, stack)
	if werr := os.MkdirAll(r.dir, 0o750); werr != nil {
		slog.Error("keep panic report", "err", werr)
		return
	}
	// Two recovered panics can have the same timestamp. Exclusive creation
	// keeps both, including when calls from the UI fail concurrently.
	f, werr := os.CreateTemp(r.dir, strings.TrimSuffix(name, ".txt")+"-*.txt")
	if werr != nil {
		slog.Error("keep panic report", "err", werr)
		return
	}
	_, werr = f.WriteString(text)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(f.Name()) // discard this call's incomplete report
		slog.Error("keep panic report", "err", werr)
		return
	}
	r.prune()
}

// headerLocked says which Lumen on which Windows was running, and since
// when. Call with mu held.
func (r *Reports) headerLocked() string {
	h := fmt.Sprintf("Lumen %s\nWindows %s\n", r.version, windowsVersion())
	if !r.started.IsZero() {
		h += fmt.Sprintf("Started %s\n", r.started.Format(time.RFC3339))
	}
	return h
}

// reportName names a report by its kind and time, so names sort by age.
func reportName(kind string, at time.Time) string {
	return fmt.Sprintf("%s-%s.txt", kind, at.Format("20060102-150405.000"))
}

// Files lists the reports, newest first.
func (r *Reports) Files() ([]string, error) {
	entries, err := os.ReadDir(r.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && (strings.HasPrefix(n, "crash-") || strings.HasPrefix(n, "panic-")) && strings.HasSuffix(n, ".txt") {
			names = append(names, n)
		}
	}
	// The time after the kind sorts them; crash and panic reports mix.
	slices.SortFunc(names, func(a, b string) int {
		return strings.Compare(b[strings.IndexByte(b, '-'):], a[strings.IndexByte(a, '-'):])
	})
	return names, nil
}

// prune removes all but the newest keepReports reports.
func (r *Reports) prune() {
	names, err := r.Files()
	if err != nil {
		slog.Warn("list crash reports", "err", err)
		return
	}
	for _, n := range names[min(len(names), keepReports):] {
		if err := os.Remove(filepath.Join(r.dir, n)); err != nil {
			slog.Warn("remove old crash report", "err", err)
		}
	}
}

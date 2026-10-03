package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/diag"
	"lumen/internal/store"
)

// The pages outside Lumen the UI can open (core.Link), in the user's
// browser: Lumen itself makes no network request. build/windows/nsis
// carries ReleasesURL too, checked by main's TestInstallerNamesTheReleasesPage.
const (
	ReleasesURL = "https://github.com/awaisjameel/lumen/releases"
	geoNamesURL = "https://www.geonames.org/"
)

// linkURLs is where each core.Link leads. A page not listed is never opened.
var linkURLs = map[core.Link]string{core.LinkReleases: ReleasesURL, core.LinkGeoNames: geoNamesURL}

// How much the UI may write to the log: a view failing on every frame must
// not fill it.
const (
	uiLogPerMinute = 30
	uiLogMaxField  = 4 << 10 // bytes of a message or a stack
)

// SystemService is about Lumen itself: its version, its log and crash
// reports, the problem report a user may choose to share, and the UI's own
// failures.
type SystemService struct {
	lib     *LibraryService // the settings and progress a problem report describes
	reports *diag.Reports
	paths   core.Paths
	version string
	now     func() time.Time
	uiLog   uiLogLimit
}

// NewSystemService returns the system service of the Lumen at version,
// whose crash reports are reports and whose library is lib's.
func NewSystemService(lib *LibraryService, reports *diag.Reports, paths core.Paths, version string) *SystemService {
	return &SystemService{lib: lib, reports: reports, paths: paths, version: version, now: time.Now}
}

// About says which Lumen this is, where its data is, and whether the last
// run crashed.
func (s *SystemService) About() core.About {
	return core.About{Version: s.version, DataDir: s.paths.DataDir, ReleasesURL: ReleasesURL,
		LastRunCrashed: s.reports.LastCrash() != ""}
}

// OpenLink opens one of the pages outside Lumen in the user's browser.
func (s *SystemService) OpenLink(link core.Link) error {
	url, ok := linkURLs[link]
	if !ok {
		return fmt.Errorf("%w: no page %q", store.ErrNotFound, link)
	}
	return application.Get().Browser.OpenURL(url)
}

// OpenNotices opens the licence notices of what ships with Lumen.
func (s *SystemService) OpenNotices() error {
	name := s.paths.Notices() // "" without a runtime folder, which Stat fails on too
	if fi, err := os.Stat(name); err != nil || fi.IsDir() {
		return fmt.Errorf("%w: the licence notices are not beside Lumen.exe", store.ErrNotFound)
	}
	return application.Get().Browser.OpenFile(name)
}

// OpenLogFolder opens Explorer on Lumen's data folder, its log selected.
func (s *SystemService) OpenLogFolder(ctx context.Context) error {
	return showInFolder(ctx, s.paths.Log)
}

// SaveProblemReport asks where to save a problem report, then writes it
// there: the log, the crash reports and a description of the PC and the
// library. It returns where it was saved, "" if the user cancelled.
func (s *SystemService) SaveProblemReport(ctx context.Context) (string, error) {
	name := fmt.Sprintf("Lumen problem report %s.zip", s.now().Format("2006-01-02"))
	path, err := application.Get().Dialog.SaveFile().
		SetFilename(name).
		AddFilter("Zip file", "*.zip").
		PromptForSingleSelection()
	if (err != nil && err.Error() == dialogCancelled) || (err == nil && path == "") {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := s.writeProblemReport(ctx, path); err != nil {
		return "", err
	}
	return path, nil
}

// writeProblemReport writes the report to path, leaving nothing there if it
// fails.
func (s *SystemService) writeProblemReport(ctx context.Context, path string) (err error) {
	about, err := s.describe(ctx)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	return s.reports.WriteReport(f, diag.Report{About: about, Logs: []string{s.paths.OldLog, s.paths.Log}})
}

// describe is a problem report's system.txt: which Lumen, the PC, how
// indexing stands and the settings. It names none of the library's files or
// folders, nor where Lumen keeps its data.
func (s *SystemService) describe(ctx context.Context) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Lumen %s\nReport made %s\n\n", s.version, s.now().Format(time.RFC3339))
	b.WriteString(diag.System().String())
	p := s.lib.Progress(ctx)
	fmt.Fprintf(&b, "\nAI device: %s\nAI error: %s\nFaces error: %s\n", p.AI.Device, p.AI.Error, p.AI.FacesError)
	fmt.Fprintf(&b, "Items: %d\nWaiting: metadata %d, thumbnails %d, photos %d, faces %d, videos %d, text %d\n",
		p.Total, p.Metadata, p.Thumbnail, p.Embedding, p.Faces, p.Videos, p.OCR)
	fmt.Fprintf(&b, "Couldn't be read: %d\nSkipped: %d\nPaused: %t\nWaiting for: %q\n", p.Errors, p.Skipped, p.Paused, p.Waiting)
	settings, err := s.lib.Settings(ctx)
	if err != nil {
		return "", fmt.Errorf("read settings: %w", err)
	}
	js, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "\nSettings: %s\n", js)
	return b.String(), nil
}

// LogUIError writes a failure in the UI to Lumen's log, which has no other
// way to see it.
func (s *SystemService) LogUIError(e core.UIError) {
	ok, dropped := s.uiLog.allow(s.now())
	if dropped > 0 {
		slog.Warn("ui failures not logged: too many", "count", dropped)
	}
	if ok {
		slog.Error("ui failure", "where", clip(e.Where), "err", clip(e.Message), "stack", clip(e.Stack))
	}
}

// clip shortens a field from the UI to uiLogMaxField bytes, on a rune boundary.
func clip(s string) string {
	if len(s) <= uiLogMaxField {
		return s
	}
	cut := uiLogMaxField
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// uiLogLimit lets uiLogPerMinute UI failures into the log each minute, and
// counts the rest.
type uiLogLimit struct {
	mu      sync.Mutex // guards the fields
	since   time.Time  // the current minute's start
	logged  int
	dropped int
}

// allow reports whether a failure at now may be logged, and how many were
// dropped in the minute that just ended (reported once).
func (l *uiLogLimit) allow(now time.Time) (ok bool, dropped int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.since) >= time.Minute {
		dropped = l.dropped
		l.since, l.logged, l.dropped = now, 0, 0
	}
	if l.logged >= uiLogPerMinute {
		l.dropped++
		return false, dropped
	}
	l.logged++
	return true, dropped
}

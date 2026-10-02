// Package services holds the Wails services the UI calls. Each method is
// turned into a typed TypeScript function in frontend/bindings. Services
// translate between the UI and the internal packages: they check what the
// UI sends, combine the packages' answers (a search asks people, search and
// the store), and name failures the UI explains (see ErrorCode). The
// engine's rules live in the internal packages.
package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/index"
	"lumen/internal/store"
)

// Events the UI can subscribe to.
const (
	EventProgress       = "index:progress"
	EventLibraryChanged = "library:changed"
)

func init() {
	application.RegisterEvent[core.Progress](EventProgress)
	application.RegisterEvent[core.Changes](EventLibraryChanged)
}

// Emitter forwards indexer events to the UI (it implements index.Events).
// App is set once the application exists; events before that are dropped.
type Emitter struct{ App *application.App }

// Progress sends the indexing progress.
func (e *Emitter) Progress(p core.Progress) {
	if e.App != nil {
		e.App.Event.Emit(EventProgress, p)
	}
}

// LibraryChanged says what changed in the library.
func (e *Emitter) LibraryChanged(c core.Changes) {
	if e.App != nil {
		e.App.Event.Emit(EventLibraryChanged, c)
	}
}

// LibraryService is everything about browsing and managing the library.
type LibraryService struct {
	store   *store.Store
	ix      *index.Indexer
	now     func() time.Time // the clock: time.Now, or a test's
	recycle recycler         // moves files to the Recycle Bin; nil means fileops.Recycle (a test's fake otherwise)
	blocked struct {         // the PIN, rate limiting and Undo; guarded by its mutex
		sync.Mutex
		unlocked bool
		usedAt   time.Time // the unlocked list's last use (see blockUnlockIdle)
		failures int
		retryAt  time.Time
		lastID   int64
		lastAt   time.Time
	}
	// verify is the Windows sign-in of ResetBlockPIN; nil means signin.Verify.
	// A test sets it to answer for the dialog it cannot show.
	verify func(ctx context.Context, owner uintptr, reason string) error
}

// NewLibraryService returns the library service.
func NewLibraryService(st *store.Store, ix *index.Indexer) *LibraryService {
	return &LibraryService{store: st, ix: ix, now: time.Now}
}

// Sources lists the folders in the library.
func (s *LibraryService) Sources(ctx context.Context) ([]core.Source, error) {
	return s.store.ListSources(ctx)
}

// SuggestedFolders returns likely photo folders that exist on this machine,
// for the first-run screen.
func (s *LibraryService) SuggestedFolders() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []string{
		filepath.Join(home, "Pictures"),
		filepath.Join(home, "Videos"),
		filepath.Join(home, "OneDrive", "Pictures"),
		filepath.Join(home, "OneDrive", "Pictures", "Camera Roll"),
		filepath.Join(home, "Downloads", "WhatsApp Images"),
		filepath.Join(home, "DCIM"),
	}
	for _, drive := range "DEFGH" {
		candidates = append(candidates, string(drive)+`:\DCIM`, string(drive)+`:\Photos`)
	}
	var out []string
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			covered := false
			for _, o := range out {
				covered = covered || core.IsWithin(c, o)
			}
			if !covered {
				out = append(out, c)
			}
		}
	}
	return out
}

// dialogCancelled is the message Wails' Windows file dialog fails with when
// the user closes it (its error value is internal to Wails).
const dialogCancelled = "cancelled by user"

// PickFolder opens the native folder picker. It returns "" if cancelled.
func (s *LibraryService) PickFolder() (string, error) {
	path, err := application.Get().Dialog.OpenFile().
		SetTitle("Choose a folder with photos").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(false).
		PromptForSingleSelection()
	if err != nil && err.Error() == dialogCancelled {
		return "", nil
	}
	return path, err
}

// AddSources adds folders and starts scanning them, all or none: a folder
// that doesn't exist fails the call before any is added. Folders already
// inside the library are skipped; if that leaves nothing to add, the call
// fails with store.ErrAlreadyCovered, so the UI can say so.
func (s *LibraryService) AddSources(ctx context.Context, paths []string) ([]core.Source, error) {
	for _, p := range paths {
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("%w: %s", ErrFolderNotFound, p)
		}
	}
	added := false
	defer func() {
		if added {
			s.ix.RescanAll() // scan what was added, even if a later folder failed
		}
	}()
	covered := false
	for _, p := range paths {
		switch _, err := s.store.AddSource(ctx, p); {
		case err == nil:
			added = true
		case errors.Is(err, store.ErrAlreadyCovered):
			covered = true
		default:
			return nil, err
		}
	}
	if covered && !added {
		return nil, store.ErrAlreadyCovered
	}
	return s.store.ListSources(ctx)
}

// RemoveSource takes a folder out of the library. Files on disk are untouched.
func (s *LibraryService) RemoveSource(ctx context.Context, id int64) error {
	if err := s.store.RemoveSource(ctx, id); err != nil {
		return err
	}
	s.ix.RescanAll() // restarts watchers without the removed folder
	return nil
}

// Rescan checks every folder for changes.
func (s *LibraryService) Rescan() { s.ix.RescanAll() }

// Timeline returns the view's asset counts per day, newest first.
func (s *LibraryService) Timeline(ctx context.Context, view core.View) ([]core.DayBucket, error) {
	minScore, err := tagConfidence(ctx, s.store)
	if err != nil {
		return nil, err
	}
	return s.store.DayBuckets(ctx, view, minScore)
}

// Days returns the view's grid items for the days from..to (YYYY-MM-DD, inclusive).
func (s *LibraryService) Days(ctx context.Context, from, to string, view core.View) ([]core.DayItems, error) {
	minScore, err := tagConfidence(ctx, s.store)
	if err != nil {
		return nil, err
	}
	return s.store.DayItems(ctx, from, to, view, minScore)
}

// Focus tells the indexer which days are on screen so they are processed first.
func (s *LibraryService) Focus(from, to string) { s.ix.SetFocus(from, to) }

// Asset returns the details for one asset.
func (s *LibraryService) Asset(ctx context.Context, id int64) (core.AssetDetail, error) {
	return s.store.AssetDetail(ctx, id)
}

// DuplicateGroups returns exact file copies and likely similar photos for review.
func (s *LibraryService) DuplicateGroups(ctx context.Context) ([]core.DuplicateGroup, error) {
	return s.store.DuplicateGroups(ctx)
}

// Places lists nearby cities found from GPS coordinates using bundled data.
func (s *LibraryService) Places(ctx context.Context) ([]core.Place, error) {
	return s.store.Places(ctx)
}

// Place returns one city and its current visible count.
func (s *LibraryService) Place(ctx context.Context, id int64) (core.Place, error) {
	return s.store.Place(ctx, id)
}

// Events detects time and location groups in the currently visible timeline.
func (s *LibraryService) Events(ctx context.Context) ([]core.Event, error) {
	gap, err := s.store.EventGapHours(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.Events(ctx, gap)
}

// Event returns one group, including any name chosen by the user.
func (s *LibraryService) Event(ctx context.Context, id int64) (core.Event, error) {
	gap, err := s.store.EventGapHours(ctx)
	if err != nil {
		return core.Event{}, err
	}
	return s.store.Event(ctx, id, gap)
}

// SetEventName renames an event; an empty name restores its generated title.
func (s *LibraryService) SetEventName(ctx context.Context, id int64, name string) error {
	gap, err := s.store.EventGapHours(ctx)
	if err != nil {
		return err
	}
	return s.store.SetEventName(ctx, id, name, gap)
}

// SetFavourite marks or unmarks a favourite.
func (s *LibraryService) SetFavourite(ctx context.Context, id int64, fav bool) error {
	return s.store.SetFavourite(ctx, id, fav)
}

// Albums lists saved collections with their current counts and covers.
func (s *LibraryService) Albums(ctx context.Context) ([]core.Album, error) {
	minScore, err := tagConfidence(ctx, s.store)
	if err != nil {
		return nil, err
	}
	return s.store.Albums(ctx, minScore)
}

// CreateAlbum saves a new manual album holding items, or a smart rule (no
// items), in one step.
func (s *LibraryService) CreateAlbum(ctx context.Context, name string, kind core.AlbumKind, rule core.AlbumRule,
	items []int64) (int64, error) {
	return s.store.CreateAlbum(ctx, name, kind, rule, items)
}

// UpdateAlbum changes an album's name and smart rule.
func (s *LibraryService) UpdateAlbum(ctx context.Context, id int64, name string, rule core.AlbumRule) error {
	return s.store.UpdateAlbum(ctx, id, name, rule)
}

// DeleteAlbum removes an album without touching media files.
func (s *LibraryService) DeleteAlbum(ctx context.Context, id int64) error {
	return s.store.DeleteAlbum(ctx, id)
}

// AssetAlbums lists the manual albums containing an asset.
func (s *LibraryService) AssetAlbums(ctx context.Context, id int64) ([]int64, error) {
	return s.store.AssetAlbums(ctx, id)
}

// SetAlbumItem changes membership in a manual album.
func (s *LibraryService) SetAlbumItem(ctx context.Context, assetID, albumID int64, include bool) error {
	return s.store.SetAlbumItem(ctx, assetID, albumID, include)
}

// ShowInFolder opens Explorer with one of the asset's files selected: path,
// which must be one of them, or "" for the first that is there.
func (s *LibraryService) ShowInFolder(ctx context.Context, id int64, path string) error {
	f, err := s.store.AssetFile(ctx, id)
	if err != nil {
		return err
	}
	switch {
	case path != "":
		if !slices.Contains(f.Paths, path) {
			return store.ErrNotFound
		}
	default:
		i := slices.IndexFunc(f.Paths, func(p string) bool { _, err := os.Stat(p); return err == nil })
		if i < 0 {
			return fmt.Errorf("%w: no copy of the file is available", store.ErrNotFound)
		}
		path = f.Paths[i]
	}
	return showInFolder(ctx, path)
}

func showInFolder(ctx context.Context, path string) error {
	// Explorer outlives this call (and Lumen), so it gets no deadline.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), "explorer.exe", "/select,", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release() // Explorer outlives us; don't wait for it
}

// Progress returns the current indexing state.
func (s *LibraryService) Progress(ctx context.Context) core.Progress { return s.ix.Progress(ctx) }

// SetPaused pauses or resumes background indexing.
func (s *LibraryService) SetPaused(paused bool) { s.ix.SetPaused(paused) }

// Settings returns the user's settings.
func (s *LibraryService) Settings(ctx context.Context) (core.Settings, error) {
	videos, err := s.store.Bool(ctx, core.SettingVideos, false)
	if err != nil {
		return core.Settings{}, err
	}
	battery, err := s.store.Bool(ctx, core.SettingAIOnBattery, false)
	if err != nil {
		return core.Settings{}, err
	}
	conf, err := tagConfidence(ctx, s.store)
	if err != nil {
		return core.Settings{}, err
	}
	gap, err := s.store.EventGapHours(ctx)
	if err != nil {
		return core.Settings{}, err
	}
	cacheMB, err := s.store.ThumbCacheMB(ctx)
	if err != nil {
		return core.Settings{}, err
	}
	ocr, err := s.store.Bool(ctx, core.SettingOCR, false)
	return core.Settings{Videos: videos, AIOnBattery: battery, TagConfidence: conf, EventGapHours: gap,
		ThumbCacheMB: cacheMB, OCR: ocr}, err
}

// SetEventGapHours sets the quiet period that splits detected events.
func (s *LibraryService) SetEventGapHours(ctx context.Context, hours int) error {
	hours = min(max(hours, core.EventGapMin), core.EventGapMax)
	return s.store.SetEventGapHours(ctx, hours)
}

// SetAIOnBattery lets the AI keep working on battery power (it waits for
// mains power by default).
func (s *LibraryService) SetAIOnBattery(ctx context.Context, on bool) error {
	if err := s.store.SetBool(ctx, core.SettingAIOnBattery, on); err != nil {
		return err
	}
	s.ix.SetAIOnBattery(on)
	return nil
}

// SetTagConfidence sets how sure the AI must be for a tag to apply. Stored
// scores cover the whole range, so this takes effect at once.
func (s *LibraryService) SetTagConfidence(ctx context.Context, v float64) error {
	v = min(max(v, core.TagConfidenceMin), core.TagConfidenceMax)
	if err := s.store.SetFloat(ctx, core.SettingTagConfidence, v); err != nil {
		return err
	}
	// A lower confidence can make more photos screenshots or documents.
	return s.ix.QueueOCR(ctx)
}

// SetVideos turns video indexing on or off, then rescans so the change applies.
func (s *LibraryService) SetVideos(ctx context.Context, on bool) error {
	if err := s.store.SetBool(ctx, core.SettingVideos, on); err != nil {
		return err
	}
	s.ix.RescanAll()
	return nil
}

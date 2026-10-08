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
	"lumen/internal/media"
	"lumen/internal/store"
)

// Events the UI can subscribe to.
const (
	EventProgress         = "index:progress"
	EventLibraryChanged   = "library:changed"
	EventOrganiseProgress = "organise:progress" // a run moving or undoing files
)

func init() {
	application.RegisterEvent[core.Progress](EventProgress)
	application.RegisterEvent[core.Changes](EventLibraryChanged)
	application.RegisterEvent[core.OrganiseProgress](EventOrganiseProgress)
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
	media   *media.Cache
	store   *store.Store
	ix      *index.Indexer
	now     func() time.Time // the clock: time.Now, or a test's
	recycle recycler         // moves files to the Recycle Bin; nil means fileops.Recycle (a test's fake otherwise)
	open    opener           // hands a file to the shell; nil means fileops.Open (a test's fake otherwise)
	gate    *BlockGate       // whether the Blocked screen is unlocked, shared with media serving
	// recycling serialises every move of files to the Recycle Bin, from its
	// validation to the removal of the rows: two requests must not each see
	// the other's files as the copies still to keep.
	recycling sync.Mutex
	blocked   struct { // PIN tries and Undo; guarded by its mutex
		sync.Mutex
		failures int
		retryAt  time.Time
		lastID   int64
		lastAt   time.Time
	}
	// verify is the Windows sign-in of ResetBlockPIN; nil means signin.Verify.
	// A test sets it to answer for the dialog it cannot show.
	verify func(ctx context.Context, owner uintptr, reason string) error

	organise organiser // the organise-on-disk run in progress (organise.go); the zero value works
	exports  struct {
		sync.Mutex
		batch  int64
		cancel context.CancelFunc
	}
}

// NewLibraryService returns the library service. gate is the Blocked
// screen's lock it shares with media serving; nil gives it one of its own.
// cache renders what is exported; nil leaves rendered exports unsupported.
func NewLibraryService(st *store.Store, ix *index.Indexer, gate *BlockGate, cache *media.Cache) *LibraryService {
	if gate == nil {
		gate = &BlockGate{}
	}
	s := &LibraryService{store: st, ix: ix, now: time.Now, gate: gate, media: cache}
	if ix != nil {
		// Files an interrupted organise run left half-moved are settled
		// before the first scan could find them so.
		ix.BeforeFirstScan(s.settleOrganise)
	}
	return s
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

// chosen is what a file dialog answered: the path the user picked, or "" if
// they closed it, which is no failure.
func chosen(path string, err error) (string, error) {
	if err != nil && err.Error() == dialogCancelled {
		return "", nil
	}
	return path, err
}

// publishFile writes a file the user asked to save and puts it at path in
// one rename, so a failed or cancelled save leaves an existing destination
// exactly as it was. pattern names the temporary file made beside it.
func publishFile(ctx context.Context, path, pattern string, write func(*os.File) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }() // only the temporary file this call created
	err = write(f)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// PickFolder opens the native folder picker. It returns "" if cancelled.
func (s *LibraryService) PickFolder() (string, error) {
	return chosen(application.Get().Dialog.OpenFile().
		SetTitle("Choose a folder with photos").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(false).
		PromptForSingleSelection())
}

// SourcePathKey is the key source folders are compared by in the library.
// Onboarding uses it too, so locale collation cannot hide distinct folders.
func (*LibraryService) SourcePathKey(path string) string {
	return core.Fold(filepath.Clean(path))
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
	d, err := s.store.AssetDetail(ctx, id, false)
	if errors.Is(err, store.ErrNotFound) && s.unlockedNow() {
		// Only a photo the open Blocked screen shows is asked of the gate, so
		// looking at ordinary photos is no use of it.
		d, err = s.store.AssetDetail(ctx, id, true)
	}
	d.Caps.Render = err == nil && d.Kind == core.KindPhoto && s.media != nil && s.media.Renderer != nil && media.CanRender(d.Width, d.Height)
	return d, err
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
	f, err := s.store.AssetFile(ctx, id, false)
	if errors.Is(err, store.ErrNotFound) && s.unlockedNow() {
		f, err = s.store.AssetFile(ctx, id, true)
	}
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

// Failures says why the files that couldn't be read failed, by reason.
func (s *LibraryService) Failures(ctx context.Context) ([]core.FailureGroup, error) {
	return s.store.Failures(ctx)
}

// SetPaused pauses or resumes background indexing.
func (s *LibraryService) SetPaused(paused bool) { s.ix.SetPaused(paused) }

// Settings returns the user's settings.
func (s *LibraryService) Settings(ctx context.Context) (core.Settings, error) {
	var set core.Settings
	for key, to := range map[string]*bool{
		core.SettingVideos:          &set.Videos,
		core.SettingAIOnBattery:     &set.AIOnBattery,
		core.SettingOCR:             &set.OCR,
		core.SettingXMP:             &set.XMP,
		core.SettingPauseFullScreen: &set.PauseFullScreen,
		core.SettingPauseBusy:       &set.PauseBusy,
		core.SettingIdleOnly:        &set.IdleOnly,
	} {
		on, err := s.store.Bool(ctx, key, core.DefaultOn(key))
		if err != nil {
			return core.Settings{}, err
		}
		*to = on
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
	threads, err := s.store.CPUThreads(ctx)
	if err != nil {
		return core.Settings{}, err
	}
	set.TagConfidence, set.EventGapHours, set.ThumbCacheMB = conf, gap, cacheMB
	set.CPUThreads, set.MaxCPUThreads, set.DefaultCPUThreads = threads, core.MaxCPUThreads(), core.DefaultCPUThreads()
	if set.ExcludedFormats, err = s.store.ExcludedFormats(ctx); err != nil {
		return core.Settings{}, err
	}
	return set, nil
}

// Preferences returns how the user likes the interface to look.
func (s *LibraryService) Preferences(ctx context.Context) (core.Preferences, error) {
	return s.store.Preferences(ctx)
}

// SetPreferences stores the interface preferences, held to what the UI can
// apply, and returns them as stored. They change nothing on disk and start
// no indexing.
func (s *LibraryService) SetPreferences(ctx context.Context, p core.Preferences) (core.Preferences, error) {
	return s.store.SetPreferences(ctx, p)
}

// MigratePreferences offers the library, once, the choices an older Lumen
// kept in the WebView's storage, and returns the preferences afterwards. A
// value the library already has is never replaced.
func (s *LibraryService) MigratePreferences(ctx context.Context, legacy core.LegacyPreferences) (core.Preferences, error) {
	return s.store.MigrateLegacyPreferences(ctx, legacy)
}

// SetCPUThreads sets how many CPU threads indexing may keep busy, held to
// what the machine has. It applies at once.
func (s *LibraryService) SetCPUThreads(ctx context.Context, n int) error {
	n, err := s.store.SetCPUThreads(ctx, n)
	if err != nil {
		return err
	}
	s.ix.SetCPUThreads(n)
	return nil
}

// SetEventGapHours sets the quiet period that splits detected events.
func (s *LibraryService) SetEventGapHours(ctx context.Context, hours int) error {
	hours = min(max(hours, core.EventGapMin), core.EventGapMax)
	return s.store.SetEventGapHours(ctx, hours)
}

// SetAIOnBattery lets the AI keep working on battery power (it waits for
// mains power by default).
func (s *LibraryService) SetAIOnBattery(ctx context.Context, on bool) error {
	return s.setHold(ctx, core.SettingAIOnBattery, on, s.ix.SetAIOnBattery)
}

// SetPauseFullScreen makes indexing wait while a game or full-screen app is
// in front (the default), or not.
func (s *LibraryService) SetPauseFullScreen(ctx context.Context, on bool) error {
	return s.setHold(ctx, core.SettingPauseFullScreen, on, s.ix.SetPauseFullScreen)
}

// SetPauseBusy makes indexing wait while other programs keep the PC busy
// (the default), or not.
func (s *LibraryService) SetPauseBusy(ctx context.Context, on bool) error {
	return s.setHold(ctx, core.SettingPauseBusy, on, s.ix.SetPauseBusy)
}

// SetIdleOnly makes indexing run only while the user is away from the PC,
// or whenever nothing else holds it (the default).
func (s *LibraryService) SetIdleOnly(ctx context.Context, on bool) error {
	return s.setHold(ctx, core.SettingIdleOnly, on, s.ix.SetIdleOnly)
}

// setHold stores a setting that holds indexing back, then applies it at once.
func (s *LibraryService) setHold(ctx context.Context, key string, on bool, apply func(bool)) error {
	if err := s.store.SetBool(ctx, key, on); err != nil {
		return err
	}
	apply(on)
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

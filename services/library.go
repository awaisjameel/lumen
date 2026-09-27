// Package services holds the thin Wails services the UI calls. Each method is
// turned into a typed TypeScript function in frontend/bindings. Services only
// translate between the UI and the internal packages; they hold no logic.
package services

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

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
	application.RegisterEvent[int](EventLibraryChanged) // payload: visible asset count
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

// LibraryChanged says the library changed, with how many assets are visible.
func (e *Emitter) LibraryChanged(visible int) {
	if e.App != nil {
		e.App.Event.Emit(EventLibraryChanged, visible)
	}
}

// LibraryService is everything about browsing and managing the library.
type LibraryService struct {
	store *store.Store
	ix    *index.Indexer
}

// NewLibraryService returns the library service.
func NewLibraryService(st *store.Store, ix *index.Indexer) *LibraryService {
	return &LibraryService{store: st, ix: ix}
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
				covered = covered || store.IsWithin(c, o)
			}
			if !covered {
				out = append(out, c)
			}
		}
	}
	return out
}

// PickFolder opens the native folder picker. It returns "" if cancelled.
func (s *LibraryService) PickFolder() (string, error) {
	return application.Get().Dialog.OpenFile().
		SetTitle("Choose a folder with photos").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(false).
		PromptForSingleSelection()
}

// AddSources adds folders and starts scanning them. Folders already inside
// the library are skipped silently.
func (s *LibraryService) AddSources(ctx context.Context, paths []string) ([]core.Source, error) {
	for _, p := range paths {
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			return nil, errors.New("folder not found: " + p)
		}
		if _, err := s.store.AddSource(ctx, p); err != nil && !errors.Is(err, store.ErrAlreadyCovered) {
			return nil, err
		}
	}
	s.ix.RescanAll()
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

// SetFavourite marks or unmarks a favourite.
func (s *LibraryService) SetFavourite(ctx context.Context, id int64, fav bool) error {
	return s.store.SetFavourite(ctx, id, fav)
}

// ShowInFolder opens Explorer with the asset's file selected.
func (s *LibraryService) ShowInFolder(ctx context.Context, id int64) error {
	f, err := s.store.AssetFile(ctx, id)
	if err != nil {
		return err
	}
	// Explorer outlives this call (and Lumen), so it gets no deadline.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), "explorer.exe", "/select,", f.Path)
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
	return core.Settings{Videos: videos, AIOnBattery: battery, TagConfidence: float64(conf)}, err
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
	return s.store.SetFloat(ctx, core.SettingTagConfidence, v)
}

// SetVideos turns video indexing on or off, then rescans so the change applies.
func (s *LibraryService) SetVideos(ctx context.Context, on bool) error {
	if err := s.store.SetBool(ctx, core.SettingVideos, on); err != nil {
		return err
	}
	s.ix.RescanAll()
	return nil
}

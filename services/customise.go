package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/media"
)

// A customisation file carries the interface preferences from one library to
// another: how Lumen looks, views and plays, the reassigned shortcuts and the
// export presets. It holds nothing else. Its type has no place for a folder,
// playback history, the PIN, a block or an engine setting, and reading one
// refuses a field the type does not have: importing a file can therefore not
// turn on a feature that writes files (XMP sidecars) or start a run.
const (
	customisationKind    = "lumen-customisation"
	customisationVersion = 1
	// customisationMaxBytes bounds a file to read: one Lumen wrote is a few
	// kilobytes.
	customisationMaxBytes = 256 << 10
)

type customisationFile struct {
	Kind        string           `json:"kind"`
	Version     int              `json:"version"`
	Preferences core.Preferences `json:"preferences"`
}

// The ways a customisation file is refused.
var (
	// ErrPresetInvalid means the file is not a customisation file Lumen wrote,
	// or holds something one never does.
	ErrPresetInvalid = errors.New("not a Lumen customisation file")
	// ErrPresetNewer means a later Lumen wrote the file in a form this one
	// does not know.
	ErrPresetNewer = errors.New("customisation file from a newer Lumen")
)

// SetFormatExcluded makes indexing leave a photo format out, or take it in
// again. Left out, its files are no longer added or read again; the photos
// the library already holds stay, with everything known about them. Taken in
// again, the folders are checked so that its files are found.
func (s *LibraryService) SetFormatExcluded(ctx context.Context, family core.Family, excluded bool) error {
	if !slices.Contains(core.ExcludableFamilies(), family) {
		return fmt.Errorf("%w: %q is not a format indexing can leave out", media.ErrUnsupported, family)
	}
	changed, err := s.store.SetFormatExcluded(ctx, family, excluded)
	if err != nil {
		return err
	}
	if changed && !excluded {
		s.ix.RescanAll()
	}
	return nil
}

// ResetPreferences puts the settings of the given categories back to their
// defaults. It never touches the library's folders, photos, tags, albums,
// people, the PIN or the blocks, and leaves alone the settings that decide
// what the library holds or writes: videos, reading text, and XMP sidecars.
func (s *LibraryService) ResetPreferences(ctx context.Context, categories []core.PrefCategory) error {
	for _, c := range categories {
		if !slices.Contains(core.PrefCategories(), c) {
			return fmt.Errorf("%w: no settings category %q", media.ErrUnsupported, c)
		}
	}
	if _, err := s.store.ResetPreferences(ctx, categories); err != nil {
		return err
	}
	if slices.Contains(categories, core.PrefIndexing) {
		if err := s.resetIndexing(ctx); err != nil {
			return err
		}
	}
	if slices.Contains(categories, core.PrefSearch) {
		if err := s.SetAIOnBattery(ctx, core.DefaultOn(core.SettingAIOnBattery)); err != nil {
			return err
		}
		if err := s.SetTagConfidence(ctx, core.TagConfidenceDefault); err != nil {
			return err
		}
	}
	if slices.Contains(categories, core.PrefFiles) {
		return s.SetEventGapHours(ctx, core.EventGapDefault)
	}
	return nil
}

// resetIndexing puts back what indexing waits for, how hard it works, how
// much the thumbnail cache keeps and which formats it takes in.
func (s *LibraryService) resetIndexing(ctx context.Context) error {
	for _, set := range []func() error{
		func() error { return s.SetPauseFullScreen(ctx, core.DefaultOn(core.SettingPauseFullScreen)) },
		func() error { return s.SetPauseBusy(ctx, core.DefaultOn(core.SettingPauseBusy)) },
		func() error { return s.SetIdleOnly(ctx, core.DefaultOn(core.SettingIdleOnly)) },
		func() error { return s.SetCPUThreads(ctx, core.DefaultCPUThreads()) },
		func() error { return s.SetThumbCacheMB(ctx, core.ThumbCacheDefaultMB) },
	} {
		if err := set(); err != nil {
			return err
		}
	}
	excluded, err := s.store.ExcludedFormats(ctx)
	if err != nil || len(excluded) == 0 {
		return err
	}
	if _, err := s.store.SetExcludedFormats(ctx, nil); err != nil {
		return err
	}
	s.ix.RescanAll() // the formats taken in again are found
	return nil
}

// ExportCustomisation asks where to save the interface preferences, then
// writes them there. It returns the file's name, "" if the user cancelled.
func (s *LibraryService) ExportCustomisation(ctx context.Context) (string, error) {
	path, err := application.Get().Dialog.SaveFile().
		SetFilename("Lumen customisation.json").
		AddFilter("Lumen customisation", "*.json").
		PromptForSingleSelection()
	if (err != nil && err.Error() == dialogCancelled) || (err == nil && path == "") {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := s.writeCustomisation(ctx, path); err != nil {
		return "", err
	}
	return filepath.Base(path), nil
}

// writeCustomisation publishes the file in one rename, so a failed save
// leaves an existing destination as it was.
func (s *LibraryService) writeCustomisation(ctx context.Context, path string) error {
	p, err := s.store.Preferences(ctx)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(customisationFile{Kind: customisationKind, Version: customisationVersion, Preferences: p}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".lumen-customisation-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }() // only the temporary file this call created
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write customisation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("save customisation: %w", err)
	}
	return nil
}

// ReadCustomisation asks for a customisation file and reads it, changing
// nothing: it returns the preferences the file holds, held to what the UI can
// apply, and which of them differ from the library's, for the user to review.
// They are applied with SetPreferences. The name is "" if the user cancelled.
func (s *LibraryService) ReadCustomisation(ctx context.Context) (core.CustomisationPreview, error) {
	path, err := application.Get().Dialog.OpenFile().
		SetTitle("Choose a Lumen customisation file").
		AddFilter("Lumen customisation", "*.json").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		PromptForSingleSelection()
	if (err != nil && err.Error() == dialogCancelled) || (err == nil && path == "") {
		return core.CustomisationPreview{Changed: []string{}}, nil
	}
	if err != nil {
		return core.CustomisationPreview{}, err
	}
	return s.readCustomisation(ctx, path)
}

func (s *LibraryService) readCustomisation(ctx context.Context, path string) (core.CustomisationPreview, error) {
	f, err := os.Open(path)
	if err != nil {
		return core.CustomisationPreview{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, customisationMaxBytes+1))
	if err != nil {
		return core.CustomisationPreview{}, err
	}
	if len(data) > customisationMaxBytes {
		return core.CustomisationPreview{}, fmt.Errorf("%w: the file is too large", ErrPresetInvalid)
	}
	// The kind and version are read first, leniently: a newer Lumen's file is
	// told apart from one that is not Lumen's at all.
	var head struct {
		Kind        string          `json:"kind"`
		Version     int             `json:"version"`
		Preferences json.RawMessage `json:"preferences"`
	}
	if err := json.Unmarshal(data, &head); err != nil || head.Kind != customisationKind || head.Version < 1 {
		return core.CustomisationPreview{}, ErrPresetInvalid
	}
	if head.Version > customisationVersion {
		return core.CustomisationPreview{}, ErrPresetNewer
	}
	if !bytes.HasPrefix(bytes.TrimSpace(head.Preferences), []byte("{")) {
		return core.CustomisationPreview{}, ErrPresetInvalid
	}
	var file customisationFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // only what the type lists is ever read
	if err := dec.Decode(&file); err != nil {
		return core.CustomisationPreview{}, errors.Join(ErrPresetInvalid, err)
	}
	current, err := s.store.Preferences(ctx)
	if err != nil {
		return core.CustomisationPreview{}, err
	}
	// Preferences the file leaves out keep the library's: an older file
	// without a later preference must not reset it. A list the file holds
	// replaces the library's whole.
	var present struct {
		Preferences map[string]json.RawMessage `json:"preferences"`
	}
	if err := json.Unmarshal(data, &present); err != nil {
		return core.CustomisationPreview{}, errors.Join(ErrPresetInvalid, err)
	}
	merged := current
	viewer := *current.Viewer
	merged.Viewer = &viewer
	merged.Shortcuts, merged.ExportPresets = maps.Clone(current.Shortcuts), slices.Clone(current.ExportPresets)
	for name := range present.Preferences {
		switch {
		case strings.EqualFold(name, "shortcuts"):
			merged.Shortcuts = nil
		case strings.EqualFold(name, "exportPresets"):
			merged.ExportPresets = nil
		}
	}
	overlay := customisationFile{Preferences: merged}
	if err := json.Unmarshal(data, &overlay); err != nil {
		return core.CustomisationPreview{}, errors.Join(ErrPresetInvalid, err)
	}
	next := overlay.Preferences.Normalised()
	changed := core.PreferenceChanges(current, next)
	if changed == nil {
		changed = []string{}
	}
	return core.CustomisationPreview{Name: filepath.Base(path), Preferences: next, Changed: changed}, nil
}

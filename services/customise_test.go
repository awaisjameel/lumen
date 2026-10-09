package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
	"lumen/internal/index"
	"lumen/internal/media"
)

// newCustomLibrary is a library service over a library of one photo, with an
// indexer that is never started: settings reach it, nothing is scanned.
func newCustomLibrary(t *testing.T) *LibraryService {
	t.Helper()
	l := newShownLibrary(t, 1)
	paths := core.PathsIn(t.TempDir())
	check(t, paths.Ensure())
	return NewLibraryService(l.st, index.New(l.st, paths, l.cache, index.Models{}, quietEvents{}), nil, nil)
}

// customised is preferences away from their defaults in every category.
func customised() core.Preferences {
	p := core.DefaultPreferences()
	p.Theme, p.Accent, p.Motion = core.ThemeDark, core.AccentRose, core.MotionReduced
	p.StartView, p.ThumbFit, p.HideVideoBadge = core.StartPeople, core.ThumbContain, true
	p.FocusRing = core.FocusStrong
	p.Shortcuts = map[string]string{"viewer.zoomIn": "Z"}
	p.ExportPresets = []core.ExportPreset{{Name: "Web", Format: core.ExportJPEG, Side: 2048, Quality: 80}}
	viewer := *p.Viewer
	viewer.Speed, viewer.Open, viewer.Subtitles = 1.5, core.ViewerOpenActual, true
	p.Viewer = &viewer
	return p.Normalised()
}

func TestACustomisationFileCarriesThePreferencesToAnotherLibrary(t *testing.T) {
	ctx := t.Context()
	from, to := newCustomLibrary(t), newCustomLibrary(t)
	want, err := from.SetPreferences(ctx, customised())
	check(t, err)
	path := filepath.Join(t.TempDir(), "Lumen customisation.json")
	check(t, from.writeCustomisation(ctx, path))

	preview, err := to.readCustomisation(ctx, path)
	check(t, err)
	if preview.Name != "Lumen customisation.json" {
		t.Errorf("preview name = %q", preview.Name)
	}
	if !reflect.DeepEqual(preview.Preferences, want) {
		t.Errorf("the file holds %+v, want %+v", preview.Preferences, want)
	}
	for _, name := range []string{"theme", "startView", "shortcuts", "exportPresets", "viewer.speed", "viewer.open"} {
		if !slices.Contains(preview.Changed, name) {
			t.Errorf("the preview does not list %q as changed: %v", name, preview.Changed)
		}
	}
	// Reading is only a preview: nothing is applied until the user agrees.
	if kept, err := to.Preferences(ctx); err != nil || !reflect.DeepEqual(kept, core.DefaultPreferences()) {
		t.Errorf("reading a file changed the preferences: %+v, %v", kept, err)
	}
	applied, err := to.SetPreferences(ctx, preview.Preferences)
	if err != nil || !reflect.DeepEqual(applied, want) {
		t.Errorf("applied %+v, %v, want %+v", applied, err, want)
	}
	again, err := to.readCustomisation(ctx, path)
	if err != nil || len(again.Changed) != 0 {
		t.Errorf("after applying, the file still differs in %v (%v)", again.Changed, err)
	}
}

func TestACustomisationFileNamesNoFolderAndNoEngineSetting(t *testing.T) {
	ctx := t.Context()
	s := newCustomLibrary(t)
	_, err := s.SetPreferences(ctx, customised())
	check(t, err)
	check(t, s.store.SetBool(ctx, core.SettingXMP, true))
	path := filepath.Join(t.TempDir(), "out.json")
	check(t, s.writeCustomisation(ctx, path))
	data, err := os.ReadFile(path)
	check(t, err)
	sources, err := s.Sources(ctx)
	check(t, err)
	text := string(data)
	escaped, err := json.Marshal(sources[0].Path)
	check(t, err)
	if strings.Contains(text, sources[0].Path) || strings.Contains(text, strings.Trim(string(escaped), `"`)) {
		t.Errorf("the file names a library folder:\n%s", text)
	}
	var top map[string]json.RawMessage
	check(t, json.Unmarshal(data, &top))
	if len(top) != 3 || top["kind"] == nil || top["version"] == nil || top["preferences"] == nil {
		t.Errorf("the file holds more than its kind, version and preferences: %v", top)
	}
	for _, word := range []string{"xmp", "pin", "block", "source", "history", "position", "ocr"} {
		if strings.Contains(strings.ToLower(text), `"`+word) {
			t.Errorf("the file has a field about %q:\n%s", word, text)
		}
	}
}

func TestACustomisationFileCannotTurnOnWhatWritesFiles(t *testing.T) {
	ctx := t.Context()
	s := newCustomLibrary(t)
	for name, content := range map[string]string{
		"a setting beside the preferences": `{"kind":"lumen-customisation","version":1,"preferences":{},"settings":{"xmp":true}}`,
		"an engine setting among them":     `{"kind":"lumen-customisation","version":1,"preferences":{"xmp":true}}`,
		"a run to start":                   `{"kind":"lumen-customisation","version":1,"preferences":{"organise":{"template":"{Year}"}}}`,
		"a viewer field Lumen never wrote": `{"kind":"lumen-customisation","version":1,"preferences":{"viewer":{"path":"C:\\Users"}}}`,
		"another program's file":           `{"name":"package","version":1}`,
		"no version":                       `{"kind":"lumen-customisation","preferences":{}}`,
		"no preferences":                   `{"kind":"lumen-customisation","version":1}`,
		"null preferences":                 `{"kind":"lumen-customisation","version":1,"preferences":null}`,
		"not JSON":                         `theme = dark`,
	} {
		path := filepath.Join(t.TempDir(), "in.json")
		check(t, os.WriteFile(path, []byte(content), 0o600))
		if _, err := s.readCustomisation(ctx, path); !errors.Is(err, ErrPresetInvalid) {
			t.Errorf("%s: read = %v, want it refused as invalid", name, err)
		}
	}
	if on, err := s.store.Bool(ctx, core.SettingXMP, false); err != nil || on {
		t.Errorf("XMP sidecars = %v, %v, want them still off", on, err)
	}
	if kept, err := s.Preferences(ctx); err != nil || !reflect.DeepEqual(kept, core.DefaultPreferences()) {
		t.Errorf("a refused file changed the preferences: %+v, %v", kept, err)
	}
}

func TestACustomisationFileFromANewerLumenIsRefusedAsNewer(t *testing.T) {
	s := newCustomLibrary(t)
	path := filepath.Join(t.TempDir(), "in.json")
	check(t, os.WriteFile(path, []byte(`{"kind":"lumen-customisation","version":2,"preferences":{"holograms":true}}`), 0o600))
	if _, err := s.readCustomisation(t.Context(), path); !errors.Is(err, ErrPresetNewer) {
		t.Fatalf("read = %v, want it refused as newer", err)
	}
}

func TestAnOversizedCustomisationFileIsRefused(t *testing.T) {
	s := newCustomLibrary(t)
	path := filepath.Join(t.TempDir(), "in.json")
	check(t, os.WriteFile(path, []byte(`{"kind":"lumen-customisation","version":1,"preferences":{"theme":"`+
		strings.Repeat("x", customisationMaxBytes)+`"}}`), 0o600))
	if _, err := s.readCustomisation(t.Context(), path); !errors.Is(err, ErrPresetInvalid) {
		t.Fatalf("read = %v, want it refused", err)
	}
}

func TestACustomisationFileIsHeldToWhatTheUICanApplyAndKeepsWhatItLeavesOut(t *testing.T) {
	ctx := t.Context()
	s := newCustomLibrary(t)
	mine := core.DefaultPreferences()
	mine.Accent = core.AccentTeal
	mine.Shortcuts = map[string]string{"viewer.zoomIn": "Z"}
	viewer := *mine.Viewer
	viewer.SkipSeconds = 30
	mine.Viewer = &viewer
	mine, err := s.SetPreferences(ctx, mine)
	check(t, err)

	path := filepath.Join(t.TempDir(), "in.json")
	check(t, os.WriteFile(path, []byte(`{"kind":"lumen-customisation","version":1,"preferences":{
		"theme":"sepia","textScale":900,"viewer":{"speed":40},
		"shortcuts":{"viewer.close":"Escape","media.open":"O","<b>":"X"}}}`), 0o600))
	preview, err := s.readCustomisation(ctx, path)
	check(t, err)
	got := preview.Preferences
	if got.Theme != core.ThemeSystem || got.TextScale != core.TextScaleMax || got.Viewer.Speed != core.PlaybackSpeedMax {
		t.Errorf("values out of range were not held: %+v, viewer %+v", got, *got.Viewer)
	}
	if want := map[string]string{"media.open": "O"}; !reflect.DeepEqual(got.Shortcuts, want) {
		t.Errorf("shortcuts = %v, want the file's valid ones only: %v", got.Shortcuts, want)
	}
	if got.Accent != core.AccentTeal || got.Viewer.SkipSeconds != 30 {
		t.Errorf("a preference the file leaves out was reset: %+v, viewer %+v", got, *got.Viewer)
	}
	if want := []string{"viewer.speed", "textScale", "shortcuts"}; !slices.Equal(preview.Changed, want) {
		t.Errorf("changed = %v, want %v", preview.Changed, want)
	}
	// The preview shares nothing with the library's own preferences.
	if now, err := s.Preferences(ctx); err != nil || !reflect.DeepEqual(now, mine) {
		t.Errorf("the library's preferences = %+v, %v, want them untouched: %+v", now, err, mine)
	}
}

func TestAFailedCustomisationSaveLeavesTheDestinationAsItWas(t *testing.T) {
	s := newCustomLibrary(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "taken")
	check(t, os.Mkdir(path, 0o700)) // a folder is where the file should go
	if err := s.writeCustomisation(t.Context(), path); err == nil {
		t.Fatal("saving over a folder succeeded")
	}
	entries, err := os.ReadDir(dir)
	check(t, err)
	if len(entries) != 1 {
		t.Errorf("the failed save left files behind: %v", entries)
	}
}

func TestCustomisationReplacesShortcutListsRegardlessOfFieldCase(t *testing.T) {
	s := newCustomLibrary(t)
	_, err := s.SetPreferences(t.Context(), customised())
	check(t, err)
	path := filepath.Join(t.TempDir(), "in.json")
	check(t, os.WriteFile(path, []byte(`{"kind":"lumen-customisation","version":1,
		"preferences":{"Shortcuts":{"viewer.flip":"P"}}}`), 0o600))
	preview, err := s.readCustomisation(t.Context(), path)
	check(t, err)
	want := map[string]string{"viewer.flip": "P"}
	if !reflect.DeepEqual(preview.Preferences.Shortcuts, want) {
		t.Errorf("shortcuts = %v, want the file's whole list %v", preview.Preferences.Shortcuts, want)
	}
}

func TestResetPreferencesRestoresEngineSettingsButNotWhatTheLibraryHolds(t *testing.T) {
	ctx := t.Context()
	s := newCustomLibrary(t)
	_, err := s.SetPreferences(ctx, customised())
	check(t, err)
	check(t, s.SetPauseBusy(ctx, false))
	check(t, s.SetIdleOnly(ctx, true))
	check(t, s.SetThumbCacheMB(ctx, 512))
	check(t, s.SetEventGapHours(ctx, 48))
	check(t, s.SetAIOnBattery(ctx, true))
	check(t, s.SetTagConfidence(ctx, 0.2))
	check(t, s.SetFormatExcluded(ctx, core.FamilyRAW, true))
	check(t, s.store.SetBool(ctx, core.SettingVideos, true))
	check(t, s.store.SetBool(ctx, core.SettingXMP, true))
	check(t, s.store.SetBool(ctx, core.SettingOCR, true))
	check(t, s.CreateBlockPIN(ctx, "123456"))
	before, err := s.Sources(ctx)
	check(t, err)

	check(t, s.ResetPreferences(ctx, core.PrefCategories()))

	if got, err := s.Preferences(ctx); err != nil || !reflect.DeepEqual(got, core.DefaultPreferences()) {
		t.Errorf("preferences after a full reset = %+v, %v, want the defaults", got, err)
	}
	set, err := s.Settings(ctx)
	check(t, err)
	if !set.PauseBusy || set.IdleOnly || set.ThumbCacheMB != core.ThumbCacheDefaultMB ||
		set.EventGapHours != core.EventGapDefault || len(set.ExcludedFormats) != 0 ||
		set.CPUThreads != core.DefaultCPUThreads() || set.AIOnBattery ||
		set.TagConfidence != core.TagConfidenceDefault {
		t.Errorf("engine settings after a full reset = %+v", set)
	}
	if !set.Videos || !set.XMP || !set.OCR {
		t.Errorf("a reset changed what the library holds or writes: %+v", set)
	}
	if ok, err := s.BlockPINConfigured(ctx); err != nil || !ok {
		t.Errorf("the PIN after a reset = %v, %v, want it kept", ok, err)
	}
	if after, err := s.Sources(ctx); err != nil || !reflect.DeepEqual(after, before) {
		t.Errorf("folders after a reset = %v, %v, want %v", after, err, before)
	}
}

func TestResetPreferencesOfOneCategoryLeavesEngineSettings(t *testing.T) {
	ctx := t.Context()
	s := newCustomLibrary(t)
	_, err := s.SetPreferences(ctx, customised())
	check(t, err)
	check(t, s.SetPauseBusy(ctx, false))
	check(t, s.ResetPreferences(ctx, []core.PrefCategory{core.PrefAppearance}))
	got, err := s.Preferences(ctx)
	check(t, err)
	if got.Theme != core.ThemeSystem || got.StartView != core.StartPeople {
		t.Errorf("preferences = %+v, want only the appearance reset", got)
	}
	if set, err := s.Settings(ctx); err != nil || set.PauseBusy {
		t.Errorf("settings = %+v, %v, want indexing settings untouched", set, err)
	}
	if err := s.ResetPreferences(ctx, []core.PrefCategory{"everything"}); !errors.Is(err, media.ErrUnsupported) {
		t.Errorf("resetting an unknown category = %v, want it refused", err)
	}
}

func TestOnlyAPhotoFormatCanBeExcluded(t *testing.T) {
	ctx := t.Context()
	s := newCustomLibrary(t)
	check(t, s.SetFormatExcluded(ctx, core.FamilyRAW, true))
	check(t, s.SetFormatExcluded(ctx, core.FamilyHEIF, true))
	check(t, s.SetFormatExcluded(ctx, core.FamilyRAW, true)) // again: nothing changes
	set, err := s.Settings(ctx)
	check(t, err)
	if want := []core.Family{core.FamilyHEIF, core.FamilyRAW}; !slices.Equal(set.ExcludedFormats, want) {
		t.Errorf("excluded formats = %v, want %v", set.ExcludedFormats, want)
	}
	check(t, s.SetFormatExcluded(ctx, core.FamilyRAW, false))
	if set, err = s.Settings(ctx); err != nil || !slices.Equal(set.ExcludedFormats, []core.Family{core.FamilyHEIF}) {
		t.Errorf("excluded formats = %v, %v, want HEIF only", set.ExcludedFormats, err)
	}
	if err := s.SetFormatExcluded(ctx, core.FamilyMP4, true); !errors.Is(err, media.ErrUnsupported) {
		t.Errorf("excluding a video format = %v, want it refused", err)
	}
	// The photo the library holds is still there.
	if d, err := s.Asset(ctx, 1); err != nil || d.ID != 1 {
		t.Errorf("the library's photo = %+v, %v", d, err)
	}
}

func TestClearingTheViewingCacheRemovesOnlyViewingCopies(t *testing.T) {
	ctx := t.Context()
	l := newShownLibrary(t, 1)
	dir := filepath.Join(l.cache.Dir, "render")
	check(t, os.MkdirAll(dir, 0o750))
	check(t, os.WriteFile(filepath.Join(dir, "1-abc.jpg"), make([]byte, 1000), 0o600))
	check(t, os.WriteFile(filepath.Join(dir, "2-def.jpg"), make([]byte, 500), 0o600))
	thumb := l.cache.Path(1, core.ThumbSmall)

	if n, err := l.m.ViewingCacheBytes(); err != nil || n != 1500 {
		t.Fatalf("viewing cache = %d bytes, %v, want 1500", n, err)
	}
	freed, err := l.m.ClearViewingCache(ctx)
	if err != nil || freed != 1500 {
		t.Fatalf("cleared %d bytes, %v, want 1500", freed, err)
	}
	if n, err := l.m.ViewingCacheBytes(); err != nil || n != 0 {
		t.Errorf("viewing cache after clearing = %d bytes, %v", n, err)
	}
	if _, err := os.Stat(thumb); err != nil {
		t.Errorf("clearing the viewing cache removed a thumbnail: %v", err)
	}
	if d, err := l.st.AssetDetail(ctx, 1, false); err != nil || d.ID != 1 {
		t.Errorf("clearing the viewing cache changed the library: %+v, %v", d, err)
	}
	// With nothing to clear it is no failure.
	if freed, err := l.m.ClearViewingCache(ctx); err != nil || freed != 0 {
		t.Errorf("clearing an empty cache = %d, %v", freed, err)
	}
}

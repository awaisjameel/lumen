package core

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestNormalisedPreferencesReplaceNewChoicesTheUICannotApply(t *testing.T) {
	viewer := ViewerPreferences{Open: "stretched", Wheel: "spin", RenderSide: 3000, SubtitleLanguage: " EN-gb "}
	got := Preferences{Viewer: &viewer, Motion: "bouncy", ViewerBackground: "white", StartView: "trash",
		ThumbFit: "stretch", FocusRing: "none"}.Normalised()
	if got.Motion != MotionSystem || got.ViewerBackground != ViewerBackgroundDark || got.StartView != StartLibrary ||
		got.ThumbFit != ThumbCover || got.FocusRing != FocusStandard {
		t.Errorf("Normalised() kept a choice that is not offered: %+v", got)
	}
	if v := got.Viewer; v.Open != ViewerOpenFit || v.Wheel != ViewerWheelZoom || v.RenderSide != RenderSideHigh ||
		v.SubtitleLanguage != "en-gb" {
		t.Errorf("the viewer's choices = %+v", v)
	}
	if lang := (ViewerPreferences{SubtitleLanguage: `C:\Users`}).Normalised().SubtitleLanguage; lang != "" {
		t.Errorf("a subtitle language that is no language tag was kept: %q", lang)
	}
}

func TestOnlyAssignableShortcutsAreValid(t *testing.T) {
	for keys, want := range map[string]bool{
		"":             true, // no shortcut
		"F":            true,
		"Ctrl+K":       true,
		"Ctrl+Shift+P": true,
		"Shift+Delete": true,
		"ArrowLeft":    true,
		"?":            true,
		"F2":           true,
		"Escape":       false, // always leaves the viewer or a dialog
		"Ctrl+Escape":  false,
		"Tab":          false,
		"Alt+F4":       false,
		"Alt+f4":       false,
		"+":            false, // joins the parts
		"Shift+Ctrl+K": false, // modifiers out of order
		"Ctrl+":        false,
		"Ctrl+K L":     false,
		"Hyper+K":      false,
	} {
		if got := ValidShortcut(keys); got != want {
			t.Errorf("ValidShortcut(%q) = %v, want %v", keys, got, want)
		}
	}
}

func TestNormalisedShortcutsKeepOnlyCommandsAndValidKeys(t *testing.T) {
	got := Preferences{Shortcuts: map[string]string{
		"viewer.zoomIn":  "Ctrl+Shift+Z",
		"media.open":     "", // deliberately without a shortcut
		"viewer.close":   "Escape",
		"<script>":       "X",
		"library.search": "Ctrl+K L",
	}}.Normalised().Shortcuts
	want := map[string]string{"viewer.zoomIn": "Ctrl+Shift+Z", "media.open": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shortcuts = %v, want %v", got, want)
	}
	if none := (Preferences{Shortcuts: map[string]string{}}).Normalised().Shortcuts; none != nil {
		t.Errorf("no shortcuts normalise to %v, want nil", none)
	}

	many := map[string]string{}
	for i := range MaxShortcuts + 20 {
		many[fmt.Sprintf("group.command%s", strings.Repeat("a", i))] = "F2"
	}
	if n := len(Preferences{Shortcuts: many}.Normalised().Shortcuts); n > MaxShortcuts {
		t.Errorf("%d shortcuts were kept, more than the %d allowed", n, MaxShortcuts)
	}
}

func TestCustomisationCannotRemoveTheViewersEscapeShortcut(t *testing.T) {
	for _, keys := range []string{"", "X", "Ctrl+W"} {
		p := Preferences{Shortcuts: map[string]string{"viewer.close": keys, "viewer.rotate": "R"}}.Normalised()
		if _, ok := p.Shortcuts["viewer.close"]; ok {
			t.Errorf("viewer.close kept override %q", keys)
		}
		if p.Shortcuts["viewer.rotate"] != "R" {
			t.Error("an assignable command lost its shortcut")
		}
	}
}

func TestNormalisedExportPresetsAreNamedBoundedAndUnique(t *testing.T) {
	got := Preferences{ExportPresets: []ExportPreset{
		{Name: "  Web  ", Format: ExportJPEG, Side: 99999, Quality: 0},
		{Name: "web", Format: ExportPNG, Side: 1024, Quality: 90}, // the same name, by fold
		{Name: "", Format: ExportJPEG},
		{Name: "Tape", Format: "betamax"},
		{Name: "Archive", Format: ExportOriginal, Side: 1, Quality: 500, Adjust: true},
	}}.Normalised().ExportPresets
	want := []ExportPreset{
		{Name: "Web", Format: ExportJPEG, Side: ExportSideMax, Quality: ExportQualityMin},
		{Name: "Archive", Format: ExportOriginal, Side: ExportSideMin, Quality: ExportQualityMax, Adjust: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("presets = %+v, want %+v", got, want)
	}

	var many []ExportPreset
	for i := range MaxExportPresets + 5 {
		many = append(many, ExportPreset{Name: fmt.Sprint("Preset ", i), Format: ExportJPEG, Side: 2048, Quality: 80})
	}
	if n := len(Preferences{ExportPresets: many}.Normalised().ExportPresets); n != MaxExportPresets {
		t.Errorf("%d presets were kept, want %d", n, MaxExportPresets)
	}
}

// changed is every preference set away from its default, so a reset that
// misses one shows.
func changed() Preferences {
	return Preferences{
		Viewer: &ViewerPreferences{Filmstrip: true, AutoHide: true, SlideshowSeconds: 9, Loop: true, Autoplay: false,
			Volume: 0.5, Muted: true, Speed: 1.5, SkipSeconds: 30, Resume: true, Open: ViewerOpenActual,
			Wheel: ViewerWheelNavigate, StillAnimations: true, RenderSide: RenderSideStandard, VideoLoop: true,
			Subtitles: true, SubtitleLanguage: "en"},
		Theme: ThemeDark, Accent: AccentRose, Density: DensityCompact, TextScale: 120, TileSize: 240,
		SidebarCollapsed: true, SidebarWidth: 300, InspectorWidth: 400, Motion: MotionReduced,
		ViewerBackground: ViewerBackgroundBlack, StartView: StartAlbums, ThumbFit: ThumbContain,
		FocusRing: FocusStrong, HideFavouriteBadge: true, HideVideoBadge: true, HideDurationBadge: true,
		Shortcuts:     map[string]string{"viewer.zoomIn": "Z"},
		ExportPresets: []ExportPreset{{Name: "Web", Format: ExportJPEG, Side: 2048, Quality: 80}},
	}.Normalised()
}

func TestEveryPreferenceBelongsToExactlyOneResetCategory(t *testing.T) {
	all, def := changed(), DefaultPreferences()
	everyChange := PreferenceChanges(def, all)
	// The fixture really does change every preference there is.
	fields := reflect.TypeFor[Preferences]().NumField() - 1 + reflect.TypeFor[ViewerPreferences]().NumField()
	if len(everyChange) != fields {
		t.Fatalf("the fixture changes %d of %d preferences: %v", len(everyChange), fields, everyChange)
	}
	var reset []string
	for _, c := range PrefCategories() {
		restored := PreferenceChanges(all, all.Reset(c))
		for _, name := range restored {
			if slices.Contains(reset, name) {
				t.Errorf("%q is reset by more than one category (again by %q)", name, c)
			}
		}
		reset = append(reset, restored...)
		if left := PreferenceChanges(def, def.Reset(c)); len(left) != 0 {
			t.Errorf("resetting %q changes the defaults: %v", c, left)
		}
	}
	slices.Sort(reset)
	slices.Sort(everyChange)
	if !slices.Equal(reset, everyChange) {
		t.Errorf("the categories reset %v, the preferences are %v", reset, everyChange)
	}
}

func TestResettingOneCategoryLeavesTheOthers(t *testing.T) {
	all := changed()
	got := all.Reset(PrefAppearance)
	if got.Theme != ThemeSystem || got.Motion != MotionSystem || got.ViewerBackground != ViewerBackgroundDark {
		t.Errorf("appearance was not reset: %+v", got)
	}
	if got.StartView != StartAlbums || got.Viewer.Speed != 1.5 || got.Shortcuts["viewer.zoomIn"] != "Z" ||
		len(got.ExportPresets) != 1 {
		t.Errorf("resetting appearance changed another category: %+v", got)
	}
	if all.Theme != ThemeDark || all.Viewer.Speed != 1.5 {
		t.Errorf("Reset changed the preferences it was called on: %+v", all)
	}
	if kept := all.Reset(PrefIndexing).Reset(PrefSearch); len(PreferenceChanges(all, kept)) != 0 {
		t.Errorf("an engine category changed the interface preferences: %v", PreferenceChanges(all, kept))
	}
}

func TestPreferenceChangesNameWhatDiffers(t *testing.T) {
	a := DefaultPreferences()
	b := DefaultPreferences()
	b.Theme = ThemeDark
	viewer := *b.Viewer
	viewer.Speed = 2
	b.Viewer = &viewer
	b.Shortcuts = map[string]string{"viewer.zoomIn": "Z"}
	if got, want := PreferenceChanges(a, b), []string{"viewer.speed", "theme", "shortcuts"}; !slices.Equal(got, want) {
		t.Errorf("PreferenceChanges = %v, want %v", got, want)
	}
	if got := PreferenceChanges(a, a); len(got) != 0 {
		t.Errorf("equal preferences differ in %v", got)
	}
}

func TestOnlyPhotoFormatsCanBeExcluded(t *testing.T) {
	for _, f := range ExcludableFamilies() {
		found := false
		for _, format := range Formats() {
			if format.Family == f {
				found = true
				if format.Kind != KindPhoto {
					t.Errorf("%q can be excluded but %s is a %s", f, format.Ext, format.Kind)
				}
			}
		}
		if !found {
			t.Errorf("%q can be excluded but no format has it", f)
		}
	}
	got := NormalisedFamilies([]Family{FamilyRAW, FamilyMP4, "betamax", FamilyRAW, FamilyJPEG})
	if want := []Family{FamilyJPEG, FamilyRAW}; !slices.Equal(got, want) {
		t.Errorf("NormalisedFamilies = %v, want %v", got, want)
	}
	if none := NormalisedFamilies(nil); none != nil {
		t.Errorf("no families normalise to %v, want nil", none)
	}
}

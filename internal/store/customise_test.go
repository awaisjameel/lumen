package store

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"lumen/internal/core"
)

func TestEveryPreferenceSurvivesReopening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	want := core.Preferences{
		Viewer: &core.ViewerPreferences{Filmstrip: true, SlideshowSeconds: 9, Volume: 0.5, Speed: 1.5, SkipSeconds: 30,
			Open: core.ViewerOpenActual, Wheel: core.ViewerWheelNavigate, StillAnimations: true,
			RenderSide: core.RenderSideStandard, VideoLoop: true, Subtitles: true, SubtitleLanguage: "en"},
		Theme: core.ThemeDark, Accent: core.AccentRose, Density: core.DensityCompact, TextScale: 120, TileSize: 240,
		SidebarWidth: 300, InspectorWidth: 400, Motion: core.MotionReduced,
		ViewerBackground: core.ViewerBackgroundBlack, StartView: core.StartAlbums, ThumbFit: core.ThumbContain,
		FocusRing: core.FocusStrong, HideFavouriteBadge: true, HideDurationBadge: true,
		Shortcuts:     map[string]string{"viewer.zoomIn": "Z", "media.open": ""},
		ExportPresets: []core.ExportPreset{{Name: "Web", Format: core.ExportJPEG, Side: 2048, Quality: 80, Adjust: true}},
	}.Normalised()
	if stored, err := st.SetPreferences(ctx, want); err != nil || !reflect.DeepEqual(stored, want) {
		t.Fatalf("SetPreferences stored %+v, %v, want %+v", stored, err, want)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	got, err := st.Preferences(ctx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("after reopening Preferences() = %+v, %v, want %+v", got, err, want)
	}
	if !reflect.DeepEqual(*got.Viewer, *want.Viewer) {
		t.Errorf("after reopening the viewer holds %+v, want %+v", *got.Viewer, *want.Viewer)
	}
}

func TestResetPreferencesRestoresOnlyTheNamedCategories(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	p := core.DefaultPreferences()
	p.Theme, p.StartView, p.FocusRing = core.ThemeDark, core.StartPeople, core.FocusStrong
	p.Shortcuts = map[string]string{"viewer.zoomIn": "Z"}
	viewer := *p.Viewer
	viewer.Speed, viewer.SlideshowSeconds = 2, 20
	p.Viewer = &viewer
	if _, err := st.SetPreferences(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := st.ResetPreferences(ctx, []core.PrefCategory{core.PrefAppearance, core.PrefVideo})
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != core.ThemeSystem || got.Viewer.Speed != 1 {
		t.Errorf("the named categories were not reset: %+v, viewer %+v", got, *got.Viewer)
	}
	if got.StartView != core.StartPeople || got.FocusRing != core.FocusStrong || got.Viewer.SlideshowSeconds != 20 ||
		got.Shortcuts["viewer.zoomIn"] != "Z" {
		t.Errorf("another category was reset too: %+v, viewer %+v", got, *got.Viewer)
	}
	if stored, err := st.Preferences(ctx); err != nil || !reflect.DeepEqual(stored, got) {
		t.Errorf("Preferences() = %+v, %v, want what the reset returned %+v", stored, err, got)
	}
}

func TestAVideoLoopNeverChosenFollowsTheOlderSharedLoop(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	// An older Lumen saved one "loop", which slideshows and videos both read.
	if err := st.setSetting(ctx, core.SettingUIViewer, `{"loop":true,"slideshowSeconds":5,"volume":1,"speed":1,"skipSeconds":10}`); err != nil {
		t.Fatal(err)
	}
	got, err := st.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Viewer.Loop || !got.Viewer.VideoLoop {
		t.Fatalf("viewer = %+v, want videos to loop as they did", *got.Viewer)
	}
	// Once the user chooses, the choice stands.
	viewer := *got.Viewer
	viewer.VideoLoop = false
	got.Viewer = &viewer
	if _, err := st.SetPreferences(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, err := st.Preferences(ctx); err != nil || again.Viewer.VideoLoop || !again.Viewer.Loop {
		t.Errorf("after choosing, viewer = %+v, %v; want the slideshow looping and videos not", *again.Viewer, err)
	}
}

func TestAnUnreadableShortcutListReadsAsNone(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	if err := st.setSetting(ctx, core.SettingUIShortcuts, `["not","a","map"]`); err != nil {
		t.Fatal(err)
	}
	if err := st.setSetting(ctx, core.SettingUIExportPresets, `{"not":"a list"}`); err != nil {
		t.Fatal(err)
	}
	got, err := st.Preferences(ctx)
	if err != nil || got.Shortcuts != nil || got.ExportPresets != nil {
		t.Errorf("Preferences() = %+v, %v, want no shortcuts and no presets", got, err)
	}
}

func TestExcludedFormatsKeepOnlyWhatCanBeExcluded(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	if got, err := st.ExcludedFormats(ctx); err != nil || got != nil {
		t.Fatalf("ExcludedFormats() = %v, %v, want none to start with", got, err)
	}
	// Whatever order they are chosen in, and whatever else is asked for, the
	// library holds the formats that can be left out, in the order offered.
	for _, family := range []core.Family{core.FamilyRAW, core.FamilyMP4, "betamax", core.FamilyHEIF} {
		changed, err := st.SetFormatExcluded(ctx, family, true)
		if want := family == core.FamilyRAW || family == core.FamilyHEIF; err != nil || changed != want {
			t.Fatalf("SetFormatExcluded(%q) = %v, %v, want changed %v", family, changed, err, want)
		}
	}
	want := []core.Family{core.FamilyHEIF, core.FamilyRAW}
	if got, err := st.ExcludedFormats(ctx); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ExcludedFormats() = %v, %v, want %v", got, err, want)
	}
	if changed, err := st.SetFormatExcluded(ctx, core.FamilyRAW, true); err != nil || changed {
		t.Errorf("excluding RAW again = %v, %v, want no change", changed, err)
	}
	if changed, err := st.ClearExcludedFormats(ctx); err != nil || !changed {
		t.Fatalf("ClearExcludedFormats() = %v, %v, want a change", changed, err)
	}
	if got, err := st.ExcludedFormats(ctx); err != nil || got != nil {
		t.Errorf("after clearing ExcludedFormats() = %v, %v, want none", got, err)
	}
	if changed, err := st.ClearExcludedFormats(ctx); err != nil || changed {
		t.Errorf("clearing none = %v, %v, want no change", changed, err)
	}
}

func TestConcurrentFormatChoicesKeepEveryExclusion(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	want := core.ExcludableFamilies()
	for range 10 {
		if _, err := st.ClearExcludedFormats(ctx); err != nil {
			t.Fatal(err)
		}
		var workers sync.WaitGroup
		start := make(chan struct{})
		for _, family := range want {
			workers.Go(func() {
				<-start
				if _, err := st.SetFormatExcluded(ctx, family, true); err != nil {
					t.Error(err)
				}
			})
		}
		close(start)
		workers.Wait()
		got, err := st.ExcludedFormats(ctx)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("concurrent choices = %v, %v, want %v", got, err, want)
		}
	}
}

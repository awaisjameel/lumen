package store

import (
	"path/filepath"
	"testing"

	"lumen/internal/core"
)

func TestPreferencesStartAtTheirDefaults(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	got, err := st.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := core.DefaultPreferences(); got != want {
		t.Errorf("Preferences() = %+v, want the defaults %+v", got, want)
	}
}

func TestSetPreferencesStoresNormalisedValuesThatSurviveReopening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	stored, err := st.SetPreferences(ctx, core.Preferences{Theme: core.ThemeDark, Accent: core.AccentTeal,
		Density: core.DensityCompact, TextScale: 999, TileSize: 200, SidebarCollapsed: true,
		SidebarWidth: 260, InspectorWidth: 360})
	if err != nil {
		t.Fatal(err)
	}
	want := core.Preferences{Theme: core.ThemeDark, Accent: core.AccentTeal, Density: core.DensityCompact,
		TextScale: core.TextScaleMax, TileSize: 200, SidebarCollapsed: true, SidebarWidth: 260, InspectorWidth: 360}
	if stored != want {
		t.Errorf("SetPreferences stored %+v, want %+v", stored, want)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if got, err := st.Preferences(ctx); err != nil || got != want {
		t.Errorf("after reopening Preferences() = %+v, %v, want %+v", got, err, want)
	}
}

func TestAStoredPreferenceTheUICannotApplyReadsAsItsDefault(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	if err := st.setSetting(ctx, core.SettingUITheme, "sepia"); err != nil {
		t.Fatal(err)
	}
	if err := st.setSetting(ctx, core.SettingUITileSize, "not a number"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != core.ThemeSystem || got.TileSize != core.TileSizeDefault {
		t.Errorf("Preferences() = %+v, want the default theme and tile size", got)
	}
}

func TestLegacyPreferencesFillOnlyWhatTheLibraryNeverChose(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	// The user already chose a theme in the new settings; the tile size is unset.
	chosen := core.DefaultPreferences()
	chosen.Theme = core.ThemeLight
	if err := st.setSetting(ctx, core.SettingUITheme, string(chosen.Theme)); err != nil {
		t.Fatal(err)
	}
	got, err := st.MigrateLegacyPreferences(ctx, core.LegacyPreferences{Theme: core.ThemeDark, TileSize: 240})
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != core.ThemeLight {
		t.Errorf("the legacy theme replaced the library's own: got %q", got.Theme)
	}
	if got.TileSize != 240 {
		t.Errorf("TileSize = %d, want the legacy 240", got.TileSize)
	}
}

func TestLegacyPreferencesAreOfferedOnlyOnce(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	if _, err := st.MigrateLegacyPreferences(ctx, core.LegacyPreferences{Theme: core.ThemeDark}); err != nil {
		t.Fatal(err)
	}
	// A repeat (the app restarted, or the first call's answer was lost) with
	// other values changes nothing, and a tile size not offered the first
	// time is not picked up later.
	got, err := st.MigrateLegacyPreferences(ctx, core.LegacyPreferences{Theme: core.ThemeLight, TileSize: 300})
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != core.ThemeDark || got.TileSize != core.TileSizeDefault {
		t.Errorf("a repeated migration changed the preferences: %+v", got)
	}
}

func TestLegacyPreferencesIgnoreValuesOutOfRange(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	got, err := st.MigrateLegacyPreferences(ctx, core.LegacyPreferences{Theme: "sepia", TileSize: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if want := core.DefaultPreferences(); got != want {
		t.Errorf("Preferences() = %+v, want the defaults %+v", got, want)
	}
	// It still counts as the one offer.
	if done, err := st.Bool(ctx, core.SettingUILegacyMigrated, false); err != nil || !done {
		t.Errorf("migration marker = %v, %v, want it set", done, err)
	}
}

func TestEachLibraryMigratesItsOwnLegacyPreferences(t *testing.T) {
	// Switching data directories: a second library has no marker, so the same
	// legacy values reach it too, without touching the first.
	first, second, ctx := openTest(t), openTest(t), t.Context()
	if _, err := first.MigrateLegacyPreferences(ctx, core.LegacyPreferences{Theme: core.ThemeDark}); err != nil {
		t.Fatal(err)
	}
	p := core.DefaultPreferences()
	p.Theme = core.ThemeLight
	if _, err := first.SetPreferences(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := second.MigrateLegacyPreferences(ctx, core.LegacyPreferences{Theme: core.ThemeDark})
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != core.ThemeDark {
		t.Errorf("the second library's theme = %q, want the legacy dark", got.Theme)
	}
	if kept, err := first.Preferences(ctx); err != nil || kept.Theme != core.ThemeLight {
		t.Errorf("the first library's theme = %q, %v, want light", kept.Theme, err)
	}
}

func TestPreferencesNeverCombineDifferentSavedChoices(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	first := core.DefaultPreferences()
	second := core.Preferences{Theme: core.ThemeDark, Accent: core.AccentTeal,
		Density: core.DensityCompact, TextScale: 120, TileSize: 240, SidebarCollapsed: true,
		SidebarWidth: 300, InspectorWidth: 400}
	if _, err := st.SetPreferences(ctx, first); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for range 128 {
			for _, choice := range []core.Preferences{second, first} {
				if _, err := st.SetPreferences(ctx, choice); err != nil {
					done <- err
					return
				}
			}
		}
		done <- nil
	}()
	for range 512 {
		got, err := st.Preferences(ctx)
		if err != nil {
			t.Error(err)
			break
		}
		if got != first && got != second {
			t.Errorf("Preferences returned part of each saved choice: %+v", got)
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

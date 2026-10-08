package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"lumen/internal/core"
)

// Preferences returns how the user likes the interface to look: each stored
// choice, or its default where none was made, all held to what the UI can
// apply (core.Preferences.Normalised).
func (s *Store) Preferences(ctx context.Context) (core.Preferences, error) {
	// A preference set is one choice: a read must not combine values from
	// either side of a save while its cache entries are being replaced.
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.preferences(ctx)
}

// preferences reads the preferences. The caller holds settingsMu.
func (s *Store) preferences(ctx context.Context) (core.Preferences, error) {
	p := core.DefaultPreferences()
	for key, to := range map[string]*string{
		core.SettingUITheme:            (*string)(&p.Theme),
		core.SettingUIAccent:           (*string)(&p.Accent),
		core.SettingUIDensity:          (*string)(&p.Density),
		core.SettingUIMotion:           (*string)(&p.Motion),
		core.SettingUIViewerBackground: (*string)(&p.ViewerBackground),
		core.SettingUIStartView:        (*string)(&p.StartView),
		core.SettingUIThumbFit:         (*string)(&p.ThumbFit),
		core.SettingUIFocusRing:        (*string)(&p.FocusRing),
	} {
		v, ok, err := s.setting(ctx, key)
		if err != nil {
			return core.Preferences{}, err
		}
		if ok {
			*to = v
		}
	}
	for key, to := range map[string]*int{
		core.SettingUITextScale:      &p.TextScale,
		core.SettingUITileSize:       &p.TileSize,
		core.SettingUISidebarWidth:   &p.SidebarWidth,
		core.SettingUIInspectorWidth: &p.InspectorWidth,
	} {
		n, err := s.Int(ctx, key, *to)
		if err != nil {
			return core.Preferences{}, err
		}
		*to = n
	}
	for key, to := range map[string]*bool{
		core.SettingUISidebarCollapsed: &p.SidebarCollapsed,
		core.SettingUIHideFavourite:    &p.HideFavouriteBadge,
		core.SettingUIHideVideo:        &p.HideVideoBadge,
		core.SettingUIHideDuration:     &p.HideDurationBadge,
	} {
		on, err := s.Bool(ctx, key, false)
		if err != nil {
			return core.Preferences{}, err
		}
		*to = on
	}
	viewer, ok, err := s.setting(ctx, core.SettingUIViewer)
	if err != nil {
		return core.Preferences{}, err
	}
	if ok {
		if err := json.Unmarshal([]byte(viewer), &p.Viewer); err != nil {
			return core.Preferences{}, err
		}
		// Before videos had a loop of their own they started as the slideshow's
		// was set: a library saved then keeps that until the user chooses.
		var stored map[string]json.RawMessage
		if err := json.Unmarshal([]byte(viewer), &stored); err == nil && p.Viewer != nil {
			if _, chosen := stored["videoLoop"]; !chosen {
				p.Viewer.VideoLoop = p.Viewer.Loop
			}
		}
	}
	// A list this Lumen cannot read (a newer one wrote it) is as if never
	// set, like any other unreadable setting.
	shortcuts, ok, err := s.setting(ctx, core.SettingUIShortcuts)
	if err != nil {
		return core.Preferences{}, err
	}
	if ok && json.Unmarshal([]byte(shortcuts), &p.Shortcuts) != nil {
		p.Shortcuts = nil
	}
	presets, ok, err := s.setting(ctx, core.SettingUIExportPresets)
	if err != nil {
		return core.Preferences{}, err
	}
	if ok && json.Unmarshal([]byte(presets), &p.ExportPresets) != nil {
		p.ExportPresets = nil
	}
	return p.Normalised(), nil
}

// SetPreferences stores every interface preference at once, normalised, and
// returns what was stored: a save is all of one request's values or none.
func (s *Store) SetPreferences(ctx context.Context, p core.Preferences) (core.Preferences, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.setPreferences(ctx, p)
}

// setPreferences stores the preferences. The caller holds settingsMu.
func (s *Store) setPreferences(ctx context.Context, p core.Preferences) (core.Preferences, error) {
	p = p.Normalised()
	set := preferenceSettings(p)
	for _, part := range []struct {
		key   string
		value any
	}{
		{core.SettingUIViewer, p.Viewer},
		{core.SettingUIShortcuts, p.Shortcuts},
		{core.SettingUIExportPresets, p.ExportPresets},
	} {
		data, err := json.Marshal(part.value)
		if err != nil {
			return core.Preferences{}, err
		}
		set = append(set, setting{part.key, string(data)})
	}
	return p, s.writeSettings(ctx, set)
}

// ResetPreferences puts the interface preferences of the given categories
// back to their defaults, in one save, and returns the preferences
// afterwards. The other categories keep what they hold.
func (s *Store) ResetPreferences(ctx context.Context, categories []core.PrefCategory) (core.Preferences, error) {
	// The lock covers the read and the write: a save racing the reset is
	// either reset with the rest or written after it, never half of each.
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	p, err := s.preferences(ctx)
	if err != nil {
		return core.Preferences{}, err
	}
	for _, c := range categories {
		p = p.Reset(c)
	}
	return s.setPreferences(ctx, p)
}

// MigrateLegacyPreferences offers this library the choices an older Lumen
// kept in the WebView's storage, once: a legacy value is stored only where
// the library has no value of its own, and never again after the first
// call. It returns the preferences as they stand afterwards.
func (s *Store) MigrateLegacyPreferences(ctx context.Context, legacy core.LegacyPreferences) (core.Preferences, error) {
	if err := s.migrateLegacyPreferences(ctx, legacy); err != nil {
		return core.Preferences{}, err
	}
	return s.Preferences(ctx)
}

func (s *Store) migrateLegacyPreferences(ctx context.Context, legacy core.LegacyPreferences) error {
	// The lock covers the check and the write, so a save racing the
	// migration is either seen by it or written after it.
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	done, err := s.Bool(ctx, core.SettingUILegacyMigrated, false)
	if err != nil || done {
		return err
	}
	set := []setting{{core.SettingUILegacyMigrated, "1"}}
	for _, c := range []struct {
		key, value string
		valid      bool
	}{
		{core.SettingUITheme, string(legacy.Theme), core.ValidTheme(legacy.Theme)},
		{core.SettingUITileSize, strconv.Itoa(legacy.TileSize),
			legacy.TileSize >= core.TileSizeMin && legacy.TileSize <= core.TileSizeMax},
	} {
		if !c.valid {
			continue
		}
		_, exists, err := s.setting(ctx, c.key)
		if err != nil {
			return err
		}
		if !exists {
			set = append(set, setting{c.key, c.value})
		}
	}
	return s.writeSettings(ctx, set)
}

// setting is one key and the value to store for it.
type setting struct{ key, value string }

func preferenceSettings(p core.Preferences) []setting {
	flag := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	return []setting{
		{core.SettingUITheme, string(p.Theme)},
		{core.SettingUIAccent, string(p.Accent)},
		{core.SettingUIDensity, string(p.Density)},
		{core.SettingUITextScale, strconv.Itoa(p.TextScale)},
		{core.SettingUITileSize, strconv.Itoa(p.TileSize)},
		{core.SettingUISidebarCollapsed, flag(p.SidebarCollapsed)},
		{core.SettingUISidebarWidth, strconv.Itoa(p.SidebarWidth)},
		{core.SettingUIInspectorWidth, strconv.Itoa(p.InspectorWidth)},
		{core.SettingUIMotion, string(p.Motion)},
		{core.SettingUIViewerBackground, string(p.ViewerBackground)},
		{core.SettingUIStartView, string(p.StartView)},
		{core.SettingUIThumbFit, string(p.ThumbFit)},
		{core.SettingUIFocusRing, string(p.FocusRing)},
		{core.SettingUIHideFavourite, flag(p.HideFavouriteBadge)},
		{core.SettingUIHideVideo, flag(p.HideVideoBadge)},
		{core.SettingUIHideDuration, flag(p.HideDurationBadge)},
	}
}

// ExcludedFormats is the photo formats indexing leaves out.
func (s *Store) ExcludedFormats(ctx context.Context) ([]core.Family, error) {
	v, _, err := s.setting(ctx, core.SettingFormatsExcluded)
	if err != nil {
		return nil, err
	}
	var list []core.Family
	for name := range strings.SplitSeq(v, ",") {
		list = append(list, core.Family(name))
	}
	return core.NormalisedFamilies(list), nil
}

// SetExcludedFormats stores the photo formats indexing leaves out, held to
// those that can be, and returns what was stored.
func (s *Store) SetExcludedFormats(ctx context.Context, list []core.Family) ([]core.Family, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.setExcludedFormats(ctx, list)
}

// SetFormatExcluded changes one format under the settings lock, so concurrent
// choices of different formats cannot overwrite one another.
func (s *Store) SetFormatExcluded(ctx context.Context, family core.Family, excluded bool) (bool, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	list, err := s.ExcludedFormats(ctx)
	if err != nil || slices.Contains(list, family) == excluded {
		return false, err
	}
	if excluded {
		list = append(list, family)
	} else {
		list = slices.DeleteFunc(list, func(f core.Family) bool { return f == family })
	}
	_, err = s.setExcludedFormats(ctx, list)
	return err == nil, err
}

func (s *Store) setExcludedFormats(ctx context.Context, list []core.Family) ([]core.Family, error) {
	list = core.NormalisedFamilies(list)
	names := make([]string, len(list))
	for i, f := range list {
		names[i] = string(f)
	}
	return list, s.writeSettings(ctx, []setting{{core.SettingFormatsExcluded, strings.Join(names, ",")}})
}

// writeSettings stores several settings in one statement, then updates the
// cache. The caller holds settingsMu (see Store.settings).
func (s *Store) writeSettings(ctx context.Context, set []setting) error {
	if len(set) == 0 {
		return nil
	}
	args := make([]any, 0, 2*len(set))
	for _, kv := range set {
		args = append(args, kv.key, kv.value)
	}
	// The settle step of tx is for photos: settings change none.
	err := s.runTx(ctx, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES "+
			strings.TrimSuffix(strings.Repeat("(?, ?),", len(set)), ",")+
			" ON CONFLICT (key) DO UPDATE SET value = excluded.value", args...)
		return err
	})
	if err != nil {
		return err
	}
	for _, kv := range set {
		s.settings.Store(kv.key, kv.value)
	}
	return nil
}

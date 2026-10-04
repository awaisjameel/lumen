package store

import (
	"context"
	"database/sql"
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
	p := core.DefaultPreferences()
	for key, to := range map[string]*string{
		core.SettingUITheme:   (*string)(&p.Theme),
		core.SettingUIAccent:  (*string)(&p.Accent),
		core.SettingUIDensity: (*string)(&p.Density),
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
	collapsed, err := s.Bool(ctx, core.SettingUISidebarCollapsed, false)
	if err != nil {
		return core.Preferences{}, err
	}
	p.SidebarCollapsed = collapsed
	return p.Normalised(), nil
}

// SetPreferences stores every interface preference at once, normalised, and
// returns what was stored: a save is all of one request's values or none.
func (s *Store) SetPreferences(ctx context.Context, p core.Preferences) (core.Preferences, error) {
	p = p.Normalised()
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return p, s.writeSettings(ctx, preferenceSettings(p))
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
	collapsed := "0"
	if p.SidebarCollapsed {
		collapsed = "1"
	}
	return []setting{
		{core.SettingUITheme, string(p.Theme)},
		{core.SettingUIAccent, string(p.Accent)},
		{core.SettingUIDensity, string(p.Density)},
		{core.SettingUITextScale, strconv.Itoa(p.TextScale)},
		{core.SettingUITileSize, strconv.Itoa(p.TileSize)},
		{core.SettingUISidebarCollapsed, collapsed},
		{core.SettingUISidebarWidth, strconv.Itoa(p.SidebarWidth)},
		{core.SettingUIInspectorWidth, strconv.Itoa(p.InspectorWidth)},
	}
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

package store

import (
	"context"
	"encoding/json"
	"slices"

	"lumen/internal/core"
)

// RecentSearches returns the searches the user ran last, newest first. A list
// this Lumen cannot read (a newer one wrote it) is as if none were kept.
func (s *Store) RecentSearches(ctx context.Context) ([]string, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.recentSearches(ctx)
}

// recentSearches reads the list. The caller holds settingsMu.
func (s *Store) recentSearches(ctx context.Context) ([]string, error) {
	v, ok, err := s.setting(ctx, core.SettingRecentSearches)
	if err != nil {
		return nil, err
	}
	var list []string
	if ok && json.Unmarshal([]byte(v), &list) != nil {
		list = nil
	}
	return core.NormalisedRecentSearches(list), nil
}

// RememberSearch puts query first among the recent searches (once, however
// it was spelt before) and returns them. The lock covers the read and the
// write, so two searches run together are both kept.
func (s *Store) RememberSearch(ctx context.Context, query string) ([]string, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	list, err := s.recentSearches(ctx)
	if err != nil {
		return nil, err
	}
	list = core.NormalisedRecentSearches(slices.Insert(list, 0, query))
	return list, s.writeRecentSearches(ctx, list)
}

// ClearRecentSearches forgets every recent search.
func (s *Store) ClearRecentSearches(ctx context.Context) error {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.writeRecentSearches(ctx, []string{})
}

// writeRecentSearches stores the list. The caller holds settingsMu.
func (s *Store) writeRecentSearches(ctx context.Context, list []string) error {
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return s.writeSettings(ctx, []setting{{core.SettingRecentSearches, string(data)}})
}

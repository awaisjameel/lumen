package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"lumen/internal/core"
)

// ErrInvalidAlbum means a name, kind, or smart rule cannot be saved.
var ErrInvalidAlbum = errors.New("invalid album")

func albumName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return "", fmt.Errorf("%w: name must be 1–80 characters", ErrInvalidAlbum)
	}
	return name, nil
}

func validateAlbumRule(rule core.AlbumRule) error {
	if rule.Kind != "" && rule.Kind != core.KindPhoto && rule.Kind != core.KindVideo {
		return fmt.Errorf("%w: unknown media kind", ErrInvalidAlbum)
	}
	if rule.Tag < 0 || rule.Person < 0 {
		return fmt.Errorf("%w: invalid tag or person", ErrInvalidAlbum)
	}
	for _, day := range []string{rule.From, rule.To} {
		if day == "" {
			continue
		}
		parsed, err := time.Parse(time.DateOnly, day)
		if err != nil || parsed.Format(time.DateOnly) != day {
			return fmt.Errorf("%w: date must be YYYY-MM-DD", ErrInvalidAlbum)
		}
	}
	if rule.From != "" && rule.To != "" && rule.From > rule.To {
		return fmt.Errorf("%w: start date follows end date", ErrInvalidAlbum)
	}
	return nil
}

// CreateAlbum creates a manual album or a saved smart rule.
func (s *Store) CreateAlbum(ctx context.Context, name, kind string, rule core.AlbumRule) (int64, error) {
	name, err := albumName(name)
	if err != nil {
		return 0, err
	}
	if kind != "manual" && kind != "smart" {
		return 0, fmt.Errorf("%w: unknown album kind", ErrInvalidAlbum)
	}
	if err := validateAlbumRule(rule); err != nil {
		return 0, err
	}
	if kind == "manual" {
		rule = core.AlbumRule{}
	}
	body, err := json.Marshal(rule)
	if err != nil {
		return 0, err
	}
	result, err := s.w.ExecContext(ctx, `INSERT INTO albums(name, kind, rule_json, created_at) VALUES(?, ?, ?, ?)`,
		name, kind, string(body), time.Now().Unix())
	if isUnique(err) {
		return 0, fmt.Errorf("%w: an album already has that name", ErrInvalidAlbum)
	}
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateAlbum changes its name and, for smart albums, its rule.
func (s *Store) UpdateAlbum(ctx context.Context, id int64, name string, rule core.AlbumRule) error {
	name, err := albumName(name)
	if err != nil {
		return err
	}
	if err := validateAlbumRule(rule); err != nil {
		return err
	}
	album, err := s.album(ctx, id)
	if err != nil {
		return err
	}
	if album.Kind == "manual" {
		rule = core.AlbumRule{}
	}
	body, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	result, err := s.w.ExecContext(ctx, `UPDATE albums SET name = ?, rule_json = ? WHERE id = ?`, name, string(body), id)
	if isUnique(err) {
		return fmt.Errorf("%w: an album already has that name", ErrInvalidAlbum)
	}
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// DeleteAlbum removes a collection and its memberships, leaving assets untouched.
func (s *Store) DeleteAlbum(ctx context.Context, id int64) error {
	result, err := s.w.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) album(ctx context.Context, id int64) (core.Album, error) {
	var a core.Album
	var raw string
	err := s.r.QueryRowContext(ctx, `SELECT id, name, kind, COALESCE(rule_json, '{}') FROM albums WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.Kind, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal([]byte(raw), &a.Rule); err != nil {
		return a, fmt.Errorf("album %d rule: %w", id, err)
	}
	return a, nil
}

// Albums includes live counts and covers. A smart album reacts to new assets,
// changed metadata, tags and people without maintaining a second membership.
func (s *Store) Albums(ctx context.Context, minScore float32) ([]core.Album, error) {
	albums, err := queryAll(ctx, s.r, func(r *sql.Rows) (a core.Album, err error) {
		var raw string
		err = r.Scan(&a.ID, &a.Name, &a.Kind, &raw)
		if err == nil {
			err = json.Unmarshal([]byte(raw), &a.Rule)
		}
		return a, err
	}, `SELECT id, name, kind, COALESCE(rule_json, '{}') FROM albums ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	for i := range albums {
		where, args := albumPredicate(albums[i], minScore)
		if err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM assets WHERE `+where, args...).Scan(&albums[i].Count); err != nil {
			return nil, err
		}
		if albums[i].Count == 0 {
			continue
		}
		if err := s.r.QueryRowContext(ctx, `SELECT id, thumb_version FROM assets WHERE `+where+
			` ORDER BY taken_at DESC, id DESC LIMIT 1`, args...).Scan(&albums[i].Cover, &albums[i].CoverThumb); err != nil {
			return nil, err
		}
	}
	return albums, nil
}

// AssetAlbums returns the manual collections an asset belongs to. Smart albums
// are computed from their rule and shown in the album browser.
func (s *Store) AssetAlbums(ctx context.Context, assetID int64) ([]int64, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (id int64, err error) {
		err = r.Scan(&id)
		return id, err
	}, `SELECT album_id FROM album_items WHERE asset_id = ?`, assetID)
}

// SetAlbumItem adds or removes an asset from a manual album.
func (s *Store) SetAlbumItem(ctx context.Context, albumID, assetID int64, include bool) error {
	if albumID <= 0 || assetID <= 0 {
		return ErrNotFound
	}
	album, err := s.album(ctx, albumID)
	if err != nil {
		return err
	}
	if album.Kind != "manual" {
		return fmt.Errorf("%w: only manual albums accept items", ErrInvalidAlbum)
	}
	var exists bool
	if err := s.r.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM assets WHERE id = ?)`, assetID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if include {
		_, err = s.w.ExecContext(ctx, `INSERT OR IGNORE INTO album_items(album_id, asset_id, added_at) VALUES(?, ?, ?)`, albumID, assetID, time.Now().Unix())
	} else {
		_, err = s.w.ExecContext(ctx, `DELETE FROM album_items WHERE album_id = ? AND asset_id = ?`, albumID, assetID)
	}
	return err
}

// albumPredicate returns a fixed SQL predicate with bound values only.
func albumPredicate(a core.Album, minScore float32) (where string, args []any) {
	where, args = visible, []any{}
	if a.Kind == "manual" {
		return where + ` AND id IN (SELECT asset_id FROM album_items WHERE album_id = ?)`, []any{a.ID}
	}
	rule := a.Rule
	if rule.Kind != "" {
		where += ` AND kind = ?`
		args = append(args, rule.Kind)
	}
	if rule.Favourites {
		where += ` AND favourite = 1`
	}
	if rule.From != "" {
		where += ` AND taken_at >= ?`
		day, _ := time.Parse(time.DateOnly, rule.From)
		args = append(args, day.Unix())
	}
	if rule.To != "" {
		where += ` AND taken_at < ?`
		day, _ := time.Parse(time.DateOnly, rule.To)
		args = append(args, day.AddDate(0, 0, 1).Unix())
	}
	if rule.Tag != 0 {
		where += ` AND id IN (SELECT x.asset_id FROM asset_tags x WHERE x.tag_id = ? AND ` + tagApplies + `)`
		args = append(args, rule.Tag, minScore)
	}
	if rule.Person != 0 {
		where += ` AND id IN (SELECT asset_id FROM faces WHERE person_id = ?)`
		args = append(args, rule.Person)
	}
	return where, args
}

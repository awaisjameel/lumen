package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"lumen/internal/core"
)

var (
	// ErrInvalidAlbum means a name, kind, or smart rule cannot be saved.
	ErrInvalidAlbum = errors.New("invalid album")
	// ErrAlbumExists means another album has that name (in any case). It
	// comes wrapped with ErrInvalidAlbum.
	ErrAlbumExists = errors.New("an album already has that name")
)

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

// albumName checks a new name for album id (0 for a new album) in the
// writer's transaction.
func albumName(ctx context.Context, tx *sql.Tx, name string, id int64) (string, error) {
	name, err := cleanName(name)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidAlbum, err)
	}
	switch other, err := named(ctx, tx, albumNames, name, id); {
	case err != nil:
		return "", err
	case other != 0:
		return "", fmt.Errorf("%w: %w", ErrInvalidAlbum, ErrAlbumExists)
	}
	return name, nil
}

// CreateAlbum creates a manual album holding items, or a saved smart rule
// (items must then be empty), all at once.
func (s *Store) CreateAlbum(ctx context.Context, name string, kind core.AlbumKind, rule core.AlbumRule, items []int64) (int64, error) {
	switch kind {
	case core.AlbumManual:
		rule = core.AlbumRule{}
	case core.AlbumSmart:
		if len(items) > 0 {
			return 0, fmt.Errorf("%w: smart albums choose their own items", ErrInvalidAlbum)
		}
	default:
		return 0, fmt.Errorf("%w: unknown album kind", ErrInvalidAlbum)
	}
	if err := validateAlbumRule(rule); err != nil {
		return 0, err
	}
	body, err := json.Marshal(rule)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.tx(ctx, func(tx *sql.Tx) error {
		name, err := albumName(ctx, tx, name, 0)
		if err != nil {
			return err
		}
		if err := albumReferences(ctx, tx, rule); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO albums (name, kind, rule_json, created_at) VALUES (?, ?, ?, ?)`,
			name, kind, string(body), time.Now().Unix())
		if err == nil {
			id, err = result.LastInsertId()
		}
		for _, asset := range items {
			if err == nil {
				err = setAlbumItem(ctx, tx, id, asset, true)
			}
		}
		return err
	})
	if isUnique(err) {
		return 0, fmt.Errorf("%w: %w", ErrInvalidAlbum, ErrAlbumExists)
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateAlbum changes its name and, for smart albums, its rule.
func (s *Store) UpdateAlbum(ctx context.Context, id int64, name string, rule core.AlbumRule) error {
	if err := validateAlbumRule(rule); err != nil {
		return err
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		a, err := album(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.Kind == core.AlbumManual {
			rule = core.AlbumRule{}
		}
		body, err := json.Marshal(rule)
		if err != nil {
			return err
		}
		if name, err = albumName(ctx, tx, name, id); err != nil {
			return err
		}
		if err := albumReferences(ctx, tx, rule); err != nil {
			return err
		}
		return affected(tx.ExecContext(ctx, `UPDATE albums SET name = ?, rule_json = ? WHERE id = ?`, name, string(body), id))
	})
	if isUnique(err) {
		return fmt.Errorf("%w: %w", ErrInvalidAlbum, ErrAlbumExists)
	}
	return err
}

// Validate references in the write transaction so deletion cannot race a save.
func albumReferences(ctx context.Context, tx *sql.Tx, rule core.AlbumRule) error {
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT
		(?1 = 0 OR EXISTS (SELECT 1 FROM tags WHERE id = ?1)) AND
		(?2 = 0 OR EXISTS (SELECT 1 FROM people WHERE id = ?2))`, rule.Tag, rule.Person).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: the selected Thing or person no longer exists", ErrInvalidAlbum)
	}
	return nil
}

// DeleteAlbum removes a collection and its memberships, leaving assets untouched.
func (s *Store) DeleteAlbum(ctx context.Context, id int64) error {
	return affected(s.w.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id))
}

func album(ctx context.Context, q querier, id int64) (core.Album, error) {
	albums, err := queryAll(ctx, q, scanAlbum, `SELECT id, name, kind, COALESCE(rule_json, '{}') FROM albums WHERE id = ?
		AND NOT `+labelBlocked(core.BlockAlbum, "albums.id"), id)
	if err != nil {
		return core.Album{}, err
	}
	if len(albums) == 0 {
		return core.Album{}, ErrNotFound
	}
	return albums[0], nil
}

// Albums includes live counts and covers. A smart album reacts to new assets,
// changed metadata, tags and people without maintaining a second membership.
func (s *Store) Albums(ctx context.Context, minScore float64) ([]core.Album, error) {
	// Counts and covers must describe the same snapshot while indexing continues.
	var albums []core.Album
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		albums, err = queryAll(ctx, tx, scanAlbum, `SELECT id, name, kind, COALESCE(rule_json, '{}') FROM albums
			WHERE NOT `+labelBlocked(core.BlockAlbum, "albums.id")+`
			ORDER BY name COLLATE NOCASE`)
		if err != nil {
			return err
		}
		for i := range albums {
			a := &albums[i]
			where, args := albumFilter(*a).where(minScore)
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM assets WHERE `+where, args...).Scan(&a.Count); err != nil {
				return err
			}
			if a.Count == 0 {
				continue // no cover to find
			}
			// The newest member that has a thumbnail yet.
			err := tx.QueryRowContext(ctx, `SELECT id, thumb_version FROM assets WHERE `+where+
				` AND thumb_version > 0 ORDER BY taken_at DESC, id DESC LIMIT 1`, args...).Scan(&a.Cover, &a.CoverThumb)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		return nil
	})
	return albums, err
}

func scanAlbum(r *sql.Rows) (core.Album, error) {
	var a core.Album
	var raw string
	if err := r.Scan(&a.ID, &a.Name, &a.Kind, &raw); err != nil {
		return a, err
	}
	if err := json.Unmarshal([]byte(raw), &a.Rule); err != nil {
		return a, fmt.Errorf("album %d rule: %w", a.ID, err)
	}
	return a, nil
}

// albumFilter selects an album's members: a manual album's items, or what
// a smart album's rule describes.
func albumFilter(a core.Album) assetFilter {
	if a.Kind == core.AlbumManual {
		return assetFilter{album: a.ID}
	}
	r := a.Rule
	return assetFilter{kind: r.Kind, favourites: r.Favourites, from: r.From, to: r.To, tag: r.Tag, person: r.Person}
}

// AssetAlbums returns the manual collections an asset belongs to. Smart albums
// are computed from their rule and shown in the album browser.
func (s *Store) AssetAlbums(ctx context.Context, assetID int64) ([]int64, error) {
	return queryAll(ctx, s.r, scanID, `SELECT i.album_id FROM album_items i WHERE i.asset_id = ?
		AND EXISTS (SELECT 1 FROM assets WHERE id = i.asset_id AND `+visible+`)
		AND NOT `+labelBlocked(core.BlockAlbum, "i.album_id"), assetID)
}

// SetAlbumItem adds or removes an asset from a manual album.
func (s *Store) SetAlbumItem(ctx context.Context, albumID, assetID int64, include bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error { return setAlbumItem(ctx, tx, albumID, assetID, include) })
}

func setAlbumItem(ctx context.Context, tx *sql.Tx, albumID, assetID int64, include bool) error {
	var kind core.AlbumKind
	err := tx.QueryRowContext(ctx, `SELECT kind FROM albums WHERE id = ?
		AND EXISTS (SELECT 1 FROM assets WHERE id = ? AND `+present+`)`, albumID, assetID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if kind != core.AlbumManual {
		return fmt.Errorf("%w: only manual albums accept items", ErrInvalidAlbum)
	}
	if include {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO album_items (album_id, asset_id, added_at) VALUES (?, ?, ?)`,
			albumID, assetID, time.Now().Unix())
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM album_items WHERE album_id = ? AND asset_id = ?`, albumID, assetID)
	}
	return err
}

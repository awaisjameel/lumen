package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"lumen/internal/core"
)

var (
	// ErrInvalidAlbum means a name, kind, or smart rule cannot be saved.
	ErrInvalidAlbum = errors.New("invalid album")
	// ErrAlbumExists means another album has that name (in any case). It
	// comes wrapped with ErrInvalidAlbum.
	ErrAlbumExists = errors.New("an album already has that name")
	// ErrAlbumMediaKind rejects an unsupported photo/video filter.
	ErrAlbumMediaKind = fmt.Errorf("%w: unknown media kind", ErrInvalidAlbum)
	// ErrAlbumSelection rejects a negative Thing or person ID.
	ErrAlbumSelection = fmt.Errorf("%w: invalid tag or person", ErrInvalidAlbum)
	// ErrAlbumDate rejects an invalid calendar date.
	ErrAlbumDate = fmt.Errorf("%w: date must be YYYY-MM-DD", ErrInvalidAlbum)
	// ErrAlbumDateOrder rejects a reversed date range.
	ErrAlbumDateOrder = fmt.Errorf("%w: start date follows end date", ErrInvalidAlbum)
	// ErrAlbumSmartItems rejects explicit members in a smart album.
	ErrAlbumSmartItems = fmt.Errorf("%w: smart albums choose their own items", ErrInvalidAlbum)
	// ErrAlbumKind rejects an unsupported collection kind.
	ErrAlbumKind = fmt.Errorf("%w: unknown album kind", ErrInvalidAlbum)
	// ErrAlbumReference rejects an unavailable Thing or person.
	ErrAlbumReference = fmt.Errorf("%w: the selected Thing or person is unavailable", ErrInvalidAlbum)
	// ErrAlbumChanged rejects an unsaved definition based on an older version.
	ErrAlbumChanged = errors.New("album definition changed")
	// The preview conceals blocked reference identities; writes explain that
	// the reference is unavailable without naming it.
	errAlbumReferenceHidden = fmt.Errorf("%w: selected reference is blocked", ErrAlbumReference)
	// ErrAlbumManualOnly rejects member edits in a smart album.
	ErrAlbumManualOnly = fmt.Errorf("%w: only manual albums accept items", ErrInvalidAlbum)
)

func validateAlbumRule(rule core.AlbumRule) error {
	if rule.Kind != "" && rule.Kind != core.KindPhoto && rule.Kind != core.KindVideo {
		return ErrAlbumMediaKind
	}
	if rule.Tag < 0 || rule.Person < 0 {
		return ErrAlbumSelection
	}
	for _, day := range []string{rule.From, rule.To} {
		if day == "" {
			continue
		}
		parsed, err := time.Parse(time.DateOnly, day)
		if err != nil || parsed.Format(time.DateOnly) != day {
			return ErrAlbumDate
		}
	}
	if rule.From != "" && rule.To != "" && rule.From > rule.To {
		return ErrAlbumDateOrder
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
			return 0, ErrAlbumSmartItems
		}
	default:
		return 0, ErrAlbumKind
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
				_, err = setAlbumItem(ctx, tx, asset, id, true)
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
	_, err := s.updateAlbum(ctx, id, name, &rule, nil)
	return err
}

// SaveAlbum writes the intended entity and definition atomically. A retry of
// an already committed identical result succeeds without replaying the write.
func (s *Store) SaveAlbum(ctx context.Context, edit core.AlbumEdit) (core.AlbumVersion, error) {
	if edit.ID <= 0 || edit.Version.Identity == "" || edit.Version.Revision == "" {
		return core.AlbumVersion{}, ErrInvalidAlbum
	}
	return s.updateAlbum(ctx, edit.ID, edit.Name, edit.Rule, &edit.Version)
}

func (s *Store) updateAlbum(ctx context.Context, id int64, name string, rule *core.AlbumRule, expected *core.AlbumVersion) (core.AlbumVersion, error) {
	if rule != nil {
		if err := validateAlbumRule(*rule); err != nil {
			return core.AlbumVersion{}, err
		}
	}
	var saved core.AlbumVersion
	err := s.tx(ctx, func(tx *sql.Tx) error {
		a, err := album(ctx, tx, id)
		if err != nil {
			return err
		}
		if expected != nil && a.Version.Identity != expected.Identity {
			return ErrNotFound
		}
		wanted := a.Rule
		if a.Kind == core.AlbumManual {
			wanted = core.AlbumRule{}
		} else if rule != nil {
			wanted = *rule
		}
		if name, err = albumName(ctx, tx, name, id); err != nil {
			return err
		}
		saved, err = albumVersion(a.Version.Identity, name, a.Kind, wanted)
		if err != nil {
			return err
		}
		if saved == a.Version {
			return nil
		}
		if expected != nil && a.Version.Revision != expected.Revision {
			return ErrAlbumChanged
		}
		if rule != nil {
			if err := albumReferences(ctx, tx, wanted); err != nil {
				return err
			}
		}
		body, err := json.Marshal(wanted)
		if err != nil {
			return err
		}
		return affected(tx.ExecContext(ctx, `UPDATE albums SET name = ?, rule_json = ? WHERE id = ?`, name, string(body), id))
	})
	if isUnique(err) {
		return core.AlbumVersion{}, fmt.Errorf("%w: %w", ErrInvalidAlbum, ErrAlbumExists)
	}
	if err != nil {
		return core.AlbumVersion{}, err
	}
	return saved, nil
}

// albumReferences checks existence in the caller's snapshot. Saves use their
// write transaction, so deletion cannot race the saved rule.
func albumReferences(ctx context.Context, tx *sql.Tx, rule core.AlbumRule) error {
	for _, target := range []struct {
		kind core.BlockKind
		id   int64
	}{{core.BlockTag, rule.Tag}, {core.BlockPerson, rule.Person}} {
		if target.id == 0 {
			continue
		}
		blocked, err := isBlocked(ctx, tx, target.kind, target.id)
		if err != nil {
			return err
		}
		if blocked {
			return errAlbumReferenceHidden
		}
	}
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT
		(?1 = 0 OR EXISTS (SELECT 1 FROM tags WHERE id = ?1)) AND
		(?2 = 0 OR EXISTS (SELECT 1 FROM people WHERE id = ?2))`, rule.Tag, rule.Person).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrAlbumReference
	}
	return nil
}

// PreviewAlbum evaluates an unsaved smart rule with the saved album's
// canonical predicate. Validation, count and sample share one snapshot, so
// concurrent indexing or privacy changes cannot mix their results.
func (s *Store) PreviewAlbum(ctx context.Context, rule core.AlbumRule, minScore float64) (core.AlbumPreview, error) {
	if err := validateAlbumRule(rule); err != nil {
		return core.AlbumPreview{}, err
	}
	var out core.AlbumPreview
	err := s.read(ctx, func(tx *sql.Tx) error {
		if err := albumReferences(ctx, tx, rule); err != nil {
			if errors.Is(err, errAlbumReferenceHidden) {
				return ErrNotFound
			}
			return err
		}
		where, args := albumFilter(core.Album{Kind: core.AlbumSmart, Rule: rule}).where(minScore)
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+where, args...).Scan(&out.Count); err != nil {
			return err
		}
		var err error
		out.Items, err = queryAll(ctx, tx, func(r *sql.Rows) (it core.GridItem, err error) {
			err = r.Scan(append([]any{&it.ID}, gridFields(&it)...)...)
			return it, err
		}, "SELECT id, "+gridColumns+" FROM assets WHERE "+where+
			" ORDER BY taken_at DESC, id DESC LIMIT ?", append(args, core.AlbumPreviewSize)...)
		return err
	})
	if err != nil {
		return core.AlbumPreview{}, err
	}
	return out, nil
}

// DeleteAlbum removes a collection and its memberships, leaving assets untouched.
func (s *Store) DeleteAlbum(ctx context.Context, id int64) error {
	return affected(s.w.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id))
}

func album(ctx context.Context, q querier, id int64) (core.Album, error) {
	albums, err := queryAll(ctx, q, scanAlbum, `SELECT id, name, kind, COALESCE(rule_json, '{}'), identity FROM albums WHERE id = ?
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
		albums, err = queryAll(ctx, tx, scanAlbum, `SELECT id, name, kind, COALESCE(rule_json, '{}'), identity FROM albums
			WHERE NOT `+labelBlocked(core.BlockAlbum, "albums.id")+`
			ORDER BY `+byName("name"))
		if err != nil {
			return err
		}
		for i := range albums {
			a := &albums[i]
			where, args := albumFilter(*a).where(minScore)
			err := tx.QueryRowContext(ctx, `WITH cover AS (
				SELECT id, thumb_version FROM assets WHERE `+where+`
				AND thumb_version > 0 ORDER BY taken_at DESC, id DESC LIMIT 1)
				SELECT count(*), COALESCE((SELECT id FROM cover), 0),
					COALESCE((SELECT thumb_version FROM cover), 0)
				FROM assets WHERE `+where, slices.Concat(args, args)...).Scan(&a.Count, &a.Cover, &a.CoverThumb)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return albums, err
}

func scanAlbum(r *sql.Rows) (core.Album, error) {
	var a core.Album
	var raw, identity string
	if err := r.Scan(&a.ID, &a.Name, &a.Kind, &raw, &identity); err != nil {
		return a, err
	}
	if err := json.Unmarshal([]byte(raw), &a.Rule); err != nil {
		return a, fmt.Errorf("album %d rule: %w", a.ID, err)
	}
	var err error
	a.Version, err = albumVersion(identity, a.Name, a.Kind, a.Rule)
	return a, err
}

func albumVersion(identity, name string, kind core.AlbumKind, rule core.AlbumRule) (core.AlbumVersion, error) {
	definition := struct {
		Identity string
		Name     string
		Kind     core.AlbumKind
		Rule     core.AlbumRule
	}{identity, name, kind, rule}
	body, err := json.Marshal(definition)
	if err != nil {
		return core.AlbumVersion{}, err
	}
	digest := sha256.Sum256(body)
	return core.AlbumVersion{Identity: identity, Revision: hex.EncodeToString(digest[:])}, nil
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

// SetAlbumItem adds or removes an asset from a manual album. Like tagging
// (TagAsset), it takes the asset first.
func (s *Store) SetAlbumItem(ctx context.Context, assetID, albumID int64, include bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := setAlbumItem(ctx, tx, assetID, albumID, include)
		return err
	})
}

func setAlbumItem(ctx context.Context, tx *sql.Tx, assetID, albumID int64, include bool) (bool, error) {
	var kind core.AlbumKind
	err := tx.QueryRowContext(ctx, `SELECT kind FROM albums WHERE id = ?
		AND EXISTS (SELECT 1 FROM assets WHERE id = ? AND `+present+`)`, albumID, assetID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if kind != core.AlbumManual {
		return false, ErrAlbumManualOnly
	}
	var result sql.Result
	if include {
		result, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO album_items (album_id, asset_id, added_at) VALUES (?, ?, ?)`,
			albumID, assetID, time.Now().Unix())
	} else {
		result, err = tx.ExecContext(ctx, `DELETE FROM album_items WHERE album_id = ? AND asset_id = ?`, albumID, assetID)
	}
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed > 0, err
}

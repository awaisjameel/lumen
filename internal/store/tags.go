package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"lumen/internal/core"
)

var (
	// ErrTagExists means another tag already has that name.
	ErrTagExists = errors.New("a tag with that name already exists")
	// ErrBuiltinTag means the operation only applies to tags the user made.
	ErrBuiltinTag = errors.New("built-in Things can be blocked, not deleted")
	// ErrTagInAlbum protects a saved rule from dangling or reused tag IDs.
	ErrTagInAlbum = errors.New("this Thing is used by a smart album; edit that album's rule before deleting it")
)

// VocabLabel is one built-in Thing, as the vocabulary defines it.
type VocabLabel struct {
	Name      string
	Prompts   []string
	Exclusive bool // a broad kind of photo (see search.Label)
	Sink      bool // a kind that absorbs photos and is never shown or applied
}

// AutoTag is a tag the AI applies, with the text embeddings of its prompts.
type AutoTag struct {
	ID        int64
	Name      string
	Builtin   bool // from Lumen's vocabulary (else a label the user made)
	Prompts   []string
	Exclusive bool
	Model     string    // model version that made Vecs; "" = not embedded yet
	Vecs      []float32 // len(Prompts) x dim, unit vectors
}

// SyncVocabulary makes the built-in tags match labels. New labels are added,
// changed ones are updated (their embeddings dropped, so they are redone and
// the library re-tagged), and built-ins no longer listed are removed, unless
// the user tagged photos with them, in which case they become the user's own.
// Blocked built-ins keep their prompts and matches until unblocked: retiring
// one must never remove the user's block or reveal its photos.
func (s *Store) SyncVocabulary(ctx context.Context, labels []VocabLabel) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		// hidden, which lists and suggestions read, is exactly is_sink since
		// blocking replaced the user's hiding (migration 0014).
		up, err := tx.PrepareContext(ctx, `INSERT INTO tags (name, kind, prompts, exclusive, hidden, created_at, is_sink)
			VALUES (?1, 'builtin', ?2, ?3, ?5, ?4, ?5)
			ON CONFLICT (name) DO UPDATE SET kind = 'builtin', prompts = ?2, exclusive = ?3,
				is_sink = ?5, hidden = ?5,
				model = CASE WHEN prompts IS ?2 AND exclusive = ?3 THEN model END,
				vecs = CASE WHEN prompts IS ?2 AND exclusive = ?3 THEN vecs END`)
		if err != nil {
			return err
		}
		defer up.Close()
		names := make([]any, 0, len(labels))
		for _, l := range labels {
			p, err := json.Marshal(l.Prompts)
			if err != nil {
				return err
			}
			if _, err := up.ExecContext(ctx, l.Name, string(p), l.Exclusive, now, l.Sink); err != nil {
				return err
			}
			names = append(names, l.Name)
		}
		list, err := json.Marshal(names)
		if err != nil {
			return err
		}
		stale := `kind = 'builtin' AND name NOT IN (SELECT value FROM json_each(?)) AND NOT ` + labelBlocked(core.BlockTag, "tags.id")
		if _, err := tx.ExecContext(ctx, `DELETE FROM asset_tags WHERE source = 'ai' AND tag_id IN (SELECT id FROM tags WHERE `+stale+`)`, list); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tags SET kind = 'manual', prompts = NULL, exclusive = 0, model = NULL, vecs = NULL
			WHERE `+stale+` AND (EXISTS (SELECT 1 FROM asset_tags x WHERE x.tag_id = tags.id AND x.source = 'user')
			OR EXISTS (SELECT 1 FROM albums WHERE kind = 'smart' AND `+ruleTag+` = tags.id))`, list); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM tags WHERE `+stale, list)
		return err
	})
}

// AutoTags returns every tag the AI applies (built-in and user labels,
// blocked ones included: a block hides the label, and in photo mode its
// photos, but the AI keeps applying it so the block covers new photos).
func (s *Store) AutoTags(ctx context.Context) ([]AutoTag, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (t AutoTag, err error) {
		var prompts string
		var vecs []byte
		if err := r.Scan(&t.ID, &t.Name, &t.Builtin, &prompts, &t.Exclusive, &t.Model, &vecs); err != nil {
			return t, err
		}
		if err := json.Unmarshal([]byte(prompts), &t.Prompts); err != nil {
			return t, fmt.Errorf("tag %q: prompts: %w", t.Name, err)
		}
		t.Vecs = bytesFloat32(vecs)
		return t, nil
	}, `SELECT id, name, kind = 'builtin', prompts, exclusive, COALESCE(model, ''), vecs
		FROM tags WHERE kind IN ('builtin', 'label') ORDER BY id`)
}

// SetTagVectors stores the text embeddings of a tag's prompts.
func (s *Store) SetTagVectors(ctx context.Context, id int64, model string, vecs []float32) error {
	_, err := s.w.ExecContext(ctx, "UPDATE tags SET model = ?, vecs = ? WHERE id = ?", model, float32Bytes(vecs), id)
	return err
}

// SetTagScores replaces the AI's verdict for one tag across the library
// (after a label is created or its prompts change). User edits stay.
func (s *Store) SetTagScores(ctx context.Context, tagID int64, scores []AssetScore) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM asset_tags WHERE tag_id = ? AND source = 'ai'", tagID); err != nil {
			return err
		}
		add, err := prepareAddAI(ctx, tx)
		if err != nil {
			return err
		}
		defer add.Close()
		for _, sc := range scores {
			if _, err := add.ExecContext(ctx, sc.AssetID, tagID, sc.Score); err != nil {
				return err
			}
		}
		return nil
	})
}

// AssetScore is one asset's AI probability for a tag.
type AssetScore struct {
	AssetID int64
	Score   float32
}

// Things lists every tag with how many visible assets carry it (AI tags
// counting from minScore up) and the best one to show on its card. Most
// used first.
func (s *Store) Things(ctx context.Context, minScore float64) ([]core.Tag, error) {
	var out []core.Tag
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		out, err = queryAll(ctx, tx, func(r *sql.Rows) (t core.Tag, err error) {
			err = r.Scan(&t.ID, &t.Name, &t.Kind, &t.Count)
			return t, err
		}, `SELECT t.id, t.name, t.kind, count(a.id)
			FROM tags t
			LEFT JOIN asset_tags x ON x.tag_id = t.id AND `+tagApplies+`
			LEFT JOIN assets a ON a.id = x.asset_id AND `+visibleA+`
			WHERE t.hidden = 0 AND NOT `+labelBlocked(core.BlockTag, "t.id")+`
			GROUP BY t.id ORDER BY count(a.id) DESC, t.name`, minScore)
		if err != nil {
			return err
		}
		return addTagCovers(ctx, tx, out, minScore)
	})
	return out, err
}

// addTagCovers picks each tag's cover: a photo the user tagged, else the
// AI's surest. Each probe walks asset_tags_by_tag (tag, source, score) in
// order and stops at the first photo that can be shown, rather than sorting
// every row of a tag the whole library carries.
func addTagCovers(ctx context.Context, tx *sql.Tx, tags []core.Tag, minScore float64) error {
	pick := `SELECT a.id, a.thumb_version FROM asset_tags x JOIN assets a ON a.id = x.asset_id
		WHERE x.tag_id = ?1 AND ` + visibleA + ` AND a.thumb_version > 0 AND `
	byUser, err := tx.PrepareContext(ctx, pick+`x.source = 'user' LIMIT 1`)
	if err != nil {
		return err
	}
	defer byUser.Close()
	byAI, err := tx.PrepareContext(ctx, pick+`x.source = 'ai' AND x.score >= ?2 ORDER BY x.score DESC LIMIT 1`)
	if err != nil {
		return err
	}
	defer byAI.Close()
	for i := range tags {
		t := &tags[i]
		if t.Count == 0 {
			continue
		}
		err := byUser.QueryRowContext(ctx, t.ID).Scan(&t.Cover, &t.CoverThumb)
		if errors.Is(err, sql.ErrNoRows) {
			err = byAI.QueryRowContext(ctx, t.ID, minScore).Scan(&t.Cover, &t.CoverThumb)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

// CreateTag makes a new user tag. A label (TagLabel) has prompts the AI
// looks for; a manual tag (TagManual) has none.
func (s *Store) CreateTag(ctx context.Context, name string, kind core.TagKind, prompts []string) (int64, error) {
	name, err := cleanName(name)
	if err != nil {
		return 0, err
	}
	var p any
	switch kind {
	case core.TagLabel:
		b, err := json.Marshal(prompts)
		if err != nil {
			return 0, err
		}
		p = string(b)
	case core.TagManual:
	default:
		return 0, fmt.Errorf("create tag: only labels and manual tags are made by users, not %q", kind)
	}
	var id int64
	err = s.tx(ctx, func(tx *sql.Tx) error {
		switch taken, err := named(ctx, tx, tagNames, name, 0); {
		case err != nil:
			return err
		case taken != 0:
			return ErrTagExists
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO tags (name, kind, prompts, created_at) VALUES (?, ?, ?, ?)",
			name, kind, p, time.Now().Unix())
		if err == nil {
			id, err = res.LastInsertId()
		}
		return err
	})
	if isUnique(err) {
		return 0, ErrTagExists
	}
	return id, err
}

// MakeLabel turns a manual tag into a label with prompts, for the AI to look
// for too. The user's own tags on it stay.
func (s *Store) MakeLabel(ctx context.Context, id int64, prompts []string) error {
	b, err := json.Marshal(prompts)
	if err != nil {
		return err
	}
	return affected(s.w.ExecContext(ctx, `UPDATE tags SET kind = 'label', prompts = ?, model = NULL, vecs = NULL
		WHERE id = ? AND kind = 'manual'`, string(b), id))
}

// TagByName finds a tag by name, whatever its case. A blocked Thing's name
// is ErrBlockedName: it is neither applied nor reported as existing.
func (s *Store) TagByName(ctx context.Context, name string) (int64, core.TagKind, error) {
	var id int64
	var kind core.TagKind
	var blocked bool
	err := s.r.QueryRowContext(ctx, "SELECT id, kind, "+labelBlocked(core.BlockTag, "tags.id")+" FROM tags WHERE "+foldName+" = ?",
		core.Fold(strings.TrimSpace(name))).Scan(&id, &kind, &blocked)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, "", ErrNotFound
	case err != nil:
		return 0, "", err
	case blocked:
		return 0, "", ErrBlockedName
	}
	return id, kind, nil
}

// DeleteTag removes a tag the user made (built-ins can only be blocked).
func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var kind core.TagKind
		switch err := tx.QueryRowContext(ctx, "SELECT kind FROM tags WHERE id = ?", id).Scan(&kind); {
		case errors.Is(err, sql.ErrNoRows):
			return ErrNotFound
		case err != nil:
			return err
		case kind == core.TagBuiltin:
			return ErrBuiltinTag
		}
		var used bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM albums WHERE kind = 'smart'
		AND `+ruleTag+` = ?)`, id).Scan(&used); err != nil {
			return err
		}
		if used {
			return ErrTagInAlbum
		}
		return affected(tx.ExecContext(ctx, "DELETE FROM tags WHERE id = ?", id))
	})
}

// AssetTags returns the tags on one asset: the user's, then the AI's from
// minScore up, surest first.
func (s *Store) AssetTags(ctx context.Context, assetID int64, minScore float64) ([]core.AssetTag, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (t core.AssetTag, err error) {
		err = r.Scan(&t.ID, &t.Name, &t.Kind, &t.Source, &t.Score)
		return t, err
	}, `SELECT t.id, t.name, t.kind, x.source, COALESCE(x.score, 0)
		FROM asset_tags x JOIN tags t ON t.id = x.tag_id
		WHERE x.asset_id = ? AND EXISTS (SELECT 1 FROM assets WHERE id = x.asset_id AND `+visible+`)
		AND `+tagApplies+` AND NOT `+labelBlocked(core.BlockTag, "t.id")+`
		ORDER BY x.source = 'user' DESC, x.score DESC, t.name`, assetID, minScore)
}

// TagAsset puts a tag on an asset as the user's own choice (this also
// confirms an AI tag, or takes back an earlier removal).
func (s *Store) TagAsset(ctx context.Context, assetID, tagID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := assetAndTagExist(ctx, tx, assetID, tagID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO asset_tags (asset_id, tag_id, source) VALUES (?, ?, 'user')
			ON CONFLICT (asset_id, tag_id) DO UPDATE SET source = 'user'`, assetID, tagID)
		return err
	})
}

// assetAndTagExist returns ErrNotFound unless both exist.
func assetAndTagExist(ctx context.Context, tx *sql.Tx, assetID, tagID int64) error {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM assets WHERE id = ? AND `+present+`)
		AND EXISTS (SELECT 1 FROM tags WHERE id = ?)`, assetID, tagID).Scan(&ok)
	if err == nil && !ok {
		err = ErrNotFound
	}
	return err
}

// UntagAsset takes a tag off an asset. For tags the AI applies, the removal
// is remembered so the AI never puts it back.
func (s *Store) UntagAsset(ctx context.Context, assetID, tagID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := assetAndTagExist(ctx, tx, assetID, tagID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO asset_tags (asset_id, tag_id, source)
			SELECT ?1, ?2, 'rejected' FROM tags WHERE id = ?2 AND kind != 'manual'
			ON CONFLICT (asset_id, tag_id) DO UPDATE SET source = 'rejected'`, assetID, tagID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM asset_tags WHERE asset_id = ? AND tag_id = ?
			AND tag_id IN (SELECT id FROM tags WHERE kind = 'manual')`, assetID, tagID)
		return err
	})
}

// SearchTags returns up to limit shown tags whose names contain text (in
// any case), for the search box's suggestions, names starting with text
// first. Only their id, name and kind are set.
func (s *Store) SearchTags(ctx context.Context, text string, limit int) ([]core.Tag, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []core.Tag{}, nil
	}
	return queryAll(ctx, s.r, func(r *sql.Rows) (t core.Tag, err error) {
		err = r.Scan(&t.ID, &t.Name, &t.Kind)
		return t, err
	}, `SELECT id, name, kind FROM tags WHERE `+nameMatch+` AND hidden = 0
		AND NOT `+labelBlocked(core.BlockTag, "tags.id")+`
		ORDER BY `+nameFirst+` LIMIT ?2`, core.Fold(text), limit)
}

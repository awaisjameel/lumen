package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"lumen/internal/core"
)

var (
	// ErrTagExists means another tag already has that name.
	ErrTagExists = errors.New("a tag with that name already exists")
	// ErrBuiltinTag means the operation only applies to tags the user made.
	ErrBuiltinTag = errors.New("built-in things can be hidden, not deleted")
)

// VocabLabel is one built-in Thing, as the vocabulary defines it.
type VocabLabel struct {
	Name      string
	Prompts   []string
	Exclusive bool // a broad kind of photo (see search.Label)
	Hidden    bool // never shown (a sink kind); otherwise hiding is the user's choice
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

// tagApplies is the condition for an asset_tags row (alias x) to count: the
// user put it there, or the AI did with at least the given confidence.
const tagApplies = "(x.source = 'user' OR (x.source = 'ai' AND x.score >= ?))"

// SyncVocabulary makes the built-in tags match labels. New labels are added,
// changed ones are updated (their embeddings dropped, so they are redone and
// the library re-tagged), and built-ins no longer listed are removed, unless
// the user tagged photos with them, in which case they become the user's own.
func (s *Store) SyncVocabulary(ctx context.Context, labels []VocabLabel) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		up, err := tx.PrepareContext(ctx, `INSERT INTO tags (name, kind, prompts, exclusive, hidden, created_at)
			VALUES (?1, 'builtin', ?2, ?3, ?5, ?4)
			ON CONFLICT (name) DO UPDATE SET kind = 'builtin', prompts = ?2, exclusive = ?3,
				hidden = CASE WHEN ?5 THEN 1 ELSE hidden END,
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
			if _, err := up.ExecContext(ctx, l.Name, string(p), l.Exclusive, now, l.Hidden); err != nil {
				return err
			}
			names = append(names, l.Name)
		}
		list, err := json.Marshal(names)
		if err != nil {
			return err
		}
		stale := `kind = 'builtin' AND name NOT IN (SELECT value FROM json_each(?))`
		if _, err := tx.ExecContext(ctx, `DELETE FROM asset_tags WHERE source = 'ai' AND tag_id IN (SELECT id FROM tags WHERE `+stale+`)`, list); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tags SET kind = 'manual', prompts = NULL, exclusive = 0, model = NULL, vecs = NULL
			WHERE `+stale+` AND EXISTS (SELECT 1 FROM asset_tags x WHERE x.tag_id = tags.id AND x.source = 'user')`, list); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM tags WHERE `+stale, list)
		return err
	})
}

// AutoTags returns every tag the AI applies (built-in and user labels,
// hidden ones included: hiding only affects the Things view).
func (s *Store) AutoTags(ctx context.Context) ([]AutoTag, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id, name, kind = 'builtin', prompts, exclusive, COALESCE(model, ''), vecs
		FROM tags WHERE kind IN ('builtin', 'label') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutoTag
	for rows.Next() {
		var t AutoTag
		var prompts string
		var vecs []byte
		if err := rows.Scan(&t.ID, &t.Name, &t.Builtin, &prompts, &t.Exclusive, &t.Model, &vecs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(prompts), &t.Prompts); err != nil {
			return nil, fmt.Errorf("tag %q: prompts: %w", t.Name, err)
		}
		t.Vecs = bytesFloat32(vecs)
		out = append(out, t)
	}
	return out, rows.Err()
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
func (s *Store) Things(ctx context.Context, minScore float32) ([]core.Tag, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT t.id, t.name, t.kind, t.hidden, count(a.id)
		FROM tags t
		LEFT JOIN asset_tags x ON x.tag_id = t.id AND `+tagApplies+`
		LEFT JOIN assets a ON a.id = x.asset_id AND `+visibleAs("a")+`
		GROUP BY t.id ORDER BY count(a.id) DESC, t.name`, minScore)
	if err != nil {
		return nil, err
	}
	var out []core.Tag
	for rows.Next() {
		var t core.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Kind, &t.Hidden, &t.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Covers: the user's own choices first, then the AI's surest match.
	cover, err := s.r.PrepareContext(ctx, `SELECT a.id, a.thumb_version FROM asset_tags x JOIN assets a ON a.id = x.asset_id
		WHERE x.tag_id = ? AND `+tagApplies+` AND `+visibleAs("a")+` AND a.thumb_version > 0
		ORDER BY x.source = 'user' DESC, x.score DESC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer cover.Close()
	for i := range out {
		if out[i].Count == 0 {
			continue
		}
		err := cover.QueryRowContext(ctx, out[i].ID, minScore).Scan(&out[i].Cover, &out[i].CoverThumb)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	return out, nil
}

// CreateTag makes a new user tag. A label (TagLabel) has prompts the AI
// looks for; a manual tag (TagManual) has none.
func (s *Store) CreateTag(ctx context.Context, name string, kind core.TagKind, prompts []string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("a tag needs a name")
	}
	var p any
	if kind == core.TagLabel {
		b, err := json.Marshal(prompts)
		if err != nil {
			return 0, err
		}
		p = string(b)
	}
	res, err := s.w.ExecContext(ctx, "INSERT INTO tags (name, kind, prompts, created_at) VALUES (?, ?, ?, ?)",
		name, kind, p, time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, ErrTagExists
		}
		return 0, err
	}
	return res.LastInsertId()
}

// TagByName finds a tag by name (case-insensitive).
func (s *Store) TagByName(ctx context.Context, name string) (int64, core.TagKind, error) {
	var id int64
	var kind core.TagKind
	err := s.r.QueryRowContext(ctx, "SELECT id, kind FROM tags WHERE name = ?", strings.TrimSpace(name)).Scan(&id, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	return id, kind, err
}

// DeleteTag removes a tag the user made (built-ins can only be hidden).
func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	var kind core.TagKind
	switch err := s.r.QueryRowContext(ctx, "SELECT kind FROM tags WHERE id = ?", id).Scan(&kind); {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return err
	case kind == core.TagBuiltin:
		return ErrBuiltinTag
	}
	_, err := s.w.ExecContext(ctx, "DELETE FROM tags WHERE id = ?", id)
	return err
}

// SetTagHidden hides a tag from Things, or shows it again.
func (s *Store) SetTagHidden(ctx context.Context, id int64, hidden bool) error {
	_, err := s.w.ExecContext(ctx, "UPDATE tags SET hidden = ? WHERE id = ?", hidden, id)
	return err
}

// AssetTags returns the tags on one asset: the user's, then the AI's from
// minScore up, surest first.
func (s *Store) AssetTags(ctx context.Context, assetID int64, minScore float32) ([]core.AssetTag, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT t.id, t.name, t.kind, x.source, COALESCE(x.score, 0)
		FROM asset_tags x JOIN tags t ON t.id = x.tag_id
		WHERE x.asset_id = ? AND `+tagApplies+`
		ORDER BY x.source = 'user' DESC, x.score DESC, t.name`, assetID, minScore)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.AssetTag{}
	for rows.Next() {
		var t core.AssetTag
		if err := rows.Scan(&t.ID, &t.Name, &t.Kind, &t.Source, &t.Score); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TagAsset puts a tag on an asset as the user's own choice (this also
// confirms an AI tag, or takes back an earlier removal).
func (s *Store) TagAsset(ctx context.Context, assetID, tagID int64) error {
	_, err := s.w.ExecContext(ctx, `INSERT INTO asset_tags (asset_id, tag_id, source) VALUES (?, ?, 'user')
		ON CONFLICT (asset_id, tag_id) DO UPDATE SET source = 'user'`, assetID, tagID)
	return err
}

// UntagAsset takes a tag off an asset. For tags the AI applies, the removal
// is remembered so the AI never puts it back.
func (s *Store) UntagAsset(ctx context.Context, assetID, tagID int64) error {
	_, err := s.w.ExecContext(ctx, `INSERT INTO asset_tags (asset_id, tag_id, source)
		SELECT ?1, ?2, 'rejected' FROM tags WHERE id = ?2 AND kind != 'manual'
		ON CONFLICT (asset_id, tag_id) DO UPDATE SET source = 'rejected'`, assetID, tagID)
	if err != nil {
		return err
	}
	_, err = s.w.ExecContext(ctx, `DELETE FROM asset_tags WHERE asset_id = ? AND tag_id = ?
		AND tag_id IN (SELECT id FROM tags WHERE kind = 'manual')`, assetID, tagID)
	return err
}

// SearchTags returns up to limit tags whose names contain text, for the
// search box's suggestions, names starting with text first.
func (s *Store) SearchTags(ctx context.Context, text string, limit int) ([]core.Tag, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(text) + "%"
	rows, err := s.r.QueryContext(ctx, `SELECT id, name, kind, hidden FROM tags
		WHERE name LIKE ? ESCAPE '\' AND hidden = 0
		ORDER BY name LIKE ? ESCAPE '\' DESC, length(name), name LIMIT ?`, like, strings.TrimPrefix(like, "%"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Tag
	for rows.Next() {
		var t core.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Kind, &t.Hidden); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func float32Bytes(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func bytesFloat32(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

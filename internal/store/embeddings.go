package store

import (
	"context"
	"database/sql"
	"time"

	"lumen/internal/core"
)

// TagScore is an AI tag the model gave an asset, with its probability.
type TagScore struct {
	TagID int64
	Score float32
}

// EmbedResult is the outcome of the embedding stage for one asset.
type EmbedResult struct {
	AssetID int64
	Vec     []int8 // int8 embedding; the vector is Vec * Scale
	Scale   float32
	Tags    []TagScore // replaces the asset's AI tags
	Err     string
	Skipped bool // nothing to embed (a video, or no thumbnail can be made)
}

// SaveEmbeddings stores a batch of embeddings with their AI tags and closes
// the jobs, all in one transaction. The user's own tag edits are never
// touched: an AI tag the user removed stays removed.
func (s *Store) SaveEmbeddings(ctx context.Context, model string, rs []EmbedResult) error {
	if len(rs) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		emb, err := tx.PrepareContext(ctx, `INSERT INTO embeddings (asset_id, model, scale, vec) VALUES (?, ?, ?, ?)
			ON CONFLICT (asset_id) DO UPDATE SET model = excluded.model, scale = excluded.scale, vec = excluded.vec`)
		if err != nil {
			return err
		}
		defer emb.Close()
		tags, err := prepareAITags(ctx, tx)
		if err != nil {
			return err
		}
		defer tags.close()
		job, err := prepareJobUpdate(ctx, tx)
		if err != nil {
			return err
		}
		defer job.Close()
		for _, r := range rs {
			if r.Err == "" && !r.Skipped {
				if _, err := emb.ExecContext(ctx, r.AssetID, model, r.Scale, int8Bytes(r.Vec)); err != nil {
					return err
				}
				if err := tags.set(ctx, r.AssetID, r.Tags); err != nil {
					return err
				}
			}
			if err := execJob(ctx, job, r.AssetID, core.StageEmbedding, model, jobStatus(r.Err, r.Skipped), r.Err, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// AssetTagSet is the complete set of AI tags for one asset.
type AssetTagSet struct {
	AssetID int64
	Tags    []TagScore
}

// ReplaceAITags replaces the AI tags of many assets (re-tagging the library
// after the Things change), in transactions of a few thousand assets. User
// edits stay.
func (s *Store) ReplaceAITags(ctx context.Context, sets []AssetTagSet) error {
	const chunk = 2000
	for lo := 0; lo < len(sets); lo += chunk {
		err := s.tx(ctx, func(tx *sql.Tx) error {
			tags, err := prepareAITags(ctx, tx)
			if err != nil {
				return err
			}
			defer tags.close()
			for _, set := range sets[lo:min(lo+chunk, len(sets))] {
				if err := tags.set(ctx, set.AssetID, set.Tags); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// aiTagStmts replaces one asset's AI tags inside a transaction.
type aiTagStmts struct{ clear, add *sql.Stmt }

func prepareAITags(ctx context.Context, tx *sql.Tx) (*aiTagStmts, error) {
	clear, err := tx.PrepareContext(ctx, "DELETE FROM asset_tags WHERE asset_id = ? AND source = 'ai'")
	if err != nil {
		return nil, err
	}
	add, err := prepareAddAI(ctx, tx)
	if err != nil {
		clear.Close()
		return nil, err
	}
	return &aiTagStmts{clear: clear, add: add}, nil
}

func (a *aiTagStmts) set(ctx context.Context, assetID int64, tags []TagScore) error {
	if _, err := a.clear.ExecContext(ctx, assetID); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := a.add.ExecContext(ctx, assetID, t.TagID, t.Score); err != nil {
			return err
		}
	}
	return nil
}

func (a *aiTagStmts) close() {
	a.clear.Close()
	a.add.Close()
}

// prepareAddAI inserts an AI tag (asset, tag, score) unless the user already
// decided about that tag on that asset. A tag deleted since it was scored is
// skipped rather than failing the whole batch.
func prepareAddAI(ctx context.Context, tx *sql.Tx) (*sql.Stmt, error) {
	return tx.PrepareContext(ctx, `INSERT INTO asset_tags (asset_id, tag_id, source, score)
		SELECT ?1, id, 'ai', ?3 FROM tags WHERE id = ?2
		ON CONFLICT (asset_id, tag_id) DO NOTHING`)
}

// Embeddings calls fn for every stored embedding made by model. vec is only
// valid during the call.
func (s *Store) Embeddings(ctx context.Context, model string, fn func(id int64, vec []int8, scale float32)) error {
	rows, err := s.r.QueryContext(ctx, "SELECT asset_id, scale, vec FROM embeddings WHERE model = ?", model)
	if err != nil {
		return err
	}
	defer rows.Close()
	var raw sql.RawBytes
	var vec []int8
	for rows.Next() {
		var id int64
		var scale float32
		if err := rows.Scan(&id, &scale, &raw); err != nil {
			return err
		}
		vec = bytesInt8(vec[:0], raw)
		fn(id, vec, scale)
	}
	return rows.Err()
}

// CountAwaitingAI returns how many visible photos still wait for the AI, so a
// search can say its results are partial.
func (s *Store) CountAwaitingAI(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM asset_jobs j JOIN assets a ON a.id = j.asset_id
		WHERE j.stage = 'embedding' AND j.status = 'pending' AND `+visibleAs("a")).Scan(&n)
	return n, err
}

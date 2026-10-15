package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
	"unsafe"

	"lumen/internal/core"
)

// TagScore is an AI tag the model gave an asset, with its probability.
type TagScore struct {
	TagID int64
	Score float32
}

// EmbedResult is the outcome of the embedding stage for one asset.
type EmbedResult struct {
	LabelsGeneration uint64
	AssetID          int64
	Vec              []int8 // int8 embedding; the vector is Vec * Scale
	Scale            float32
	Tags             []TagScore       // replaces the asset's AI tags
	Frames           []FrameEmbedding // one vector per sampled video frame; nil for photos
	Err              string
	Skipped          bool // nothing to embed (no thumbnail can be made, or a video without frames)
}

// FrameEmbedding is one searchable moment of a video.
type FrameEmbedding struct {
	At    int64
	Vec   []int8
	Scale float32
}

// SaveEmbeddings stores a batch of embeddings made by model with their AI
// tags and closes the jobs at the stage's version, all in one transaction.
// The user's own tag edits are never touched: an AI tag the user removed
// stays removed.
func (s *Store) SaveEmbeddings(ctx context.Context, model, version string, rs []EmbedResult) error {
	return s.SaveEmbeddingVersions(ctx, model, StageVersion{Photo: version, Video: version}, rs)
}

// SaveEmbeddingVersions saves embeddings at their media-kind-specific stage versions.
func (s *Store) SaveEmbeddingVersions(ctx context.Context, model string, version StageVersion, rs []EmbedResult) error {
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
		delFrames, err := tx.PrepareContext(ctx, "DELETE FROM video_embeddings WHERE asset_id = ?")
		if err != nil {
			return err
		}
		defer delFrames.Close()
		addFrame, err := tx.PrepareContext(ctx, `INSERT INTO video_embeddings (asset_id, at_ms, model, scale, vec)
			VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer addFrame.Close()
		tags, err := prepareAITags(ctx, tx)
		if err != nil {
			return err
		}
		defer tags.close()
		job, err := prepareVersionedJobUpdate(ctx, tx, version)
		if err != nil {
			return err
		}
		defer job.Close()
		var tagged []int64 // photos whose tags changed: some may now be screenshots or documents
		for _, r := range rs {
			if r.Err == "" && !r.Skipped {
				if _, err := emb.ExecContext(ctx, r.AssetID, model, r.Scale, int8Bytes(r.Vec)); err != nil {
					return err
				}
				if _, err := delFrames.ExecContext(ctx, r.AssetID); err != nil {
					return err
				}
				for _, frame := range r.Frames {
					if _, err := addFrame.ExecContext(ctx, r.AssetID, frame.At, model, frame.Scale, int8Bytes(frame.Vec)); err != nil {
						return err
					}
				}
				if err := tags.set(ctx, r.AssetID, r.Tags); err != nil {
					return err
				}
				tagged = append(tagged, r.AssetID)
			}
			if err := execJob(ctx, job, r.AssetID, core.StageEmbedding, version.Photo, jobStatus(r.Err, r.Skipped), r.Err, now); err != nil {
				return err
			}
		}
		// After the jobs are closed, so a new OCR job is not held back by one.
		return queueOCR(ctx, tx, tagged)
	})
}

// VideoEmbeddings visits the persisted frame vectors for the current model.
// vec is reused between calls: fn must copy what it keeps.
func (s *Store) VideoEmbeddings(ctx context.Context, model string, fn func(assetID, at int64, vec []int8, scale float32)) error {
	rows, err := s.r.QueryContext(ctx, `SELECT asset_id, at_ms, scale, vec FROM video_embeddings
		WHERE model = ? ORDER BY asset_id, at_ms`, model)
	if err != nil {
		return err
	}
	defer rows.Close()
	var raw sql.RawBytes
	var vec []int8
	for rows.Next() {
		var id, at int64
		var scale float32
		if err := rows.Scan(&id, &at, &scale, &raw); err != nil {
			return err
		}
		vec = bytesInt8(vec[:0], raw)
		fn(id, at, vec, scale)
	}
	return rows.Err()
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
			chunkIDs := make([]int64, 0, chunk)
			for _, set := range sets[lo:min(lo+chunk, len(sets))] {
				if err := tags.set(ctx, set.AssetID, set.Tags); err != nil {
					return err
				}
				chunkIDs = append(chunkIDs, set.AssetID)
			}
			return queueOCR(ctx, tx, chunkIDs)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// aiTagStmts replaces one asset's AI tags inside a transaction.
type aiTagStmts struct{ del, add *sql.Stmt }

func prepareAITags(ctx context.Context, tx *sql.Tx) (*aiTagStmts, error) {
	del, err := tx.PrepareContext(ctx, "DELETE FROM asset_tags WHERE asset_id = ? AND source = 'ai'")
	if err != nil {
		return nil, err
	}
	add, err := prepareAddAI(ctx, tx) // on failure the transaction ends, closing del
	if err != nil {
		return nil, err
	}
	return &aiTagStmts{del: del, add: add}, nil
}

func (a *aiTagStmts) set(ctx context.Context, assetID int64, tags []TagScore) error {
	if _, err := a.del.ExecContext(ctx, assetID); err != nil {
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
	_ = a.del.Close()
	_ = a.add.Close()
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
	return s.LoadEmbeddings(ctx, model, nil, fn)
}

// LoadEmbeddings calls prepare with the matching count before visiting vectors.
// Both reads share a snapshot, so indexing during reservation cannot change it.
// A nil prepare skips the count. vec is only valid during fn.
func (s *Store) LoadEmbeddings(ctx context.Context, model string, prepare func(int), fn func(id int64, vec []int8, scale float32)) error {
	return s.read(ctx, func(tx *sql.Tx) error {
		if prepare != nil {
			var n int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM embeddings WHERE model = ?", model).Scan(&n); err != nil {
				return err
			}
			prepare(n)
		}
		return visitEmbeddings(ctx, tx, model, fn)
	})
}

func visitEmbeddings(ctx context.Context, tx *sql.Tx, model string, fn func(id int64, vec []int8, scale float32)) error {
	rows, err := tx.QueryContext(ctx, "SELECT asset_id, scale, vec FROM embeddings WHERE model = ?", model)
	if err != nil {
		return err
	}
	defer rows.Close()
	var raw sql.RawBytes
	var id int64
	var scale float64 // Scanning the driver's float64 avoids a string round trip for each float32.
	for rows.Next() {
		if err := rows.Scan(&id, &scale, &raw); err != nil {
			return err
		}
		if math.Abs(scale) > math.MaxFloat32 {
			return fmt.Errorf("embedding of asset %d: scale %g overflows float32", id, scale)
		}
		vec := unsafe.Slice((*int8)(unsafe.Pointer(unsafe.SliceData(raw))), len(raw)) // Byte and int8 layouts match, and fn only borrows the row.
		fn(id, vec, float32(scale))
	}
	return rows.Err()
}

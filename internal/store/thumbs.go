package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"lumen/internal/core"
)

// ThumbCacheMB is the thumbnail cache's size cap in MB. A stored value out of
// range (edited by hand) reads as the default, as Settings shows it.
func (s *Store) ThumbCacheMB(ctx context.Context) (int, error) {
	mb, err := s.Int(ctx, core.SettingThumbCacheMB, core.ThumbCacheDefaultMB)
	if mb < core.ThumbCacheMinMB || mb > core.ThumbCacheMaxMB {
		mb = core.ThumbCacheDefaultMB
	}
	return mb, err
}

// SetThumbCacheMB stores the thumbnail cache's size cap, held to the allowed
// range, and returns the value stored.
func (s *Store) SetThumbCacheMB(ctx context.Context, mb int) (int, error) {
	mb = min(max(mb, core.ThumbCacheMinMB), core.ThumbCacheMaxMB)
	return mb, s.SetInt(ctx, core.SettingThumbCacheMB, mb)
}

// CPUThreads is how many CPU threads indexing may keep busy: the user's
// choice, held to what this machine has (a library moved from a bigger one
// keeps working), or core.DefaultCPUThreads if they made none.
func (s *Store) CPUThreads(ctx context.Context) (int, error) {
	n, err := s.Int(ctx, core.SettingCPUThreads, 0)
	if n < 1 {
		return core.DefaultCPUThreads(), err
	}
	return min(n, core.MaxCPUThreads()), err
}

// SetCPUThreads stores how many CPU threads indexing may keep busy, held to
// 1..core.MaxCPUThreads, and returns the value stored.
func (s *Store) SetCPUThreads(ctx context.Context, n int) (int, error) {
	n = min(max(n, 1), core.MaxCPUThreads())
	return n, s.SetInt(ctx, core.SettingCPUThreads, n)
}

// TouchThumbs records when assets' thumbnails were last shown (asset id to
// unix seconds), in one statement. A later time wins over an earlier one, and
// an asset deleted since it was shown is left out, so the batch never fails
// on it.
func (s *Store) TouchThumbs(ctx context.Context, views map[int64]int64) error {
	if len(views) == 0 {
		return nil
	}
	pairs := make([][2]int64, 0, len(views))
	for id, at := range views {
		pairs = append(pairs, [2]int64{id, at})
	}
	list, err := json.Marshal(pairs)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		// WHERE 1: SQLite reads ON CONFLICT after a join as part of the join otherwise.
		_, err := tx.ExecContext(ctx, `INSERT INTO thumb_views (asset_id, viewed_at)
			SELECT a.id, json_extract(e.value, '$[1]') FROM json_each(?) e
			JOIN assets a ON a.id = json_extract(e.value, '$[0]') WHERE 1
			ON CONFLICT (asset_id) DO UPDATE SET viewed_at = max(viewed_at, excluded.viewed_at)`, string(list))
		return err
	})
}

// thumbJobsUnfinished holds back an asset while a stage that reads its
// thumbnail has not run: the image model, the face finder and the text
// reader all take the 1024 px picture from the cache.
const thumbJobsUnfinished = `NOT EXISTS (SELECT 1 FROM asset_jobs j WHERE j.asset_id = a.id
	AND j.stage IN ('` + string(core.StageEmbedding) + `', '` + string(core.StageFaces) + `', '` + string(core.StageOCR) + `')
	AND j.status IN ` + unfinished + `)`

// The two halves of the eviction order. A thumbnail nobody has looked at goes
// first, oldest asset first: nothing says it matters. Then come the viewed
// ones, longest unseen first. Each runs on its own partial index, so a page
// of candidates costs a walk of the index to its first rows, not a sort of
// the library.
const (
	neverViewedQuery = `SELECT a.id FROM assets a INDEXED BY assets_thumb_added
		WHERE a.thumb_version > 0 AND ` + thumbJobsUnfinished + `
			AND NOT EXISTS (SELECT 1 FROM thumb_views v WHERE v.asset_id = a.id)
		ORDER BY a.added_at, a.id LIMIT ?`
	viewedQuery = `SELECT v.asset_id FROM thumb_views v INDEXED BY thumb_views_oldest
		JOIN assets a ON a.id = v.asset_id
		WHERE v.viewed_at > 0 AND a.thumb_version > 0 AND ` + thumbJobsUnfinished + `
		ORDER BY v.viewed_at, v.asset_id LIMIT ?`
)

// ThumbEvictionCandidates lists up to limit assets whose thumbnails the cache
// may drop, best first: never shown, oldest first, then shown, least recently
// first. It leaves out assets without a thumbnail, those already evicted, and
// those with an embedding, faces or text job still to run.
func (s *Store) ThumbEvictionCandidates(ctx context.Context, limit int) ([]int64, error) {
	var ids []int64
	err := s.read(ctx, func(tx *sql.Tx) error {
		var err error
		if ids, err = queryAll(ctx, tx, scanID, neverViewedQuery, limit); err != nil || len(ids) >= limit {
			return err
		}
		more, err := queryAll(ctx, tx, scanID, viewedQuery, limit-len(ids))
		ids = append(ids, more...)
		return err
	})
	return ids, err
}

// MarkThumbsEvicted records that assets' thumbnails were removed, so they stop
// coming up as candidates until shown or rebuilt again. A view at or after
// before (the time the eviction began) is kept: that thumbnail was rebuilt
// meanwhile. It returns how many rows it wrote.
func (s *Store) MarkThumbsEvicted(ctx context.Context, ids []int64, before int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO thumb_views (asset_id, viewed_at)
			SELECT a.id, 0 FROM json_each(?1) e JOIN assets a ON a.id = e.value WHERE 1
			ON CONFLICT (asset_id) DO UPDATE SET viewed_at = 0 WHERE viewed_at < ?2`, string(list), before)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n, err
}

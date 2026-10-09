package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"lumen/internal/core"
)

// SaveCompressionPlan persists the exact source copies and destination names reviewed.
func (s *Store) SaveCompressionPlan(ctx context.Context, options core.CompressionOptions, jobs []core.CompressionJob) (int64, error) {
	var id int64
	data, err := json.Marshal(options)
	if err != nil {
		return 0, err
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "INSERT INTO compression_runs(created_at, options) VALUES (?, ?)", time.Now().Unix(), string(data))
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO compression_jobs(run_id, asset_id, from_path, to_path, copies, size, mtime, state, reason, restore_digest, restore_size) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, j := range jobs {
			if _, err := stmt.ExecContext(ctx, id, j.AssetID, j.From, j.To, j.Copies, j.Size, j.MTime, j.State, j.Reason, j.RestoreDigest, j.RestoreSize); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// CompressionJobs reads an internal journal, including owned temporary paths.
func (s *Store) CompressionJobs(ctx context.Context, run int64) ([]core.CompressionJob, error) {
	return s.compressionJobs(ctx, run, false)
}

// CompressionRecoveryJobs loads only unfinished checkpoints and pending cleanup.
func (s *Store) CompressionRecoveryJobs(ctx context.Context) ([]core.CompressionJob, error) {
	return queryAll(ctx, s.r, readCompressionJob, "SELECT "+compressionColumns+" FROM compression_jobs j WHERE j.state IN ('encoding', 'verifying', 'committing') OR (j.state = 'done' AND j.temp != '') ORDER BY j.id")
}

// CompressionJob reads one checkpoint without loading the rest of a bulk run.
func (s *Store) CompressionJob(ctx context.Context, id int64) (core.CompressionJob, error) {
	jobs, err := queryAll(ctx, s.r, readCompressionJob, "SELECT "+compressionColumns+" FROM compression_jobs j WHERE j.id = ?", id)
	if err != nil {
		return core.CompressionJob{}, err
	}
	if len(jobs) == 0 {
		return core.CompressionJob{}, ErrNotFound
	}
	return jobs[0], nil
}

// VisibleCompressionJobs omits covered sources and destinations from public history.
func (s *Store) VisibleCompressionJobs(ctx context.Context, run int64) ([]core.CompressionJob, error) {
	jobs, err := s.compressionJobs(ctx, run, true)
	if err != nil {
		return nil, err
	}
	visibleJobs := make([]core.CompressionJob, 0, len(jobs))
	for _, j := range jobs {
		j.Temp, j.PublishTemp, j.SidecarDigest = "", "", ""
		visibleJobs = append(visibleJobs, j)
	}
	return visibleJobs, nil
}

// compressionVisible extends the canonical asset predicate to both sides of
// a related output and its destination folder. Folder paths are normalised
// before storage, so this is core.IsWithin expressed with the same fold key.
var compressionVisible = `j.asset_id IN (SELECT id FROM assets WHERE ` + visible + `)
	AND (j.output_asset_id IS NULL OR j.output_asset_id IN (SELECT id FROM assets WHERE ` + visible + `))
	AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.kind = 'folder' AND (
	` + foldKey("j.to_path") + ` = ` + foldKey("b.path") + ` OR substr(` + foldKey("j.to_path") + `, 1, length(rtrim(` + foldKey("b.path") + `, char(92)))+1) = rtrim(` + foldKey("b.path") + `, char(92))||char(92)))`

const compressionColumns = `j.id, j.run_id, j.asset_id, j.from_path, j.to_path, j.copies, j.size, j.mtime, j.digest, j.output_size, j.output_digest, j.state, j.reason, j.verification, j.tool, j.temp, j.publish_temp, j.sidecar_digest, j.sidecar_size, j.restore_digest, j.restore_size, COALESCE(j.output_asset_id, 0)`

// CompressionRestoreAllowed permits an external output only through its
// verified, visible publication journal; ordinary path membership still wins.
func (s *Store) CompressionRestoreAllowed(ctx context.Context, job core.CompressionJob) error {
	if job.RestoreDigest == "" {
		return ErrNotFound
	}
	var allowed bool
	err := s.r.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM compression_jobs j JOIN compression_runs r ON r.id = j.run_id
		WHERE json_extract(r.options, '$.mode') = 'reversible' AND j.state = 'done' AND j.to_path = ? AND j.output_digest = ?
		AND j.digest = ? AND j.size = ? AND COALESCE(j.output_asset_id, j.asset_id) = ? AND `+compressionVisible+`)`, job.From, job.Digest, job.RestoreDigest, job.RestoreSize, job.AssetID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrNotFound
	}
	return nil
}

func readCompressionJob(r *sql.Rows) (j core.CompressionJob, err error) {
	err = r.Scan(&j.ID, &j.RunID, &j.AssetID, &j.From, &j.To, &j.Copies, &j.Size, &j.MTime, &j.Digest, &j.OutputSize, &j.OutputDigest, &j.State, &j.Reason, &j.Verification, &j.Tool, &j.Temp, &j.PublishTemp, &j.SidecarDigest, &j.SidecarSize, &j.RestoreDigest, &j.RestoreSize, &j.OutputAssetID)
	return j, err
}

// CompressionPage returns at most 100 visible outcomes from a consistent snapshot.
func (s *Store) CompressionPage(ctx context.Context, runID int64, offset int) (core.CompressionPage, error) {
	page := core.CompressionPage{Offset: max(offset, 0)}
	err := s.read(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM compression_jobs j WHERE j.run_id = ? AND "+compressionVisible, runID).Scan(&page.Total); err != nil {
			return err
		}
		jobs, err := queryAll(ctx, tx, readCompressionJob, "SELECT "+compressionColumns+" FROM compression_jobs j WHERE j.run_id = ? AND "+compressionVisible+" ORDER BY j.id LIMIT 100 OFFSET ?", runID, page.Offset)
		if err != nil {
			return err
		}
		for i := range jobs {
			jobs[i].Temp, jobs[i].PublishTemp, jobs[i].SidecarDigest = "", "", ""
		}
		page.Jobs = jobs
		return nil
	})
	return page, err
}

func (s *Store) compressionJobs(ctx context.Context, run int64, visibleOnly bool) ([]core.CompressionJob, error) {
	where := ""
	if visibleOnly {
		where = " AND " + compressionVisible
	}
	return queryAll(ctx, s.r, readCompressionJob, "SELECT "+compressionColumns+" FROM compression_jobs j WHERE (? = 0 OR j.run_id = ?)"+where+" ORDER BY j.id", run, run)
}

// SetCompressionJob checkpoints generation or publication without changing its plan.
func (s *Store) SetCompressionJob(ctx context.Context, j core.CompressionJob) error {
	return affected(s.w.ExecContext(ctx, `UPDATE compression_jobs SET digest = ?, output_size = ?, output_digest = ?, state = ?, reason = ?, verification = ?, tool = ?, temp = ?, publish_temp = ?, sidecar_digest = ?, sidecar_size = ? WHERE id = ?`, j.Digest, j.OutputSize, j.OutputDigest, j.State, j.Reason, j.Verification, j.Tool, j.Temp, j.PublishTemp, j.SidecarDigest, j.SidecarSize, j.ID))
}

// SetCompressionRun records the durable state independently of the active worker.
func (s *Store) SetCompressionRun(ctx context.Context, id int64, state core.CompressionState) error {
	return affected(s.w.ExecContext(ctx, "UPDATE compression_runs SET state = ? WHERE id = ?", state, id))
}

// CompressionRun reads one run's options, including during recovery.
func (s *Store) CompressionRun(ctx context.Context, id int64) (core.CompressionRun, error) {
	var r core.CompressionRun
	var data string
	err := s.r.QueryRowContext(ctx, "SELECT id, created_at, options, state FROM compression_runs WHERE id = ?", id).Scan(&r.ID, &r.CreatedAt, &data, &r.State)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(data), &r.Options)
	return r, err
}

// CompressionRuns shows bounded recent history with counts of visible outcomes only.
func (s *Store) CompressionRuns(ctx context.Context) ([]core.CompressionRun, error) {
	return queryAll(ctx, s.r, func(rows *sql.Rows) (r core.CompressionRun, err error) {
		var data string
		err = rows.Scan(&r.ID, &r.CreatedAt, &data, &r.State, &r.Total, &r.Ready, &r.Done, &r.Skipped, &r.Failed, &r.Cancelled, &r.SavedBytes, &r.AddedBytes)
		if err == nil {
			err = json.Unmarshal([]byte(data), &r.Options)
		}
		r.Processed = r.Ready + r.Done + r.Skipped + r.Failed + r.Cancelled
		return r, err
	}, `SELECT r.id, r.created_at, r.options, r.state, count(*),
		sum(j.state = 'ready'), sum(j.state = 'done'), sum(j.state = 'skipped'), sum(j.state = 'failed'), sum(j.state = 'cancelled'),
		sum(CASE WHEN j.state = 'done' THEN max(j.size-j.output_size, 0) ELSE 0 END),
		sum(CASE WHEN j.state = 'done' THEN j.output_size+j.sidecar_size ELSE 0 END)
		FROM compression_runs r JOIN compression_jobs j ON j.run_id = r.id WHERE `+compressionVisible+` GROUP BY r.id ORDER BY r.id DESC LIMIT 100`)
}

// FinishCompression registers a related output and transfers user annotations
// once, in the same transaction that settles the journal. Exact source copies
// keep their asset; the new bytes receive their own content identity.
func (s *Store) FinishCompression(ctx context.Context, j core.CompressionJob, hash []byte, mtime int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var state core.CompressionState
		if err := tx.QueryRowContext(ctx, "SELECT state FROM compression_jobs WHERE id = ?", j.ID).Scan(&state); err != nil {
			return err
		}
		if state == core.CompressionDone {
			return nil
		}
		if state != core.CompressionCommitting {
			return ErrOrganiseState
		}
		sources, err := queryAll(ctx, tx, func(r *sql.Rows) (source core.Source, err error) {
			err = r.Scan(&source.ID, &source.Path)
			return source, err
		}, "SELECT id, path FROM sources WHERE enabled = 1")
		if err != nil {
			return err
		}
		var sourceID int64
		for _, source := range sources {
			if core.IsWithin(j.To, source.Path) {
				sourceID = source.ID
				break
			}
		}
		var output int64
		if sourceID != 0 {
			var existingHash []byte
			err := tx.QueryRowContext(ctx, "SELECT a.content_hash FROM files f JOIN assets a ON a.id = f.asset_id WHERE "+foldKey("f.path")+" = "+foldKey("?1"), j.To).Scan(&existingHash)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && !slices.Equal(existingHash, hash) {
				return ErrOrganiseState
			}
			err = tx.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", hash).Scan(&output)
			if errors.Is(err, sql.ErrNoRows) {
				res, err := tx.ExecContext(ctx, `INSERT INTO assets(content_hash, kind, ext, taken_at, date_from, added_at, favourite, display_rotation, display_flip, width, height, orientation, lat, lon, camera, place_id, place_checked)
					SELECT ?, kind, ?, taken_at, date_from, ?, favourite, display_rotation, display_flip, width, height, orientation, lat, lon, camera, place_id, place_checked FROM assets WHERE id = ?`, hash, filepath.Ext(j.To), time.Now().Unix(), j.AssetID)
				if err != nil {
					return err
				}
				output, err = res.LastInsertId()
				if err != nil {
					return err
				}
				if err := transferCompressionAnnotations(ctx, tx, j.AssetID, output); err != nil {
					return err
				}
				for _, stage := range core.Stages {
					status := core.JobPending
					if len(waitsFor[stage]) > 0 {
						status = core.JobWaiting
					}
					if _, err := tx.ExecContext(ctx, `INSERT INTO asset_jobs(asset_id, stage, status, updated_at, taken_at, kind) SELECT id, ?, ?, ?, taken_at, kind FROM assets WHERE id = ?`, stage, status, time.Now().Unix(), output); err != nil {
						return err
					}
				}
			} else if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO files(asset_id, source_id, path, size, mtime) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING", output, sourceID, j.To, j.OutputSize, mtime); err != nil {
				return err
			}
		}
		return affected(tx.ExecContext(ctx, "UPDATE compression_jobs SET state = ?, publish_temp = '', output_asset_id = NULLIF(?, 0) WHERE id = ?", core.CompressionDone, output, j.ID))
	})
}

func transferCompressionAnnotations(ctx context.Context, tx *sql.Tx, from, to int64) error {
	for _, query := range []string{
		"INSERT INTO asset_tags(asset_id, tag_id, source, score) SELECT ?1, tag_id, source, score FROM asset_tags WHERE asset_id = ?2",
		"INSERT INTO album_items(album_id, asset_id, added_at) SELECT album_id, ?1, added_at FROM album_items WHERE asset_id = ?2",
		"INSERT INTO event_members(tag_id, asset_id, moved) SELECT tag_id, ?1, 0 FROM event_members WHERE asset_id = ?2",
	} {
		if _, err := tx.ExecContext(ctx, query, to, from); err != nil {
			return err
		}
	}
	// Keep known people visible to blocking while the copy awaits analysis.
	// The faces stage rebuilds automatic detections and retains user corrections.
	faces, err := queryAll(ctx, tx, func(r *sql.Rows) (id int64, err error) { err = r.Scan(&id); return id, err }, "SELECT id FROM faces WHERE asset_id = ?", from)
	if err != nil {
		return err
	}
	for _, id := range faces {
		res, err := tx.ExecContext(ctx, `INSERT INTO faces(asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id, assigned_by, at_ms) SELECT ?, x, y, w, h, score, sharpness, yaw, model, vec, person_id, assigned_by, at_ms FROM faces WHERE id = ?`, to, id)
		if err != nil {
			return err
		}
		copyID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO face_rejections(face_id, person_id) SELECT ?, person_id FROM face_rejections WHERE face_id = ?", copyID, id); err != nil {
			return err
		}
		// Look-alike rules cover new photos too. Keep their known matches while
		// analysis is pending; single-face rules retain their original scope.
		if _, err := tx.ExecContext(ctx, `INSERT INTO face_block_faces(face_block_id, face_id, asset_id)
			SELECT m.face_block_id, ?1, ?2 FROM face_block_faces m JOIN face_blocks b ON b.id = m.face_block_id
			WHERE m.face_id = ?3 AND b.lookalikes = 1`, copyID, to, id); err != nil {
			return err
		}
	}
	return nil
}

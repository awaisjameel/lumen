package store

import (
	"context"
	"database/sql"
	"time"

	"lumen/internal/core"
)

// JobItem is one asset waiting for a stage, with what a worker needs to run it.
type JobItem struct {
	AssetID     int64
	Path        string
	Kind        core.Kind
	Ext         string
	Orientation int
}

// TimeRange limits a job query to assets taken in [From, To) (wall-clock seconds).
type TimeRange struct{ From, To int64 }

// PendingJobs returns up to limit assets waiting for stage, newest first. With
// a range, only assets inside it are returned; the indexer uses that to do
// whatever the user is looking at first. Thumbnails wait for metadata, which
// supplies the orientation.
func (s *Store) PendingJobs(ctx context.Context, stage core.Stage, within *TimeRange, limit int) ([]JobItem, error) {
	q := `SELECT a.id, f.path, a.kind, a.ext, a.orientation
		FROM asset_jobs j
		JOIN assets a ON a.id = j.asset_id
		JOIN files f ON f.id = (SELECT id FROM files WHERE asset_id = a.id LIMIT 1)
		WHERE j.stage = ? AND j.status = 'pending' AND a.missing_since IS NULL`
	args := []any{stage}
	if stage == core.StageThumbnail {
		q += ` AND NOT EXISTS (SELECT 1 FROM asset_jobs m
			WHERE m.asset_id = a.id AND m.stage = 'metadata' AND m.status = 'pending')`
	}
	if within != nil {
		q += " AND a.taken_at >= ? AND a.taken_at < ?"
		args = append(args, within.From, within.To)
	}
	q += " ORDER BY a.taken_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobItem
	for rows.Next() {
		var j JobItem
		if err := rows.Scan(&j.AssetID, &j.Path, &j.Kind, &j.Ext, &j.Orientation); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MetadataResult is the outcome of the metadata stage for one asset.
type MetadataResult struct {
	AssetID     int64
	TakenAt     int64 // 0 keeps the scanner's guess
	Width       int
	Height      int
	Orientation int
	Camera      string
	Lat, Lon    *float64
	Err         string
}

// ThumbResult is the outcome of the thumbnail stage for one asset.
type ThumbResult struct {
	AssetID int64
	PHash   *uint64
	Err     string
	Skipped bool // the format isn't supported by this build yet
}

// Versions recorded with each finished job. Bump one when its stage's output
// would change (a better decoder, a new model); RequeueOutdated then re-runs
// just that stage.
const (
	MetadataVersion  = "imagemeta-1.1+goexif"
	ThumbnailVersion = "go-image-1"
)

// StageVersions maps each stage to its current version.
var StageVersions = map[core.Stage]string{
	core.StageMetadata:  MetadataVersion,
	core.StageThumbnail: ThumbnailVersion,
}

// RequeueOutdated puts back in the queue every finished job whose stage has
// a newer version (a better decoder, an upgraded model). Other stages and
// all user edits are untouched. It returns how many jobs were requeued.
func (s *Store) RequeueOutdated(ctx context.Context) (int64, error) {
	var total int64
	for stage, version := range StageVersions {
		res, err := s.w.ExecContext(ctx, `UPDATE asset_jobs SET status = 'pending', error = NULL, updated_at = ?
			WHERE stage = ? AND status != 'pending' AND COALESCE(model_version, '') != ?`,
			time.Now().Unix(), stage, version)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

// SaveMetadata stores a batch of metadata results and closes their jobs.
func (s *Store) SaveMetadata(ctx context.Context, rs []MetadataResult) error {
	if len(rs) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		upd, err := tx.PrepareContext(ctx, `UPDATE assets SET
				taken_at = CASE WHEN ?1 > 0 THEN ?1 ELSE taken_at END,
				date_from = CASE WHEN ?1 > 0 THEN 'exif' ELSE date_from END,
				width = NULLIF(?2, 0), height = NULLIF(?3, 0), orientation = ?4,
				camera = NULLIF(?5, ''), lat = ?6, lon = ?7
			WHERE id = ?8`)
		if err != nil {
			return err
		}
		defer upd.Close()
		job, err := prepareJobUpdate(ctx, tx)
		if err != nil {
			return err
		}
		defer job.Close()
		for _, r := range rs {
			if r.Err == "" {
				if _, err := upd.ExecContext(ctx, r.TakenAt, r.Width, r.Height, max(r.Orientation, 1),
					r.Camera, r.Lat, r.Lon, r.AssetID); err != nil {
					return err
				}
			}
			if err := execJob(ctx, job, r.AssetID, core.StageMetadata, MetadataVersion, jobStatus(r.Err), r.Err, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveThumbnails records a batch of finished thumbnails and closes their jobs.
func (s *Store) SaveThumbnails(ctx context.Context, rs []ThumbResult) error {
	if len(rs) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		upd, err := tx.PrepareContext(ctx, "UPDATE assets SET phash = ?, thumb_version = thumb_version + 1 WHERE id = ?")
		if err != nil {
			return err
		}
		defer upd.Close()
		job, err := prepareJobUpdate(ctx, tx)
		if err != nil {
			return err
		}
		defer job.Close()
		for _, r := range rs {
			if r.Err == "" && !r.Skipped {
				var ph any
				if r.PHash != nil {
					ph = int64(*r.PHash) // SQLite integers are signed; the bits are what matter
				}
				if _, err := upd.ExecContext(ctx, ph, r.AssetID); err != nil {
					return err
				}
			}
			status := jobStatus(r.Err)
			if r.Skipped {
				status = core.JobSkipped
			}
			if err := execJob(ctx, job, r.AssetID, core.StageThumbnail, ThumbnailVersion, status, r.Err, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func prepareJobUpdate(ctx context.Context, tx *sql.Tx) (*sql.Stmt, error) {
	return tx.PrepareContext(ctx, `UPDATE asset_jobs SET status = ?, model_version = ?, error = NULLIF(?, ''), updated_at = ?
		WHERE asset_id = ? AND stage = ?`)
}

func jobStatus(errMsg string) core.JobStatus {
	if errMsg != "" {
		return core.JobError
	}
	return core.JobDone
}

func execJob(ctx context.Context, st *sql.Stmt, assetID int64, stage core.Stage, version string,
	status core.JobStatus, errMsg string, now int64) error {
	_, err := st.ExecContext(ctx, status, version, errMsg, now, assetID, stage)
	return err
}

// JobCounts returns how many jobs are waiting per stage, and how many failed
// or were skipped. It reads only unfinished jobs (the asset_jobs_status index).
func (s *Store) JobCounts(ctx context.Context) (pending map[core.Stage]int, failed, skipped int, err error) {
	rows, err := s.r.QueryContext(ctx, `SELECT stage, status, count(*) FROM asset_jobs
		WHERE status IN ('pending', 'error', 'skipped') GROUP BY stage, status`)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	pending = map[core.Stage]int{}
	for rows.Next() {
		var stage core.Stage
		var status core.JobStatus
		var n int
		if err := rows.Scan(&stage, &status, &n); err != nil {
			return nil, 0, 0, err
		}
		switch status {
		case core.JobPending:
			pending[stage] = n
		case core.JobSkipped:
			skipped += n
		case core.JobError:
			failed += n
		}
	}
	return pending, failed, skipped, rows.Err()
}

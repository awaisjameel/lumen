package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
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
	Duration    int64
}

// TimeRange limits a job query to assets taken in [From, To) (wall-clock seconds).
type TimeRange struct{ From, To int64 }

// waitsFor lists the stages each stage needs finished first: thumbnails
// need the orientation from metadata, the image model reads the thumbnail,
// and faces need both (the orientation to decode upright, the thumbnail to be
// shown cut out of). Every earlier stage is listed, not just the previous
// one, so a re-run of metadata holds back everything after it.
var waitsFor = map[core.Stage][]core.Stage{
	core.StageThumbnail: {core.StageMetadata},
	core.StageEmbedding: {core.StageMetadata, core.StageThumbnail},
	core.StageFaces:     {core.StageMetadata, core.StageThumbnail},
}

// afterMetadata are the stages whose results depend on the orientation
// metadata reads, so they re-run when it changes.
var afterMetadata = []core.Stage{core.StageThumbnail, core.StageEmbedding, core.StageFaces}

// MaxAttempts is how many times in a row a job may fail before it is left
// failed until its stage's version changes.
const MaxAttempts = 3

// primaryFile selects the file a job or the media routes read for asset a:
// the first one registered, so every caller agrees on it.
const primaryFile = "(SELECT min(id) FROM files WHERE asset_id = a.id)"

// PendingJobs returns up to limit assets waiting for stage, newest first. With
// a range, only assets inside it are returned; the indexer uses that to do
// whatever the user is looking at first. An asset with an earlier stage still
// pending is left for later. The asset_jobs_pending index gives the order,
// whatever statistics the planner has: sorting the whole backlog for every
// batch cost a third of a second at 300,000 photos.
func (s *Store) PendingJobs(ctx context.Context, stage core.Stage, within *TimeRange, limit int) ([]JobItem, error) {
	q, args := pendingJobsQuery(stage, within, limit)
	return queryAll(ctx, s.r, func(r *sql.Rows) (j JobItem, err error) {
		err = r.Scan(&j.AssetID, &j.Path, &j.Kind, &j.Ext, &j.Orientation, &j.Duration)
		return j, err
	}, q, args...)
}

func pendingJobsQuery(stage core.Stage, within *TimeRange, limit int) (q string, args []any) {
	q = `SELECT a.id, f.path, a.kind, a.ext, a.orientation, COALESCE(a.duration_ms, 0)
		FROM asset_jobs j INDEXED BY asset_jobs_pending
		JOIN assets a ON a.id = j.asset_id
		JOIN files f ON f.id = ` + primaryFile + `
		WHERE j.stage = ? AND j.status = 'pending' AND a.` + present
	args = []any{stage}
	if before := waitsFor[stage]; len(before) > 0 {
		q += ` AND NOT EXISTS (SELECT 1 FROM asset_jobs p
			WHERE p.asset_id = j.asset_id AND p.status = 'pending' AND p.stage IN (` + placeholders(len(before)) + `))`
		for _, st := range before {
			args = append(args, st)
		}
	}
	if within != nil {
		q += " AND j.taken_at >= ? AND j.taken_at < ?"
		args = append(args, within.From, within.To)
	}
	q += " ORDER BY j.taken_at DESC, j.asset_id DESC LIMIT ?"
	return q, append(args, limit)
}

// placeholders returns n comma-separated SQL parameters.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// RetryFailed puts failed jobs of a source's assets back in the queue, for
// jobs that have not yet failed MaxAttempts times: a scan just found their
// files readable, so a passing cause (an unplugged drive, a locked file) may
// be gone. It returns how many jobs were requeued.
func (s *Store) RetryFailed(ctx context.Context, sourceID int64) (int64, error) {
	res, err := s.w.ExecContext(ctx, `UPDATE asset_jobs SET status = 'pending', error = NULL, updated_at = ?
		WHERE status = 'error' AND attempts < ?
		AND asset_id IN (SELECT asset_id FROM files WHERE source_id = ?)`, time.Now().Unix(), MaxAttempts, sourceID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MetadataResult is the outcome of the metadata stage for one asset.
type MetadataResult struct {
	AssetID     int64
	TakenAt     int64 // 0 keeps the scanner's guess
	Modified    int64 // when the file says it was last saved: replaces an older-looking guess from the file's time; 0 = unknown
	Width       int
	Height      int
	Orientation int
	Camera      string
	Lat, Lon    *float64
	Duration    int64
	VideoCodec  string
	AudioCodec  string
	Err         string
	Skipped     bool
}

// ThumbResult is the outcome of the thumbnail stage for one asset.
type ThumbResult struct {
	AssetID int64
	PHash   *uint64
	Err     string
	Skipped bool // the format isn't supported by this build yet
}

// RequeueOutdated puts back in the queue every finished job whose stage now
// has a different version (a better decoder, an upgraded model). Every job
// records the version that ran it, so only that stage re-runs; other stages
// and all user edits are untouched. It returns how many jobs were requeued.
func (s *Store) RequeueOutdated(ctx context.Context, versions map[core.Stage]string) (int64, error) {
	var total int64
	for stage, version := range versions {
		res, err := s.w.ExecContext(ctx, `UPDATE asset_jobs SET status = 'pending', error = NULL, attempts = 0, updated_at = ?
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

// SaveMetadata stores a batch of metadata results and closes their jobs,
// recording the version that produced them.
func (s *Store) SaveMetadata(ctx context.Context, version string, rs []MetadataResult) error {
	if len(rs) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		// Without a capture date, the time the file says it was saved beats
		// the file system's when it is older (copies are newer), but never a
		// date in the file's name.
		upd, err := tx.PrepareContext(ctx, `UPDATE assets SET
				taken_at = CASE WHEN ?1 != 0 THEN ?1
					WHEN ?12 != 0 AND date_from = 'file' AND ?12 < taken_at THEN ?12 ELSE taken_at END,
				date_from = CASE WHEN ?1 != 0 THEN 'exif' ELSE date_from END,
				width = NULLIF(?2, 0), height = NULLIF(?3, 0), orientation = ?4,
				camera = NULLIF(?5, ''), lat = ?6, lon = ?7,
				duration_ms = NULLIF(?8, 0), video_codec = NULLIF(?9, ''), audio_codec = NULLIF(?10, '')
			WHERE id = ?11`)
		if err != nil {
			return err
		}
		defer upd.Close()
		// Thumbnails, embeddings and faces made with another orientation are
		// sideways: redo them (unless they are still waiting anyway).
		redo, err := tx.PrepareContext(ctx, `UPDATE asset_jobs SET status = 'pending', error = NULL, attempts = 0, updated_at = ?1
			WHERE asset_id = ?2 AND status != 'pending' AND (SELECT orientation FROM assets WHERE id = ?2) != ?3
			AND stage IN (`+placeholders(len(afterMetadata))+`)`) // ?4 on, as SQLite numbers them after ?3
		if err != nil {
			return err
		}
		defer redo.Close()
		job, err := prepareJobUpdate(ctx, tx)
		if err != nil {
			return err
		}
		defer job.Close()
		for _, r := range rs {
			if r.Err == "" && !r.Skipped {
				args := []any{now, r.AssetID, max(r.Orientation, 1)}
				for _, st := range afterMetadata {
					args = append(args, st)
				}
				if _, err := redo.ExecContext(ctx, args...); err != nil {
					return err
				}
				if _, err := upd.ExecContext(ctx, r.TakenAt, r.Width, r.Height, max(r.Orientation, 1),
					r.Camera, r.Lat, r.Lon, r.Duration, r.VideoCodec, r.AudioCodec, r.AssetID, r.Modified); err != nil {
					return err
				}
			}
			if err := execJob(ctx, job, r.AssetID, core.StageMetadata, version, jobStatus(r.Err, r.Skipped), r.Err, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveThumbnails records a batch of finished thumbnails and closes their jobs.
func (s *Store) SaveThumbnails(ctx context.Context, version string, rs []ThumbResult) error {
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
			if err := execJob(ctx, job, r.AssetID, core.StageThumbnail, version, jobStatus(r.Err, r.Skipped), r.Err, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// prepareJobUpdate closes a job (see execJob), counting consecutive failures.
func prepareJobUpdate(ctx context.Context, tx *sql.Tx) (*sql.Stmt, error) {
	return tx.PrepareContext(ctx, `UPDATE asset_jobs SET status = ?1, model_version = ?2, error = NULLIF(?3, ''), updated_at = ?4,
			attempts = CASE WHEN ?1 = 'error' THEN attempts + 1 ELSE 0 END
		WHERE asset_id = ?5 AND stage = ?6`)
}

func jobStatus(errMsg string, skipped bool) core.JobStatus {
	switch {
	case errMsg != "":
		return core.JobError
	case skipped:
		return core.JobSkipped
	}
	return core.JobDone
}

func execJob(ctx context.Context, st *sql.Stmt, assetID int64, stage core.Stage, version string,
	status core.JobStatus, errMsg string, now int64) error {
	_, err := st.ExecContext(ctx, status, version, errMsg, now, assetID, stage)
	return err
}

// CountPending returns how many visible assets still wait for any of the
// stages, so a search can say its results may grow.
func (s *Store) CountPending(ctx context.Context, stages ...core.Stage) (int, error) {
	if len(stages) == 0 {
		return 0, nil
	}
	list, err := json.Marshal(stages)
	if err != nil {
		return 0, err
	}
	var n int
	err = s.r.QueryRowContext(ctx, `SELECT count(DISTINCT j.asset_id) FROM asset_jobs j JOIN assets a ON a.id = j.asset_id
		WHERE j.status = 'pending' AND j.stage IN (SELECT value FROM json_each(?)) AND `+visibleA, string(list)).Scan(&n)
	return n, err
}

// JobCounts returns how many jobs are waiting per stage, and how many failed
// or were skipped. It reads only unfinished jobs (the asset_jobs_status
// index), and ignores assets whose files are gone: their jobs wait, unrun,
// until the content reappears.
func (s *Store) JobCounts(ctx context.Context) (pending map[core.Stage]int, failed, skipped int, err error) {
	rows, err := s.r.QueryContext(ctx, `SELECT j.stage, j.status, count(*) FROM asset_jobs j
		JOIN assets a ON a.id = j.asset_id
		WHERE j.status IN ('pending', 'error', 'skipped') AND a.`+present+`
		GROUP BY j.stage, j.status`)
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
		case core.JobDone: // not selected
		}
	}
	return pending, failed, skipped, rows.Err()
}

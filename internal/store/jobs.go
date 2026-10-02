package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"lumen/internal/core"
	"lumen/internal/places"
)

// JobItem is one asset waiting for a stage, with what a worker needs to run it.
type JobItem struct {
	AssetID     int64
	Path        string
	Kind        core.Kind
	Ext         string
	Orientation int
	Duration    int64
	TakenAt     int64
	Width       int // as stored (0 if not known yet): upgrade rules judge by size
	Height      int
	// Prior is the version that made the output this job would replace,
	// when an upgrade put the finished job back in the queue: the stage may
	// keep that output where it would come out the same. "" otherwise.
	Prior string
}

// TimeRange limits a job query to assets taken in [From, To) (wall-clock seconds).
type TimeRange struct{ From, To int64 }

// waitsFor lists the stages each stage needs finished first: thumbnails
// need the orientation from metadata, the image model reads the thumbnail,
// and faces need both (the orientation to decode upright, the thumbnail to be
// shown cut out of). Every earlier stage is listed, not just the previous
// one, so a re-run of metadata holds back everything after it.
//
// Text is read from the original photo, but only once the embedding says the
// photo is a screenshot or a document, so it waits for that too.
var waitsFor = map[core.Stage][]core.Stage{
	core.StageThumbnail: {core.StageMetadata},
	core.StageEmbedding: {core.StageMetadata, core.StageThumbnail},
	core.StageFaces:     {core.StageMetadata, core.StageThumbnail},
	core.StageOCR:       {core.StageMetadata, core.StageThumbnail, core.StageEmbedding},
}

// queuedStages is every stage that has jobs: core.Stages, and the OCR stage,
// which only some photos have.
var queuedStages = append(slices.Clone(core.Stages), core.StageOCR)

// unfinished is the SQL list of statuses of a job still to do.
const unfinished = "('pending', 'waiting')"

// heldBack is true for a job of asset_jobs (the outer table) whose earlier
// stages (waitsFor) are not all finished, NULL for a stage with none.
var heldBack = func() string {
	var b strings.Builder
	b.WriteString("CASE asset_jobs.stage")
	for _, st := range queuedStages {
		before := waitsFor[st]
		if len(before) == 0 {
			continue
		}
		fmt.Fprintf(&b, " WHEN '%s' THEN EXISTS (SELECT 1 FROM asset_jobs p WHERE p.asset_id = asset_jobs.asset_id"+
			" AND p.status IN %s AND p.stage IN ('%s'))", st, unfinished, strings.Join(stageNames(before), "', '"))
	}
	return b.String() + " END"
}()

func stageNames(stages []core.Stage) []string {
	names := make([]string, len(stages))
	for i, st := range stages {
		names[i] = string(st)
	}
	return names
}

// settleSQL, completed with a condition on asset_jobs, makes the unfinished
// jobs it selects 'waiting' while an earlier stage they need is unfinished
// and 'pending' once none is, so asset_jobs_pending holds only jobs a worker
// can run. Every change to a job's status is followed by it. Metadata jobs
// (heldBack is NULL) and jobs already right are left alone.
var settleSQL = `UPDATE asset_jobs SET status = CASE WHEN ` + heldBack + ` THEN 'waiting' ELSE 'pending' END
	WHERE status IN ` + unfinished + ` AND (status = 'waiting') != (` + heldBack + `) AND `

func settle(ctx context.Context, tx *sql.Tx, where string, args ...any) error {
	_, err := tx.ExecContext(ctx, settleSQL+where, args...)
	return err
}

// afterMetadata are the stages whose results depend on the orientation
// metadata reads, so they re-run when it changes.
var afterMetadata = []core.Stage{core.StageThumbnail, core.StageEmbedding, core.StageFaces, core.StageOCR}

// MaxAttempts is how many times in a row a job may fail before it is left
// failed until its stage's version changes.
const MaxAttempts = 3

// primaryFile selects the file a job or the media routes read for asset a:
// the first one registered, so every caller agrees on it.
const primaryFile = "(SELECT min(id) FROM files WHERE asset_id = a.id)"

// PendingJobs returns up to limit assets waiting for stage, newest first. With
// a range, only assets inside it are returned; the indexer uses that to do
// whatever the user is looking at first. An asset with an earlier stage still
// unfinished is left for later (its job is waiting; the check here only
// guards against a status left unsettled). The asset_jobs_pending index gives the order,
// whatever statistics the planner has: sorting the whole backlog for every
// batch cost a third of a second at 300,000 photos.
func (s *Store) PendingJobs(ctx context.Context, stage core.Stage, within *TimeRange, limit int) ([]JobItem, error) {
	q, args := pendingJobsQuery(stage, within, limit)
	return queryAll(ctx, s.r, scanJob, q, args...)
}

// PendingJobsKind selects runnable jobs of one media kind in queue order.
func (s *Store) PendingJobsKind(ctx context.Context, stage core.Stage, kind core.Kind, within *TimeRange, limit int) ([]JobItem, error) {
	q, args := pendingJobsKindQuery(stage, kind, within, limit)
	return queryAll(ctx, s.r, scanJob, q, args...)
}

func pendingJobsKindQuery(stage core.Stage, kind core.Kind, within *TimeRange, limit int) (q string, args []any) {
	q, args = runnableQuery("asset_jobs j INDEXED BY asset_jobs_pending_kind", nil, stage)
	q += " AND j.kind = ?"
	args = append(args, kind)
	if within != nil {
		q += " AND j.taken_at >= ? AND j.taken_at < ?"
		args = append(args, within.From, within.To)
	}
	q += " ORDER BY j.taken_at DESC, j.asset_id DESC LIMIT ?"
	return q, append(args, limit)
}

func pendingJobsQuery(stage core.Stage, within *TimeRange, limit int) (q string, args []any) {
	q, args = runnableQuery("asset_jobs j INDEXED BY asset_jobs_pending", nil, stage)
	if within != nil {
		q += " AND j.taken_at >= ? AND j.taken_at < ?"
		args = append(args, within.From, within.To)
	}
	q += " ORDER BY j.taken_at DESC, j.asset_id DESC LIMIT ?"
	return q, append(args, limit)
}

// scanJob reads a JobItem as runnableQuery selects it.
func scanJob(r *sql.Rows) (j JobItem, err error) {
	err = r.Scan(&j.AssetID, &j.Path, &j.Kind, &j.Ext, &j.Orientation, &j.Duration, &j.Prior, &j.TakenAt, &j.Width, &j.Height)
	return j, err
}

// runnableQuery selects the jobs of stage a worker can run, as JobItems,
// from the tables in from (which name the jobs j), with args for from.
func runnableQuery(from string, fromArgs []any, stage core.Stage) (q string, args []any) {
	q = `SELECT a.id, f.path, a.kind, a.ext, a.orientation, COALESCE(a.duration_ms, 0), COALESCE(j.prior_version, ''), j.taken_at,
			COALESCE(a.width, 0), COALESCE(a.height, 0)
		FROM ` + from + `
		JOIN assets a ON a.id = j.asset_id
		JOIN files f ON f.id = ` + primaryFile + `
		WHERE j.stage = ? AND j.status = 'pending' AND a.` + present
	args = append(slices.Clip(fromArgs), stage)
	if before := waitsFor[stage]; len(before) > 0 {
		q += ` AND NOT EXISTS (SELECT 1 FROM asset_jobs p
			WHERE p.asset_id = j.asset_id AND p.status IN ` + unfinished + ` AND p.stage IN (` + placeholders(len(before)) + `))`
		for _, st := range before {
			args = append(args, st)
		}
	}
	return q, args
}

// PendingJobsOf returns the assets among ids whose job for stage can run now
// (see PendingJobs), newest first.
func (s *Store) PendingJobsOf(ctx context.Context, stage core.Stage, ids []int64) ([]JobItem, error) {
	if len(ids) == 0 {
		return []JobItem{}, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	// CROSS JOIN: look each one up by key, rather than walk the stage's queue.
	q, args := runnableQuery("json_each(?) ids CROSS JOIN asset_jobs j ON j.asset_id = ids.value", []any{string(list)}, stage)
	return queryAll(ctx, s.r, scanJob, q+" ORDER BY j.taken_at DESC, j.asset_id DESC", args...)
}

// Unfinished returns which of ids still have stage to do (pending or waiting).
func (s *Store) Unfinished(ctx context.Context, stage core.Stage, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := queryAll(ctx, s.r, func(r *sql.Rows) (id int64, err error) {
		err = r.Scan(&id)
		return id, err
	}, `SELECT j.asset_id FROM json_each(?) ids CROSS JOIN asset_jobs j ON j.asset_id = ids.value
		WHERE j.stage = ? AND j.status IN `+unfinished, string(list), stage)
	for _, id := range rows {
		out[id] = true
	}
	return out, err
}

// placeholders returns n comma-separated SQL parameters.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// RetryFailed puts failed jobs of a source's assets back in the queue, for
// jobs that have not yet failed MaxAttempts times: a scan just found their
// files readable, so a passing cause (an unplugged drive, a locked file) may
// be gone. It returns how many jobs were requeued.
func (s *Store) RetryFailed(ctx context.Context, sourceID int64) (n int64, err error) {
	const ofSource = "asset_id IN (SELECT asset_id FROM files WHERE source_id = ?)"
	err = s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE asset_jobs SET status = 'pending', error = NULL, updated_at = ?
			WHERE status = 'error' AND attempts < ? AND `+ofSource, time.Now().Unix(), MaxAttempts, sourceID)
		if err != nil {
			return err
		}
		if n, _ = res.RowsAffected(); n == 0 {
			return nil
		}
		return settle(ctx, tx, ofSource, sourceID)
	})
	return n, err
}

// MetadataResult is the outcome of the metadata stage for one asset.
type MetadataResult struct {
	AssetID     int64
	TakenAt     int64 // 0 uses the scanner's guess, restoring it if EXIF no longer supplies a date
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
	// Unchanged: the metadata read at the job's prior version stands (see
	// index.metadataStand); only the new version is recorded.
	Unchanged bool
}

// ThumbResult is the outcome of the thumbnail stage for one asset.
type ThumbResult struct {
	AssetID int64
	PHash   *uint64
	Err     string
	Skipped bool // the format isn't supported by this build yet
	// Unchanged: the thumbnails made at the job's prior version stand (see
	// JobItem.Prior); nothing but the version is saved.
	Unchanged bool
}

// RequeueOutdated puts back in the queue every finished job whose stage now
// has a different version (a better decoder, an upgraded model). Every job
// records the version that ran it, so only that stage re-runs; other stages
// and all user edits are untouched. A job that had succeeded keeps that
// version as its prior_version (JobItem.Prior), for the stage to check
// whether its output would change. It returns how many jobs were requeued.
func (s *Store) RequeueOutdated(ctx context.Context, versions map[core.Stage]string) (total int64, err error) {
	kinds := make(map[core.Stage]StageVersion, len(versions))
	for stage, version := range versions {
		kinds[stage] = StageVersion{Photo: version, Video: version}
	}
	return s.RequeueVersions(ctx, kinds)
}

// StageVersion versions each media kind and optionally adopts an unchanged legacy photo version.
type StageVersion struct {
	Photo       string
	Video       string
	LegacyPhoto string
}

// RequeueVersions retries outdated work without rerunning unchanged legacy photos.
func (s *Store) RequeueVersions(ctx context.Context, versions map[core.Stage]StageVersion) (total int64, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		for stage, version := range versions {
			if version.LegacyPhoto != "" {
				if _, err := tx.ExecContext(ctx, `UPDATE asset_jobs SET model_version = ?
					WHERE stage = ? AND status NOT IN `+unfinished+` AND model_version = ?
					AND asset_id IN (SELECT id FROM assets WHERE kind = 'photo')`,
					version.Photo, stage, version.LegacyPhoto); err != nil {
					return err
				}
			}
			res, err := tx.ExecContext(ctx, `UPDATE asset_jobs SET status = 'pending', error = NULL, attempts = 0, updated_at = ?,
					prior_version = CASE WHEN status = 'done' THEN model_version END
				WHERE stage = ? AND status NOT IN `+unfinished+` AND COALESCE(model_version, '') !=
					CASE (SELECT kind FROM assets WHERE id = asset_jobs.asset_id) WHEN 'video' THEN ? ELSE ? END`,
				time.Now().Unix(), stage, version.Video, version.Photo)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			total += n
		}
		if total == 0 {
			return nil
		}
		return settle(ctx, tx, "1")
	})
	return total, err
}

// SaveMetadata stores a batch of metadata results and closes their jobs,
// recording the version that produced them.
func (s *Store) SaveMetadata(ctx context.Context, version string, rs []MetadataResult) error {
	if len(rs) == 0 {
		return nil
	}
	cities := make([]places.City, len(rs))
	for i, r := range rs {
		if r.Err == "" && !r.Skipped && !r.Unchanged && r.Lat != nil && r.Lon != nil {
			var err error
			cities[i], _, err = places.Lookup(*r.Lat, *r.Lon)
			if err != nil {
				return err
			}
		}
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		guesses, err := metadataDateGuesses(ctx, tx, rs)
		if err != nil {
			return err
		}
		// Without a capture date, the time the file says it was saved beats
		// the file system's when it is older (copies are newer), but never a
		// date in the file's name.
		upd, err := tx.PrepareContext(ctx, `UPDATE assets SET
				taken_at = CASE WHEN ?1 != 0 THEN ?1
					WHEN date_from = 'exif' AND ?15 != '' THEN
						CASE WHEN ?12 != 0 AND ?15 = 'file' AND ?12 < ?14 THEN ?12 ELSE ?14 END
					WHEN ?12 != 0 AND date_from = 'file' AND ?12 < taken_at THEN ?12 ELSE taken_at END,
				date_from = CASE WHEN ?1 != 0 THEN 'exif'
					WHEN date_from = 'exif' AND ?15 != '' THEN ?15 ELSE date_from END,
				width = NULLIF(?2, 0), height = NULLIF(?3, 0), orientation = ?4,
				camera = NULLIF(?5, ''), lat = ?6, lon = ?7, place_id = ?13, place_checked = 1,
				duration_ms = NULLIF(?8, 0), video_codec = NULLIF(?9, ''), audio_codec = NULLIF(?10, '')
			WHERE id = ?11`)
		if err != nil {
			return err
		}
		defer upd.Close()
		// Thumbnails, embeddings and faces made with another orientation are
		// sideways: redo them in full (unless they are still to do anyway, with
		// no output to keep). The job is settled below, with the metadata job.
		redo, err := tx.PrepareContext(ctx, `UPDATE asset_jobs SET status = 'pending', error = NULL, attempts = 0, updated_at = ?1,
				prior_version = NULL
			WHERE asset_id = ?2 AND (status NOT IN `+unfinished+` OR prior_version IS NOT NULL)
			AND (SELECT orientation FROM assets WHERE id = ?2) != ?3
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
		for i, r := range rs {
			if r.Err == "" && !r.Skipped && !r.Unchanged {
				if err := savePlace(ctx, tx, cities[i]); err != nil {
					return err
				}
				var placeID any
				if cities[i].ID != 0 {
					placeID = cities[i].ID
				}
				args := []any{now, r.AssetID, max(r.Orientation, 1)}
				for _, st := range afterMetadata {
					args = append(args, st)
				}
				if _, err := redo.ExecContext(ctx, args...); err != nil {
					return err
				}
				if _, err := upd.ExecContext(ctx, r.TakenAt, r.Width, r.Height, max(r.Orientation, 1),
					r.Camera, r.Lat, r.Lon, r.Duration, r.VideoCodec, r.AudioCodec, r.AssetID, r.Modified, placeID,
					guesses[r.AssetID].takenAt, guesses[r.AssetID].from); err != nil {
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

type dateGuess struct {
	takenAt int64
	from    core.DateSource
}

// metadataDateGuesses restores the scanner's best guess across remaining copies
// when a metadata re-run rejects the date that EXIF previously supplied.
func metadataDateGuesses(ctx context.Context, tx *sql.Tx, rs []MetadataResult) (map[int64]dateGuess, error) {
	guesses := make(map[int64]dateGuess)
	var ids []int64
	for _, r := range rs {
		if r.TakenAt == 0 && r.Err == "" && !r.Skipped && !r.Unchanged {
			ids = append(ids, r.AssetID)
		}
	}
	if len(ids) == 0 {
		return guesses, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id, f.path, f.mtime
		FROM json_each(?) ids CROSS JOIN assets a ON a.id = ids.value
		JOIN files f ON f.asset_id = a.id WHERE a.date_from = 'exif' ORDER BY f.id`, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, mtime int64
		var path string
		if err := rows.Scan(&id, &path, &mtime); err != nil {
			return nil, err
		}
		at, from := core.GuessDate(filepath.Base(path), time.Unix(0, mtime))
		if old, ok := guesses[id]; !ok || (old.from == core.DateFromFile && (from == core.DateFromFilename || at < old.takenAt)) {
			guesses[id] = dateGuess{takenAt: at, from: from}
		}
	}
	return guesses, rows.Err()
}

// SaveThumbnails records a batch of finished thumbnails and closes their
// jobs. An Unchanged result only records the version. New thumbnails of an
// asset already embedded (an upgrade made them afresh) put its embedding
// back in the queue: the image model reads the thumbnail, so its vector and
// tags were made from the old pixels.
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
		// Settled with the thumbnail job, below.
		reembed, err := tx.PrepareContext(ctx, `UPDATE asset_jobs SET status = 'pending', error = NULL, attempts = 0, updated_at = ?,
				prior_version = NULL
			WHERE asset_id = ? AND stage = ? AND status NOT IN `+unfinished)
		if err != nil {
			return err
		}
		defer reembed.Close()
		job, err := prepareJobUpdate(ctx, tx)
		if err != nil {
			return err
		}
		defer job.Close()
		for _, r := range rs {
			if r.Err == "" && !r.Skipped && !r.Unchanged {
				var ph any
				if r.PHash != nil {
					ph = int64(*r.PHash) // SQLite integers are signed; the bits are what matter
				}
				if _, err := upd.ExecContext(ctx, ph, r.AssetID); err != nil {
					return err
				}
				if _, err := reembed.ExecContext(ctx, now, r.AssetID, core.StageEmbedding); err != nil {
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

// jobCloser closes jobs (see execJob) in a save's transaction.
type jobCloser struct {
	update *sql.Stmt // records the outcome, counting consecutive failures
	settle *sql.Stmt // then releases the asset's jobs that waited for it
	video  string
}

func prepareJobUpdate(ctx context.Context, tx *sql.Tx) (*jobCloser, error) {
	update, err := tx.PrepareContext(ctx, `UPDATE asset_jobs SET status = ?1,
		model_version = CASE (SELECT kind FROM assets WHERE id = ?5) WHEN 'video' THEN ?7 ELSE ?2 END,
		error = NULLIF(?3, ''), updated_at = ?4,
			prior_version = NULL, attempts = CASE WHEN ?1 = 'error' THEN attempts + 1 ELSE 0 END
		WHERE asset_id = ?5 AND stage = ?6`)
	if err != nil {
		return nil, err
	}
	settle, err := tx.PrepareContext(ctx, settleSQL+"asset_id = ?")
	if err != nil {
		return nil, err // update closes with tx
	}
	return &jobCloser{update: update, settle: settle}, nil
}

func prepareVersionedJobUpdate(ctx context.Context, tx *sql.Tx, version StageVersion) (*jobCloser, error) {
	closer, err := prepareJobUpdate(ctx, tx)
	if err == nil {
		closer.video = version.Video
	}
	return closer, err
}

func (c *jobCloser) Close() error {
	return errors.Join(c.update.Close(), c.settle.Close())
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

func execJob(ctx context.Context, c *jobCloser, assetID int64, stage core.Stage, version string,
	status core.JobStatus, errMsg string, now int64) error {
	video := c.video
	if video == "" {
		video = version
	}
	if _, err := c.update.ExecContext(ctx, status, version, errMsg, now, assetID, stage, video); err != nil {
		return err
	}
	if !hasLaterStages[stage] {
		return nil
	}
	_, err := c.settle.ExecContext(ctx, assetID)
	return err
}

// hasLaterStages is true for a stage other stages wait for.
var hasLaterStages = func() map[core.Stage]bool {
	m := map[core.Stage]bool{}
	for _, before := range waitsFor {
		for _, st := range before {
			m[st] = true
		}
	}
	return m
}()

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
		WHERE j.status IN `+unfinished+` AND j.stage IN (SELECT value FROM json_each(?)) AND `+visibleA, string(list)).Scan(&n)
	return n, err
}

// JobCounts returns how many visible jobs are still to do per stage, and how
// many failed or were skipped. Hidden assets keep being indexed, but their
// jobs must not appear in the UI's progress counts.
func (s *Store) JobCounts(ctx context.Context) (pending map[core.Stage]int, failed, skipped int, err error) {
	rows, err := s.r.QueryContext(ctx, `SELECT j.stage, j.status, count(*) FROM asset_jobs j
		JOIN assets a ON a.id = j.asset_id
		WHERE j.status IN ('pending', 'waiting', 'error', 'skipped') AND `+visibleA+`
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
		case core.JobPending, core.JobWaiting:
			pending[stage] += n
		case core.JobSkipped:
			skipped += n
		case core.JobError:
			failed += n
		case core.JobDone: // not selected
		}
	}
	return pending, failed, skipped, rows.Err()
}

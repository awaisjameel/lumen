package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"lumen/internal/core"
)

// exportHistory is how many settled outcomes the journal keeps and shows.
const exportHistory = 100

// SaveExportPlan journals a batch before any file is written. Like an
// organise plan, a new preview replaces those never run; settled outcomes
// beyond the recent history go with them, so the journal stays bounded.
func (s *Store) SaveExportPlan(ctx context.Context, jobs []core.ExportJob) (int64, error) {
	if len(jobs) == 0 {
		return 0, ErrNotFound
	}
	var batch int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM export_jobs WHERE state = ?1 OR (state IN (?2, ?3, ?4)
			AND id NOT IN (SELECT id FROM export_jobs WHERE state IN (?2, ?3, ?4) ORDER BY id DESC LIMIT ?5))`,
			core.ExportPlanned, core.ExportDone, core.ExportFailed, core.ExportCancelled, exportHistory); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM export_batches WHERE id NOT IN (SELECT batch FROM export_jobs)"); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO export_batches DEFAULT VALUES")
		if err != nil {
			return err
		}
		batch, err = res.LastInsertId()
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO export_jobs(batch, asset_id, from_path, to_path, size, mtime, options) VALUES (?, ?, ?, ?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, job := range jobs {
			data, err := json.Marshal(job.Options)
			if err != nil {
				return err
			}
			if _, err := stmt.ExecContext(ctx, batch, job.AssetID, job.From, job.To, job.Size, job.MTime, string(data)); err != nil {
				return err
			}
		}
		return nil
	})
	return batch, err
}

// HasInterruptedExports reports outputs requiring startup recovery.
func (s *Store) HasInterruptedExports(ctx context.Context) (bool, error) {
	var exists bool
	err := s.r.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM export_jobs WHERE state IN (?, ?))", core.ExportWriting, core.ExportReady).Scan(&exists)
	return exists, err
}

// ExportJobs reads journal entries, including those needed for recovery.
func (s *Store) ExportJobs(ctx context.Context, batch int64) ([]core.ExportJob, error) {
	return s.exportJobs(ctx, batch, false)
}

// VisibleExportJobs leaves covered and trashed paths out of public history.
func (s *Store) VisibleExportJobs(ctx context.Context, batch int64) ([]core.ExportJob, error) {
	return s.exportJobs(ctx, batch, true)
}

func (s *Store) exportJobs(ctx context.Context, batch int64, visibleOnly bool) ([]core.ExportJob, error) {
	where := ""
	order := " ORDER BY id"
	if visibleOnly {
		where = " AND asset_id IN (SELECT id FROM assets WHERE " + visible + ")"
	}
	if visibleOnly && batch == 0 {
		order = " ORDER BY id DESC LIMIT " + strconv.Itoa(exportHistory)
	}
	return queryAll(ctx, s.r, func(r *sql.Rows) (j core.ExportJob, err error) {
		var options string
		err = r.Scan(&j.ID, &j.Batch, &j.AssetID, &j.From, &j.To, &j.Size, &j.MTime, &j.State, &j.Error, &j.Temp, &j.Digest, &options)
		if err == nil {
			err = json.Unmarshal([]byte(options), &j.Options)
		}
		return j, err
	}, "SELECT id, batch, asset_id, from_path, to_path, size, mtime, state, error, temp, digest, options FROM export_jobs WHERE (? = 0 OR batch = ?)"+where+order, batch, batch)
}

// ExportAllowed revalidates visibility and destination blocks at publication.
func (s *Store) ExportAllowed(ctx context.Context, id int64, destination string) error {
	return s.read(ctx, func(tx *sql.Tx) error {
		var ok bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM assets WHERE id = ? AND "+visible+")", id).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		blocked, err := blockedFolders(ctx, tx)
		if err != nil {
			return err
		}
		for _, folder := range blocked {
			if core.IsWithin(destination, folder) {
				return ErrNotFound
			}
		}
		return nil
	})
}

// SetExportJob records a file operation before or after publication.
func (s *Store) SetExportJob(ctx context.Context, id int64, state core.ExportState, message, temp, digest string) error {
	return affected(s.w.ExecContext(ctx, "UPDATE export_jobs SET state = ?, error = ?, temp = ?, digest = ? WHERE id = ?", state, message, temp, digest, id))
}

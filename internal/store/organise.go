package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"lumen/internal/core"
)

// Organise on disk: the store keeps what a run is going to do (a plan of
// moves), what it has done and what was in doubt when it stopped. The files
// themselves are moved by internal/fileops, which journals each step here
// before and after, so a crash always leaves a row that says what to check.

// ErrOrganiseState means a move or run was not in the state the change
// expects: another call (or a crash recovery) got to it first.
var ErrOrganiseState = errors.New("the move is no longer in the state expected")

// OrganiseFile is a file the planner may move: a file of a visible asset in
// an enabled source. The names are the ones the UI shows, so a name a block
// hides never reaches a folder name.
type OrganiseFile struct {
	FileID   int64
	AssetID  int64
	SourceID int64
	Root     string // the source's folder: the file only ever moves inside it
	Path     string
	Size     int64 // as the library recorded them
	MTime    int64 // Unix nanoseconds
	TakenAt  int64 // wall-clock seconds (core.WallClock)
	Camera   string
	Place    string // "" for none, or when a block hides the place's name
	Event    string // the name of the event the photo is in, as the Events view shows it
}

// OrganiseSnapshot is what a plan is made from.
type OrganiseSnapshot struct {
	Files []OrganiseFile
	// Known holds core.Fold of every path the library has a file at in the
	// sources in scope: a destination there would break the unique path.
	Known map[string]bool
	// BlockedDirs are the folders the user blocked: nothing is moved into one.
	BlockedDirs []string
}

// OrganiseSnapshot reads the visible files of the enabled source sourceID
// (0: every enabled source), oldest photo first, with the names a path may
// use. Blocked, trashed, hidden and missing assets are left out by the same
// predicate every view uses.
func (s *Store) OrganiseSnapshot(ctx context.Context, sourceID int64, gapHours int) (OrganiseSnapshot, error) {
	events, err := s.Events(ctx, gapHours)
	if err != nil {
		return OrganiseSnapshot{}, err
	}
	// Events come newest first with pinned ones ahead; the lookup needs them
	// in timeline order, which is how their photos are walked below.
	slices.SortFunc(events, func(a, b core.Event) int {
		return cmp.Or(cmp.Compare(a.StartAt, b.StartAt), cmp.Compare(a.FirstID, b.FirstID))
	})
	var snap OrganiseSnapshot
	err = s.read(ctx, func(tx *sql.Tx) error {
		files, err := queryAll(ctx, tx, func(r *sql.Rows) (f OrganiseFile, err error) {
			err = r.Scan(&f.FileID, &f.AssetID, &f.SourceID, &f.Root, &f.Path, &f.Size, &f.MTime, &f.TakenAt, &f.Camera, &f.Place)
			return f, err
		}, `SELECT f.id, f.asset_id, f.source_id, s.path, f.path, f.size, f.mtime, a.taken_at, COALESCE(a.camera, ''),
			CASE WHEN a.place_id IS NULL OR `+placeBlocked("a.place_id")+` THEN ''
				ELSE COALESCE((SELECT name FROM places WHERE id = a.place_id), '') END
			FROM files f JOIN assets a ON a.id = f.asset_id JOIN sources s ON s.id = f.source_id
			WHERE s.enabled = 1 AND (?1 = 0 OR f.source_id = ?1) AND `+visibleA+`
			ORDER BY a.taken_at, f.asset_id, f.id`, sourceID)
		if err != nil {
			return err
		}
		snap.Files = files
		known, err := queryAll(ctx, tx, func(r *sql.Rows) (p string, err error) {
			err = r.Scan(&p)
			return p, err
		}, "SELECT path FROM files WHERE source_id IN (SELECT id FROM sources WHERE enabled = 1 AND (?1 = 0 OR id = ?1))", sourceID)
		if err != nil {
			return err
		}
		snap.Known = make(map[string]bool, len(known))
		for _, p := range known {
			snap.Known[core.Fold(p)] = true
		}
		snap.BlockedDirs, err = blockedFolders(ctx, tx)
		return err
	})
	if err != nil {
		return OrganiseSnapshot{}, err
	}
	// The files come in timeline order, as the events do: one walk pairs them.
	next := 0
	for i := range snap.Files {
		f := &snap.Files[i]
		for next < len(events) && eventEndsBefore(events[next], f.TakenAt, f.AssetID) {
			next++
		}
		if next < len(events) && !eventStartsAfter(events[next], f.TakenAt, f.AssetID) {
			f.Event = events[next].Name
		}
	}
	return snap, nil
}

// eventEndsBefore is whether the photo (at, id) lies after the event's last.
func eventEndsBefore(e core.Event, at, id int64) bool {
	return e.EndAt < at || (e.EndAt == at && e.LastID < id)
}

// eventStartsAfter is whether the photo (at, id) lies before the event's first.
func eventStartsAfter(e core.Event, at, id int64) bool {
	return e.StartAt > at || (e.StartAt == at && e.FirstID > id)
}

// blockedFolders lists the folders the user blocked.
func blockedFolders(ctx context.Context, q querier) ([]string, error) {
	return queryAll(ctx, q, func(r *sql.Rows) (p string, err error) {
		err = r.Scan(&p)
		return p, err
	}, "SELECT path FROM blocks WHERE kind = 'folder' AND path IS NOT NULL")
}

// The states of a move's XMP sidecar (OrganiseMove.Sidecar); "" is none.
const (
	SidecarPlanned = "planned" // one to move with the photo
	SidecarMoved   = "moved"
	SidecarKept    = "kept"   // left where it was; the move's error says why
	SidecarUndone  = "undone" // put back with the photo
)

// OrganiseSidecar is what became of a photo's sidecar in a move, handed to
// FinishOrganiseMove with the photo so that both are recorded in one commit.
type OrganiseSidecar struct {
	State  string // a Sidecar* state, or "" when the photo had none
	Reason string // why it was kept, for the move's error
	From   string // where the sidecar was, and where it is now: set when it moved
	To     string
}

// OrganiseMove is one file's move as the journal holds it.
type OrganiseMove struct {
	Seq     int
	FileID  int64 // 0 when the plan had no files row for it
	AssetID int64
	From    string
	To      string
	Size    int64
	MTime   int64
	Renamed bool
	Sidecar string // the sidecar's state: "" none, or a Sidecar* state
	State   core.OrganiseMoveState
	Error   string
}

// SaveOrganisePlan stores a preview as a planned run and numbers its moves
// from 1 in order (moves[i].Seq is set). Earlier previews that were never
// started are dropped: only the latest one can be started, and a plan of a
// large library is a lot of rows to keep.
func (s *Store) SaveOrganisePlan(ctx context.Context, template string, moves []OrganiseMove) (runID int64, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM organise_runs WHERE state = 'planned'"); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO organise_runs (template, created_at, state) VALUES (?, ?, 'planned')",
			template, time.Now().Unix())
		if err != nil {
			return err
		}
		if runID, err = res.LastInsertId(); err != nil {
			return err
		}
		ins, err := tx.PrepareContext(ctx, `INSERT INTO organise_moves
			(run_id, seq, file_id, asset_id, from_path, to_path, size, mtime, renamed, sidecar, state, error)
			VALUES (?, ?, NULLIF(?, 0), ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))`)
		if err != nil {
			return err
		}
		defer ins.Close()
		for i := range moves {
			m := &moves[i]
			m.Seq = i + 1
			if _, err := ins.ExecContext(ctx, runID, m.Seq, m.FileID, m.AssetID, m.From, m.To, m.Size, m.MTime, m.Renamed,
				m.Sidecar, m.State, m.Error); err != nil {
				return err
			}
		}
		return nil
	})
	return runID, err
}

// OrganiseRun returns one run, or ErrNotFound.
func (s *Store) OrganiseRun(ctx context.Context, id int64) (core.OrganiseRun, error) {
	runs, err := s.organiseRuns(ctx, "WHERE r.id = ?", id)
	if err != nil {
		return core.OrganiseRun{}, err
	}
	if len(runs) == 0 {
		return core.OrganiseRun{}, ErrNotFound
	}
	return runs[0], nil
}

// OrganiseRuns lists the runs that have started or finished, newest first. A
// preview that was never started is not a past run.
func (s *Store) OrganiseRuns(ctx context.Context) ([]core.OrganiseRun, error) {
	return s.organiseRuns(ctx, "WHERE r.state != 'planned'")
}

func (s *Store) organiseRuns(ctx context.Context, where string, args ...any) ([]core.OrganiseRun, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (run core.OrganiseRun, err error) {
		err = r.Scan(&run.ID, &run.Template, &run.CreatedAt, &run.State, &run.Moved, &run.Total)
		return run, err
	}, `SELECT r.id, r.template, r.created_at, r.state,
		COALESCE(SUM(m.state = 'moved'), 0), COUNT(m.seq)
		FROM organise_runs r LEFT JOIN organise_moves m ON m.run_id = r.id `+where+`
		GROUP BY r.id ORDER BY r.created_at DESC, r.id DESC`, args...)
}

// OrganiseCounts is how many moves of a run are in each state.
func (s *Store) OrganiseCounts(ctx context.Context, runID int64) (map[core.OrganiseMoveState]int, error) {
	type count struct {
		state core.OrganiseMoveState
		n     int
	}
	rows, err := queryAll(ctx, s.r, func(r *sql.Rows) (c count, err error) {
		err = r.Scan(&c.state, &c.n)
		return c, err
	}, "SELECT state, COUNT(*) FROM organise_moves WHERE run_id = ? GROUP BY state", runID)
	if err != nil {
		return nil, err
	}
	out := make(map[core.OrganiseMoveState]int, len(rows))
	for _, c := range rows {
		out[c.state] = c.n
	}
	return out, nil
}

// OrganiseMoves returns the moves of a run that are in one of states (all of
// them when none is given), by seq, or last first with descending.
func (s *Store) OrganiseMoves(ctx context.Context, runID int64, descending bool, states ...core.OrganiseMoveState) ([]OrganiseMove, error) {
	order := "ASC"
	if descending {
		order = "DESC"
	}
	filter := ""
	args := []any{runID}
	if len(states) > 0 {
		filter = " AND state IN (SELECT value FROM json_each(?))"
		list, err := json.Marshal(states)
		if err != nil {
			return nil, err
		}
		args = append(args, string(list))
	}
	return queryAll(ctx, s.r, scanOrganiseMove, `SELECT seq, COALESCE(file_id, 0), asset_id, from_path, to_path, size, mtime,
		renamed, sidecar, state, COALESCE(error, '') FROM organise_moves WHERE run_id = ?`+filter+" ORDER BY seq "+order, args...)
}

func scanOrganiseMove(r *sql.Rows) (m OrganiseMove, err error) {
	err = r.Scan(&m.Seq, &m.FileID, &m.AssetID, &m.From, &m.To, &m.Size, &m.MTime, &m.Renamed, &m.Sidecar, &m.State, &m.Error)
	return m, err
}

// SetOrganiseRunState records where a run stands.
func (s *Store) SetOrganiseRunState(ctx context.Context, runID int64, state core.OrganiseRunState) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE organise_runs SET state = ? WHERE id = ?", state, runID)
		return requireOne(res, err, ErrNotFound)
	})
}

// JournalOrganiseMove changes a move's state from one to another and commits,
// before the file is touched: a crash then leaves 'moving' (or 'undoing') for
// recovery to find. It fails with ErrOrganiseState if the move is not in the
// state from, so two callers cannot both take it. sidecar is the state of
// the photo's XMP sidecar as it goes into the move ("" when it has none), so
// that recovery knows whether a sidecar was to move with the photo.
func (s *Store) JournalOrganiseMove(ctx context.Context, runID int64, seq int, from, to core.OrganiseMoveState, sidecar string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE organise_moves SET state = ?, sidecar = ? WHERE run_id = ? AND seq = ? AND state = ?",
			to, sidecar, runID, seq, from)
		return requireOne(res, err, ErrOrganiseState)
	})
}

// SetOrganiseMove records the outcome of a move that did not change a file
// (skipped or failed, with the reason) or went back to waiting (planned, or
// moved again after an undo that found its way blocked, with the reason).
func (s *Store) SetOrganiseMove(ctx context.Context, runID int64, seq int, state core.OrganiseMoveState, reason string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE organise_moves SET state = ?, error = NULLIF(?, '') WHERE run_id = ? AND seq = ?",
			state, reason, runID, seq)
		return requireOne(res, err, ErrNotFound)
	})
}

// FinishOrganiseMove records that the file of move m (of run runID) now lies
// at newPath, having been at oldPath: in one transaction it points the files
// row at it (if the library still has that row at oldPath) and sets the
// move's state, so the library and the journal never disagree about where
// the file is. side says what became of the photo's sidecar, which is
// recorded in the same commit (a reason it stayed behind goes to the move's
// error).
func (s *Store) FinishOrganiseMove(ctx context.Context, runID int64, m OrganiseMove, oldPath, newPath string,
	state core.OrganiseMoveState, side OrganiseSidecar) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if m.FileID != 0 {
			// A row the library no longer has (a scan forgot the file) is not
			// an error: the file is where it is, and the next scan says so.
			if _, err := tx.ExecContext(ctx, "UPDATE files SET path = ?1 WHERE id = ?2 AND "+foldPath+" = "+foldKey("?3"),
				newPath, m.FileID, oldPath); err != nil {
				return err
			}
		}
		if side.To != "" {
			if err := organiseSidecarMoved(ctx, tx, side.From, side.To); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, "UPDATE organise_moves SET state = ?, sidecar = ?, error = NULLIF(?, '') WHERE run_id = ? AND seq = ?",
			state, side.State, side.Reason, runID, m.Seq)
		return requireOne(res, err, ErrNotFound)
	})
}

// organiseSidecarMoved is called in the transaction that commits a move, for
// each sidecar that moved with its photo (from the old path to the new, both
// of the sidecar). It is the place for anything else that keeps a record of
// sidecar paths (the XMP export's table, once it is in the schema) to follow
// the file in the same commit; there is no such record yet.
func organiseSidecarMoved(_ context.Context, _ *sql.Tx, _, _ string) error { return nil }

// Move refusals, for OrganiseMovable.
const (
	MoveRefusedNotVisible    = "not_visible"    // blocked, trashed, hidden or missing since the preview
	MoveRefusedMissing       = "missing"        // the library has no such file any more
	MoveRefusedBlockedFolder = "blocked_folder" // the destination is in a folder blocked since the preview
)

// OrganiseMovable decides at the moment of the move whether m may still be
// made: its asset is visible, the library still has the file at its place,
// and the destination is not inside a folder blocked since the preview. It
// returns a Move* refusal, or "" to go ahead.
func (s *Store) OrganiseMovable(ctx context.Context, m OrganiseMove) (refusal string, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		var path string
		switch err := tx.QueryRowContext(ctx, "SELECT path FROM files WHERE id = ? AND asset_id = ?", m.FileID, m.AssetID).Scan(&path); {
		case errors.Is(err, sql.ErrNoRows):
			refusal = MoveRefusedMissing
			return nil
		case err != nil:
			return err
		}
		if core.Fold(path) != core.Fold(m.From) {
			refusal = MoveRefusedMissing
			return nil
		}
		var ok bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM assets WHERE id = ? AND "+visible+")", m.AssetID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			refusal = MoveRefusedNotVisible
			return nil
		}
		blocked, err := blockedFolders(ctx, tx)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(blocked, func(dir string) bool { return core.IsWithin(m.To, dir) }) {
			refusal = MoveRefusedBlockedFolder
		}
		return nil
	})
	return refusal, err
}

// OrganiseFileAt reports whether the library has a file of the asset at path.
func (s *Store) OrganiseFileAt(ctx context.Context, assetID int64, path string) (bool, error) {
	var ok bool
	err := s.r.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM files WHERE asset_id = ?1 AND "+foldPath+" = "+foldKey("?2")+")",
		assetID, path).Scan(&ok)
	return ok, err
}

// AddOrganiseDir journals a folder a run is about to create. Journaled first,
// so a crash after the folder is made still tells undo it is Lumen's.
func (s *Store) AddOrganiseDir(ctx context.Context, runID int64, path string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO organise_dirs (run_id, path) VALUES (?, ?)", runID, path)
		return err
	})
}

// OrganiseDirs lists the folders a run created, deepest first, so removing
// them in order empties children before parents.
func (s *Store) OrganiseDirs(ctx context.Context, runID int64) ([]string, error) {
	dirs, err := queryAll(ctx, s.r, func(r *sql.Rows) (p string, err error) {
		err = r.Scan(&p)
		return p, err
	}, "SELECT path FROM organise_dirs WHERE run_id = ?", runID)
	// By length: a folder is always longer than the folder it is inside.
	slices.SortFunc(dirs, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return dirs, err
}

// DropOrganiseDir forgets a folder of the run: it is gone, or was never made.
func (s *Store) DropOrganiseDir(ctx context.Context, runID int64, path string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM organise_dirs WHERE run_id = ? AND path = ?", runID, path)
		return err
	})
}

// InterruptedOrganiseRuns lists the runs a crash left in doubt: running or
// undoing, or holding a move whose file may or may not have moved.
func (s *Store) InterruptedOrganiseRuns(ctx context.Context) ([]int64, error) {
	return queryAll(ctx, s.r, scanID, `SELECT id FROM organise_runs WHERE state IN ('running', 'undoing')
		OR id IN (SELECT run_id FROM organise_moves WHERE state IN ('moving', 'undoing')) ORDER BY id`)
}

// requireOne turns an update that changed no row into notFound.
func requireOne(res sql.Result, err, notFound error) error {
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return err
		}
		return notFound
	}
	return nil
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"lumen/internal/core"
)

// XMP sidecars. The store decides what may be exported (visible photos, no
// blocked label); internal/xmp only writes what it is given, and
// internal/index runs the pass.

// XMPSidecarSuffix is added to a file's path to name its sidecar:
// IMG_1.jpg becomes IMG_1.jpg.xmp.
const XMPSidecarSuffix = ".xmp"

// XMPRevision says whether any sidecar may be out of date: it differs from the
// last one read whenever what a sidecar says, or which photos may be exported,
// can have changed. While XMP export is off nothing in Content changes.
type XMPRevision struct {
	Content int64 // tags, people, favourites, files and the confidence (migration 0027)
	Visible int64 // which photos are visible, so exportable (VisibilityRevision)
}

// XMPRevision reads the revision.
func (s *Store) XMPRevision(ctx context.Context) (XMPRevision, error) {
	var r XMPRevision
	err := s.r.QueryRowContext(ctx, `SELECT x.value, e.visible_value
		FROM xmp_revision x, event_revision e WHERE x.id = 1 AND e.id = 1`).Scan(&r.Content, &r.Visible)
	return r, err
}

// SetXMP turns XMP export on or off. Turning it on counts as a change, so the
// exporter looks at every photo even if nothing else changed since it last
// ran. The setting and the count change together, and the settings cache
// follows only once they have committed.
func (s *Store) SetXMP(ctx context.Context, on bool) error {
	value := "0"
	if on {
		value = "1"
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			core.SettingXMP, value); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE xmp_revision SET value = value + 1 WHERE id = 1")
		return err
	})
	if err == nil {
		s.settings.Store(core.SettingXMP, value)
	}
	return err
}

// XMPFile is one file of an asset, and the sidecar beside it.
type XMPFile struct {
	Path  string    // the file's; its sidecar is Path + XMPSidecarSuffix
	Wrote []byte    // hash of what Lumen last wrote to the sidecar; nil if it never has
	Skip  *XMPStamp // the sidecar as it was when Lumen last left it alone; nil if it has not
}

// XMPStamp is how a sidecar file looked when it was left alone: its size and
// modification time (unix nanoseconds), or size -1 when there was no file. A
// file that still looks the same would be left alone again.
type XMPStamp struct {
	Size  int64
	MTime int64
}

// XMPFace is a named person's face in a photo.
type XMPFace struct {
	Name       string
	X, Y, W, H float64 // the box, as fractions of the upright photo
}

// XMPAsset is what the sidecars of one asset say. When Exportable is false the
// asset may no longer be exported (it is blocked, hidden or in the Trash) but
// still has sidecars Lumen wrote: it carries no content, so that they can be
// reduced to the marker.
type XMPAsset struct {
	ID         int64
	Exportable bool
	Video      bool
	Favourite  bool
	Width      int // as displayed, 0 if not read yet
	Height     int
	Files      []XMPFile
	Tags       []string  // Things that apply at the current confidence
	Faces      []XMPFace // named people, none whose name is blocked
}

// XMPMark identifies the version of one asset's durable export work.
// A later change replaces its revision, so acknowledging this mark cannot
// discard work committed while the sidecars were being written.
type XMPMark struct {
	AssetID  int64
	Revision int64
}

// XMPBatch is assets read together with the revision they were read at.
type XMPBatch struct {
	Assets   []XMPAsset
	Pending  []XMPMark   // includes deleted assets and photos with nothing to export
	Revision XMPRevision // at the snapshot the assets come from
}

// xmpEligible is the full pass's admission rule, shared by targeted reads.
var xmpEligible = `(
				(` + visible + ` AND (favourite = 1 OR ` + xmpHasRecord + `
					OR EXISTS (SELECT 1 FROM asset_tags x WHERE x.asset_id = assets.id AND ` + tagAppliesTo("x", tagConfidenceSQL) + `)
					OR EXISTS (SELECT 1 FROM faces f JOIN people p ON p.id = f.person_id
						WHERE f.asset_id = assets.id AND p.name IS NOT NULL AND ` + faceShown + `)))
				OR (` + present + ` AND ` + xmpHasRecord + `))`

var xmpSelect = `SELECT id, ` + visible + `, kind = 'video', favourite,
	COALESCE(width, 0), COALESCE(height, 0) FROM assets `

func scanXMPAsset(r *sql.Rows) (a XMPAsset, err error) {
	err = r.Scan(&a.ID, &a.Exportable, &a.Video, &a.Favourite, &a.Width, &a.Height)
	return a, err
}

// XMPAssets returns up to limit assets with an id above after, in id order, that
// have something to export or a sidecar to bring back to the marker. It is the
// one place export reads photos, Things and people through, with the same
// predicates as browsing: visible assets only, no Thing or person a block
// covers, and people only when named. The reads share a snapshot, and the
// batch carries the revision of it, so that the exporter can tell later that
// something changed since.
//
// Asset ids above the last one returned are for the next call; fewer than
// limit assets means there are no more.
func (s *Store) XMPAssets(ctx context.Context, after int64, limit int) (XMPBatch, error) {
	var batch XMPBatch
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		if err := tx.QueryRowContext(ctx, `SELECT x.value, e.visible_value
			FROM xmp_revision x, event_revision e WHERE x.id = 1 AND e.id = 1`).Scan(&batch.Revision.Content, &batch.Revision.Visible); err != nil {
			return err
		}
		batch.Assets, err = queryAll(ctx, tx, scanXMPAsset, xmpSelect+`WHERE id > ?1 AND `+xmpEligible+` ORDER BY id LIMIT ?2`, after, limit)
		if err != nil || len(batch.Assets) == 0 {
			return err
		}
		return fillXMPAssets(ctx, tx, batch.Assets)
	})
	return batch, err
}

// XMPAsset reads one asset again, as XMPAssets does; ok is false when it has
// nothing to export and no sidecar of Lumen's to bring back to the marker. The
// exporter re-reads an asset when the revision moved since its batch was read.
func (s *Store) XMPAsset(ctx context.Context, id int64) (a XMPAsset, ok bool, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		list, qerr := queryAll(ctx, tx, scanXMPAsset, xmpSelect+`WHERE id = ? AND `+xmpEligible, id)
		if qerr != nil || len(list) == 0 {
			return qerr
		}
		if qerr := fillXMPAssets(ctx, tx, list); qerr != nil {
			return qerr
		}
		a, ok = list[0], true
		return nil
	})
	return a, ok, err
}

// XMPPending reports whether durable background work remains after a restart.
func (s *Store) XMPPending(ctx context.Context) (bool, error) {
	var pending bool
	err := s.r.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM xmp_dirty)").Scan(&pending)
	return pending, err
}

// XMPSkippedFile is a file whose sidecar was left alone on an earlier pass.
type XMPSkippedFile struct {
	AssetID int64
	Path    string
	XMPStamp
}

// XMPSkippedFiles reads only remembered skips that still sit beside a file.
// Their stamps let the exporter notice an external edit without visiting the
// rest of the library or reading the sidecar's contents.
func (s *Store) XMPSkippedFiles(ctx context.Context) ([]XMPSkippedFile, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (f XMPSkippedFile, err error) {
		err = r.Scan(&f.AssetID, &f.Path, &f.Size, &f.MTime)
		return f, err
	}, `SELECT f.asset_id, k.path, k.size, k.mtime FROM xmp_skips k
		CROSS JOIN files f ON `+foldKey("f.path")+` = `+foldKey("substr(k.path, 1, length(k.path) - "+strconv.Itoa(len(XMPSidecarSuffix))+")"))
}

// MarkXMPAssets queues externally changed skipped sidecars. The setting is
// checked in the write transaction so disabling export cannot create work.
func (s *Store) MarkXMPAssets(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE xmp_revision SET value = value + 1
			WHERE id = 1 AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1'`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO xmp_dirty(asset_id, revision)
			SELECT value, (SELECT value FROM xmp_revision WHERE id = 1) FROM json_each(?)
			WHERE (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1'
			ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision`, string(list))
		return err
	})
}

// XMPDirtyAssets reads only marked keys. Pending also carries keys with no
// current exportable asset, so deleted or empty photos cannot clog the queue.
func (s *Store) XMPDirtyAssets(ctx context.Context, after int64, limit int) (XMPBatch, error) {
	var batch XMPBatch
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		if err := tx.QueryRowContext(ctx, `SELECT x.value, e.visible_value
			FROM xmp_revision x, event_revision e WHERE x.id = 1 AND e.id = 1`).Scan(&batch.Revision.Content, &batch.Revision.Visible); err != nil {
			return err
		}
		batch.Pending, err = queryAll(ctx, tx, func(r *sql.Rows) (m XMPMark, err error) {
			err = r.Scan(&m.AssetID, &m.Revision)
			return m, err
		}, "SELECT asset_id, revision FROM xmp_dirty WHERE asset_id > ? ORDER BY asset_id LIMIT ?", after, limit)
		if err != nil || len(batch.Pending) == 0 {
			return err
		}
		ids, err := xmpMarkIDs(batch.Pending)
		if err != nil {
			return err
		}
		batch.Assets, err = queryAll(ctx, tx, scanXMPAsset,
			xmpSelect+`WHERE id IN (SELECT value FROM json_each(?)) AND `+xmpEligible+` ORDER BY id`, ids)
		if err != nil || len(batch.Assets) == 0 {
			return err
		}
		return fillXMPAssets(ctx, tx, batch.Assets)
	})
	return batch, err
}

func xmpMarkIDs(marks []XMPMark) (string, error) {
	ids := make([]int64, len(marks))
	for i, m := range marks {
		ids[i] = m.AssetID
	}
	data, err := json.Marshal(ids)
	return string(data), err
}

// xmpHasRecord is true for an assets row with a sidecar Lumen wrote.
const xmpHasRecord = "EXISTS (SELECT 1 FROM xmp_exports e WHERE e.asset_id = assets.id)"

// fillXMPAssets adds the files, Things and faces to assets, three queries for
// all of them.
func fillXMPAssets(ctx context.Context, tx *sql.Tx, assets []XMPAsset) error {
	ids := make([]int64, len(assets))
	byID := make(map[int64]*XMPAsset, len(assets))
	var exportable []int64
	for i := range assets {
		ids[i], byID[assets[i].ID] = assets[i].ID, &assets[i]
		if assets[i].Exportable {
			exportable = append(exportable, assets[i].ID)
		}
	}
	all, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	// The sidecar's record is found by the path's fold key, the unique index.
	files, err := queryAll(ctx, tx, func(r *sql.Rows) (f struct {
		asset int64
		XMPFile
	}, err error) {
		var size, mtime sql.NullInt64
		if err = r.Scan(&f.asset, &f.Path, &f.Wrote, &size, &mtime); err == nil && size.Valid {
			f.Skip = &XMPStamp{Size: size.Int64, MTime: mtime.Int64}
		}
		return f, err
	}, `SELECT f.asset_id, f.path, x.hash, k.size, k.mtime FROM files f
		LEFT JOIN xmp_exports x ON `+foldKey("x.path")+` = `+foldKey("f.path || '"+XMPSidecarSuffix+"'")+`
		LEFT JOIN xmp_skips k ON `+foldKey("k.path")+` = `+foldKey("f.path || '"+XMPSidecarSuffix+"'")+`
		WHERE f.asset_id IN (SELECT value FROM json_each(?)) ORDER BY f.asset_id, f.id`, string(all))
	if err != nil {
		return err
	}
	for _, f := range files {
		a := byID[f.asset]
		a.Files = append(a.Files, f.XMPFile)
	}
	if len(exportable) == 0 {
		return nil
	}
	list, err := json.Marshal(exportable)
	if err != nil {
		return err
	}
	// Hidden Things are the sinks ("other objects"): never shown, never exported.
	tags, err := queryAll(ctx, tx, func(r *sql.Rows) (t struct {
		asset int64
		name  string
	}, err error) {
		err = r.Scan(&t.asset, &t.name)
		return t, err
	}, `SELECT x.asset_id, t.name FROM asset_tags x JOIN tags t ON t.id = x.tag_id
		WHERE x.asset_id IN (SELECT value FROM json_each(?)) AND t.hidden = 0
		AND `+tagAppliesTo("x", tagConfidenceSQL)+` AND NOT `+labelBlocked(core.BlockTag, "t.id")+`
		ORDER BY x.asset_id, t.name`, string(list))
	if err != nil {
		return err
	}
	for _, t := range tags {
		a := byID[t.asset]
		a.Tags = append(a.Tags, t.name)
	}
	faces, err := queryAll(ctx, tx, func(r *sql.Rows) (f struct {
		asset int64
		XMPFace
	}, err error) {
		err = r.Scan(&f.asset, &f.Name, &f.X, &f.Y, &f.W, &f.H)
		return f, err
	}, `SELECT f.asset_id, p.name, f.x, f.y, f.w, f.h FROM faces f JOIN people p ON p.id = f.person_id
		WHERE f.asset_id IN (SELECT value FROM json_each(?)) AND p.name IS NOT NULL
		AND NOT `+labelBlocked(core.BlockPerson, "p.id")+` AND `+faceShown+` -- a covered face's name never leaves the app
		ORDER BY f.asset_id, f.x, f.id`, string(list))
	if err != nil {
		return err
	}
	for _, f := range faces {
		a := byID[f.asset]
		a.Faces = append(a.Faces, f.XMPFace)
	}
	return nil
}

// XMPRecord is a sidecar Lumen wrote, or now owns.
type XMPRecord struct {
	AssetID int64
	Path    string // the sidecar's
	Hash    []byte // of the bytes in it
}

// XMPSkip is a sidecar left alone, and how it looked then.
type XMPSkip struct {
	Path string // the sidecar's
	XMPStamp
}

// XMPChanges is what an export batch changes in the store, saved together.
type XMPChanges struct {
	Completed []XMPMark   // work completed at its original snapshot revision
	Records   []XMPRecord // sidecars written, or taken as Lumen's own; replaces any record of the path
	Forget    []string    // sidecars (paths) Lumen no longer owns
	Skips     []XMPSkip   // sidecars left alone
	Unskip    []string    // sidecars (paths) that are no longer left alone
}

// Empty reports whether there is nothing to save.
func (c XMPChanges) Empty() bool {
	return len(c.Completed) == 0 && len(c.Records) == 0 && len(c.Forget) == 0 && len(c.Skips) == 0 && len(c.Unskip) == 0
}

// SaveXMP saves what an export batch did. A record of an asset deleted
// meanwhile is skipped. It is one transaction, so a sidecar is never both
// owned and left alone.
func (s *Store) SaveXMP(ctx context.Context, c XMPChanges) error {
	if c.Empty() {
		return nil
	}
	forget := "DELETE FROM xmp_exports WHERE " + foldPath + " = " + foldKey("?1")
	unskip := "DELETE FROM xmp_skips WHERE " + foldPath + " = " + foldKey("?1")
	record := `INSERT INTO xmp_exports (asset_id, path, hash)
		SELECT ?1, ?2, ?3 WHERE EXISTS (SELECT 1 FROM assets WHERE id = ?1)
		ON CONFLICT (` + foldPath + `) DO UPDATE SET asset_id = excluded.asset_id, path = excluded.path, hash = excluded.hash`
	skip := `INSERT INTO xmp_skips (path, size, mtime) VALUES (?1, ?2, ?3)
		ON CONFLICT (` + foldPath + `) DO UPDATE SET path = excluded.path, size = excluded.size, mtime = excluded.mtime`
	return s.tx(ctx, func(tx *sql.Tx) error {
		// Forgetting comes before recording, so a path in both ends up recorded.
		for _, p := range c.Forget {
			if _, err := tx.ExecContext(ctx, forget, p); err != nil {
				return err
			}
		}
		for _, p := range c.Unskip {
			if _, err := tx.ExecContext(ctx, unskip, p); err != nil {
				return err
			}
		}
		for _, r := range c.Records {
			if _, err := tx.ExecContext(ctx, record, r.AssetID, r.Path, r.Hash); err != nil {
				return err
			}
		}
		for _, k := range c.Skips {
			if _, err := tx.ExecContext(ctx, skip, k.Path, k.Size, k.MTime); err != nil {
				return err
			}
		}
		if len(c.Completed) > 0 {
			marks, err := json.Marshal(c.Completed)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `DELETE FROM xmp_dirty WHERE (asset_id, revision) IN
				(SELECT json_extract(value, '$.AssetID'), json_extract(value, '$.Revision') FROM json_each(?))`, string(marks))
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// XMPOwned lists up to limit of the sidecars Lumen wrote, every one it has a
// record of, with a Row above after, in Row order. It is what deleting them
// all reads.
func (s *Store) XMPOwned(ctx context.Context, after int64, limit int) ([]XMPOrphan, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (o XMPOrphan, err error) {
		err = r.Scan(&o.Row, &o.Path, &o.Hash)
		return o, err
	}, `SELECT rowid, path, hash FROM xmp_exports WHERE rowid > ?1 ORDER BY rowid LIMIT ?2`, after, limit)
}

// XMPOrphan is a sidecar Lumen wrote that is no longer beside any file of the
// library, and whose photo is gone or may not be shown any more.
type XMPOrphan struct {
	Row  int64 // for paging
	Path string
	Hash []byte // of what Lumen wrote
}

// XMPOrphans lists up to limit such sidecars with a Row above after, in Row
// order. They come about when the user moves or deletes a photo outside Lumen
// (its sidecar stays behind) or blocks it. A moved photo that can still be
// shown is not listed: its old sidecar is stale but harmless, and is left as
// it is.
func (s *Store) XMPOrphans(ctx context.Context, after int64, limit int) ([]XMPOrphan, error) {
	return s.xmpOrphans(ctx, after, limit, nil)
}

// XMPDirtyOrphans reads abandoned sidecars only for this batch's marked assets.
func (s *Store) XMPDirtyOrphans(ctx context.Context, marks []XMPMark) ([]XMPOrphan, error) {
	if len(marks) == 0 {
		return nil, nil
	}
	return s.xmpOrphans(ctx, 0, -1, marks)
}

func (s *Store) xmpOrphans(ctx context.Context, after int64, limit int, marks []XMPMark) ([]XMPOrphan, error) {
	filter := ""
	args := []any{after, limit}
	if marks != nil {
		ids, err := xmpMarkIDs(marks)
		if err != nil {
			return nil, err
		}
		filter = " AND e.asset_id IN (SELECT value FROM json_each(?3))"
		args = append(args, ids)
	}
	return queryAll(ctx, s.r, func(r *sql.Rows) (o XMPOrphan, err error) {
		err = r.Scan(&o.Row, &o.Path, &o.Hash)
		return o, err
	}, `SELECT e.rowid, e.path, e.hash FROM xmp_exports e
		WHERE e.rowid > ?1`+filter+`
		AND NOT EXISTS (SELECT 1 FROM files f
			WHERE `+foldKey("f.path")+` = `+foldKey("substr(e.path, 1, length(e.path) - "+strconv.Itoa(len(XMPSidecarSuffix))+")")+`)
		AND NOT EXISTS (SELECT 1 FROM assets WHERE id = e.asset_id AND `+visible+`)
		ORDER BY e.rowid LIMIT ?2`, args...)
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"

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
	Path  string // the file's; its sidecar is Path + XMPSidecarSuffix
	Wrote []byte // hash of what Lumen last wrote to the sidecar; nil if it never has
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

// XMPAssets returns up to limit assets with an id above after, in id order, that
// have something to export or a sidecar to bring back to the marker. It is the
// one place export reads photos, Things and people through, with the same
// predicates as browsing: visible assets only, no Thing or person a block
// covers, and people only when named. The three reads share a snapshot.
//
// Asset ids above the last one returned are for the next call; fewer than
// limit assets means there are no more.
func (s *Store) XMPAssets(ctx context.Context, after int64, limit int) ([]XMPAsset, error) {
	var assets []XMPAsset
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		assets, err = queryAll(ctx, tx, func(r *sql.Rows) (a XMPAsset, err error) {
			err = r.Scan(&a.ID, &a.Exportable, &a.Video, &a.Favourite, &a.Width, &a.Height)
			return a, err
		}, `SELECT id, `+visible+`, kind = 'video', favourite, COALESCE(width, 0), COALESCE(height, 0)
			FROM assets
			WHERE id > ?1 AND (
				(`+visible+` AND (favourite = 1 OR `+xmpHasRecord+`
					OR EXISTS (SELECT 1 FROM asset_tags x WHERE x.asset_id = assets.id AND `+tagAppliesTo("x", tagConfidenceSQL)+`)
					OR EXISTS (SELECT 1 FROM faces f JOIN people p ON p.id = f.person_id
						WHERE f.asset_id = assets.id AND p.name IS NOT NULL)))
				OR (`+present+` AND `+xmpHasRecord+`))
			ORDER BY id LIMIT ?2`, after, limit)
		if err != nil || len(assets) == 0 {
			return err
		}
		return fillXMPAssets(ctx, tx, assets)
	})
	return assets, err
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
		err = r.Scan(&f.asset, &f.Path, &f.Wrote)
		return f, err
	}, `SELECT f.asset_id, f.path, x.hash FROM files f
		LEFT JOIN xmp_exports x ON `+foldKey("x.path")+` = `+foldKey("f.path || '"+XMPSidecarSuffix+"'")+`
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
		AND NOT `+labelBlocked(core.BlockPerson, "p.id")+`
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

// XMPRecord is a sidecar Lumen has just written.
type XMPRecord struct {
	AssetID int64
	Path    string // the sidecar's
	Hash    []byte // of the bytes written
}

// SaveXMPExports records the sidecars Lumen wrote, replacing any earlier record
// of the same path (the file may now sit beside another asset). An asset
// deleted meanwhile is skipped.
func (s *Store) SaveXMPExports(ctx context.Context, rs []XMPRecord) error {
	if len(rs) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		st, err := tx.PrepareContext(ctx, `INSERT INTO xmp_exports (asset_id, path, hash)
			SELECT ?1, ?2, ?3 WHERE EXISTS (SELECT 1 FROM assets WHERE id = ?1)
			ON CONFLICT (`+foldPath+`) DO UPDATE SET asset_id = excluded.asset_id, path = excluded.path, hash = excluded.hash`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, r := range rs {
			if _, err := st.ExecContext(ctx, r.AssetID, r.Path, r.Hash); err != nil {
				return err
			}
		}
		return nil
	})
}

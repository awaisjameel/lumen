package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"lumen/internal/core"
)

// ErrNotFound is returned when an asset does not exist or is not visible.
var ErrNotFound = errors.New("not found")

// DayBuckets counts visible assets per calendar day, newest day first. The
// grid builds its entire layout, and the timeline scrubber, from this list.
func (s *Store) DayBuckets(ctx context.Context, f core.Filter) ([]core.DayBucket, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT strftime('%Y-%m-%d', taken_at, 'unixepoch') AS day, count(*)
		FROM assets WHERE `+visible+filterSQL(f)+` GROUP BY day ORDER BY day DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.DayBucket
	for rows.Next() {
		var b core.DayBucket
		if err := rows.Scan(&b.Day, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// filterSQL is an extra AND clause for a grid filter.
func filterSQL(f core.Filter) string {
	switch f {
	case core.FilterFavourites:
		return " AND favourite = 1"
	case core.FilterVideos:
		return " AND kind = 'video'"
	}
	return ""
}

// DayItems returns the grid items for the days from..to inclusive
// (YYYY-MM-DD, either order), grouped by day, newest first.
func (s *Store) DayItems(ctx context.Context, from, to string, f core.Filter) ([]core.DayItems, error) {
	a, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return nil, err
	}
	b, err := time.Parse(time.DateOnly, to)
	if err != nil {
		return nil, err
	}
	if a.After(b) {
		a, b = b, a
	}
	rows, err := s.r.QueryContext(ctx, `SELECT id, taken_at, COALESCE(width, 0), COALESCE(height, 0),
			kind = 'video', favourite, thumb_version
		FROM assets WHERE `+visible+filterSQL(f)+` AND taken_at >= ? AND taken_at < ?
		ORDER BY taken_at DESC, id DESC`, a.Unix(), b.AddDate(0, 0, 1).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.DayItems
	for rows.Next() {
		var it core.GridItem
		var takenAt int64
		if err := rows.Scan(&it.ID, &takenAt, &it.W, &it.H, &it.Video, &it.Favourite, &it.Thumb); err != nil {
			return nil, err
		}
		day := time.Unix(takenAt, 0).UTC().Format(time.DateOnly)
		if n := len(out); n == 0 || out[n-1].Day != day {
			out = append(out, core.DayItems{Day: day})
		}
		out[len(out)-1].Items = append(out[len(out)-1].Items, it)
	}
	return out, rows.Err()
}

// AssetDetail returns everything the details panel shows for one asset.
func (s *Store) AssetDetail(ctx context.Context, id int64) (core.AssetDetail, error) {
	var d core.AssetDetail
	var w, h sql.NullInt64
	var camera sql.NullString
	var lat, lon sql.NullFloat64
	err := s.r.QueryRowContext(ctx, `SELECT id, kind, ext, taken_at, date_from, width, height, camera, lat, lon, favourite
		FROM assets WHERE id = ? AND missing_since IS NULL`, id).
		Scan(&d.ID, &d.Kind, &d.Ext, &d.TakenAt, &d.DateFrom, &w, &h, &camera, &lat, &lon, &d.Favourite)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.Width, d.Height, d.Camera = int(w.Int64), int(h.Int64), camera.String
	if lat.Valid && lon.Valid {
		d.Lat, d.Lon = &lat.Float64, &lon.Float64
	}
	d.WebNative = core.WebNative(d.Ext)

	rows, err := s.r.QueryContext(ctx, "SELECT path, size FROM files WHERE asset_id = ? ORDER BY path", id)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p, &d.Size); err != nil {
			return d, err
		}
		d.Paths = append(d.Paths, p)
	}
	return d, rows.Err()
}

// MediaFile is what the media handler needs to serve an asset.
type MediaFile struct {
	Path         string
	Ext          string
	Kind         core.Kind
	Orientation  int
	ThumbVersion int
}

// AssetFile returns the first file on disk for an asset.
func (s *Store) AssetFile(ctx context.Context, id int64) (MediaFile, error) {
	var m MediaFile
	err := s.r.QueryRowContext(ctx, `SELECT f.path, a.ext, a.kind, a.orientation, a.thumb_version
		FROM assets a JOIN files f ON f.asset_id = a.id WHERE a.id = ? LIMIT 1`, id).
		Scan(&m.Path, &m.Ext, &m.Kind, &m.Orientation, &m.ThumbVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// SetFavourite marks or unmarks an asset as a favourite.
func (s *Store) SetFavourite(ctx context.Context, id int64, fav bool) error {
	_, err := s.w.ExecContext(ctx, "UPDATE assets SET favourite = ? WHERE id = ?", fav, id)
	return err
}

// CountVisible returns how many assets the grid shows.
func (s *Store) CountVisible(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+visible).Scan(&n)
	return n, err
}

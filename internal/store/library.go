package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"lumen/internal/core"
)

// ErrNotFound is returned when an asset, album, person, tag or face does not
// exist (or no longer does), or an asset is not visible.
var ErrNotFound = errors.New("not found")

// UnavailableAssets identifies stored vectors that must not compete for search
// results. Keep their embeddings so restored sources become searchable again.
func (s *Store) UnavailableAssets(ctx context.Context) (map[int64]bool, error) {
	ids, err := queryAll(ctx, s.r, scanID, "SELECT id FROM assets WHERE NOT ("+visible+")")
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// VisibilityRevision changes whenever which assets are visible can change:
// assets coming, going, hidden or missing, and blocks or anything a block
// covers by (migration 0016). Search checks it before reusing its
// excluded-ID cache; the indexer, before saying only tags or people changed.
func (s *Store) VisibilityRevision(ctx context.Context) (int64, error) {
	var revision int64
	err := s.r.QueryRowContext(ctx, "SELECT visible_value FROM event_revision WHERE id = 1").Scan(&revision)
	return revision, err
}

// DayBuckets counts the view's assets per calendar day, newest day first.
// The grid builds its entire layout, and the timeline scrubber, from this
// list. AI tags count from minScore up.
func (s *Store) DayBuckets(ctx context.Context, v core.View, minScore float64) ([]core.DayBucket, error) {
	where, args, err := s.viewSQL(ctx, v, minScore)
	if err != nil {
		return nil, err
	}
	return queryAll(ctx, s.r, func(r *sql.Rows) (b core.DayBucket, err error) {
		err = r.Scan(&b.Day, &b.Count)
		return b, err
	}, `SELECT strftime('%Y-%m-%d', taken_at, 'unixepoch') AS day, count(*)
		FROM assets WHERE `+where+` GROUP BY day ORDER BY day DESC`, args...)
}

// viewSQL is the WHERE clause (and its arguments) selecting a view's assets.
func (s *Store) viewSQL(ctx context.Context, v core.View, minScore float64) (where string, args []any, err error) {
	for _, target := range []struct {
		kind core.BlockKind
		id   int64
	}{{core.BlockTag, v.Tag}, {core.BlockPerson, v.Person}} {
		if target.id == 0 {
			continue
		}
		blocked, err := s.IsBlocked(ctx, target.kind, target.id)
		if err != nil {
			return "", nil, err
		}
		if blocked {
			return "", nil, ErrNotFound
		}
	}
	if v.Album != 0 {
		a, err := album(ctx, s.r, v.Album)
		if err != nil {
			return "", nil, err
		}
		where, args = albumFilter(a).where(minScore)
		return where, args, nil
	}
	if v.Place != 0 {
		var ok bool
		if err := s.r.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM places WHERE id = ? AND NOT "+placeBlocked("places.id")+")",
			v.Place).Scan(&ok); err != nil {
			return "", nil, err
		}
		if !ok {
			return "", nil, ErrNotFound
		}
	}
	f := assetFilter{tag: v.Tag, person: v.Person, place: v.Place}
	if v.Event != 0 {
		gap, err := s.EventGapHours(ctx)
		if err != nil {
			return "", nil, err
		}
		event, err := s.Event(ctx, v.Event, gap)
		if err != nil {
			return "", nil, err
		}
		f.event = &event
	}
	switch v.Filter {
	case core.FilterAll:
	case core.FilterFavourites:
		f.favourites = true
	case core.FilterVideos:
		f.kind = core.KindVideo
	}
	where, args = f.where(minScore)
	return where, args, nil
}

// assetFilter narrows the visible assets, for a view of the timeline or a
// smart album's rule. Zero fields match everything.
type assetFilter struct {
	kind       core.Kind
	favourites bool
	from, to   string // inclusive calendar days, YYYY-MM-DD (validated by the caller)
	tag        int64  // carrying this tag (AI tags from minScore up)
	person     int64  // showing this person
	place      int64  // near this city
	event      *core.Event
	album      int64 // in this manual album
}

// where is the filter as a WHERE clause for the assets table, with only
// bound values. It starts with visible, so the grid's queries use the
// assets_timeline index.
func (f assetFilter) where(minScore float64) (where string, args []any) {
	where, args = visible, []any{}
	if f.kind != "" {
		where += " AND kind = ?"
		args = append(args, f.kind)
	}
	if f.favourites {
		where += " AND favourite = 1"
	}
	if day, err := time.Parse(time.DateOnly, f.from); err == nil {
		where += " AND taken_at >= ?"
		args = append(args, day.Unix())
	}
	if day, err := time.Parse(time.DateOnly, f.to); err == nil {
		where += " AND taken_at < ?"
		args = append(args, day.AddDate(0, 0, 1).Unix())
	}
	if f.tag != 0 {
		where += " AND id IN (SELECT x.asset_id FROM asset_tags x WHERE x.tag_id = ? AND " + tagApplies + ")"
		args = append(args, f.tag, minScore)
	}
	if f.person != 0 {
		where += " AND id IN (SELECT asset_id FROM faces WHERE person_id = ?)"
		args = append(args, f.person)
	}
	if f.place != 0 {
		where += " AND place_id = ?"
		args = append(args, f.place)
	}
	if f.event != nil {
		e := f.event
		where += " AND taken_at >= ? AND taken_at <= ?"
		where += " AND (taken_at > ? OR (taken_at = ? AND id >= ?))"
		where += " AND (taken_at < ? OR (taken_at = ? AND id <= ?))"
		args = append(args, e.StartAt, e.EndAt, e.StartAt, e.StartAt, e.FirstID, e.EndAt, e.EndAt, e.LastID)
	}
	if f.album != 0 {
		where += " AND id IN (SELECT asset_id FROM album_items WHERE album_id = ?)"
		args = append(args, f.album)
	}
	return where, args
}

// DayItems returns the view's grid items for the days from..to inclusive
// (YYYY-MM-DD, either order), grouped by day, newest first.
func (s *Store) DayItems(ctx context.Context, from, to string, v core.View, minScore float64) ([]core.DayItems, error) {
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
	where, args, err := s.viewSQL(ctx, v, minScore)
	if err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT id, taken_at, `+gridColumns+`
		FROM assets WHERE `+where+` AND taken_at >= ? AND taken_at < ?
		ORDER BY taken_at DESC, id DESC`, append(args, a.Unix(), b.AddDate(0, 0, 1).Unix())...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.DayItems{}
	for rows.Next() {
		var it core.GridItem
		var takenAt int64
		if err := rows.Scan(append([]any{&it.ID, &takenAt}, gridFields(&it)...)...); err != nil {
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

// gridColumns are the columns a core.GridItem needs besides its id, in
// gridFields order.
const gridColumns = "COALESCE(width, 0), COALESCE(height, 0), kind = 'video', COALESCE(duration_ms, 0), favourite, thumb_version"

func gridFields(it *core.GridItem) []any {
	return []any{&it.W, &it.H, &it.Video, &it.Duration, &it.Favourite, &it.Thumb}
}

// GridItems returns the visible assets among ids, in the order given
// (search results arrive best match first).
func (s *Store) GridItems(ctx context.Context, ids []int64) ([]core.GridItem, error) {
	if len(ids) == 0 {
		return []core.GridItem{}, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT id, `+gridColumns+`
		FROM assets WHERE id IN (SELECT value FROM json_each(?)) AND `+visible, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make(map[int64]core.GridItem, len(ids))
	for rows.Next() {
		var it core.GridItem
		if err := rows.Scan(append([]any{&it.ID}, gridFields(&it)...)...); err != nil {
			return nil, err
		}
		found[it.ID] = it
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]core.GridItem, 0, len(found))
	for _, id := range ids {
		if it, ok := found[id]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}

// AssetDetail returns everything the details panel shows for one asset.
func (s *Store) AssetDetail(ctx context.Context, id int64) (core.AssetDetail, error) {
	var d core.AssetDetail
	var w, h sql.NullInt64
	var camera sql.NullString
	var duration sql.NullInt64
	var videoCodec, audioCodec sql.NullString
	var lat, lon sql.NullFloat64
	err := s.r.QueryRowContext(ctx, `SELECT id, kind, ext, taken_at, date_from, width, height, camera, lat, lon,
		CASE WHEN `+placeBlocked("assets.place_id")+` THEN 0 ELSE COALESCE(place_id, 0) END, favourite,
		duration_ms, video_codec, audio_codec
		FROM assets WHERE id = ? AND `+visible, id).
		Scan(&d.ID, &d.Kind, &d.Ext, &d.TakenAt, &d.DateFrom, &w, &h, &camera, &lat, &lon, &d.PlaceID, &d.Favourite,
			&duration, &videoCodec, &audioCodec)
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
	if d.Kind == core.KindVideo {
		d.Video = &core.Video{Duration: duration.Int64, Codec: videoCodec.String, Audio: audioCodec.String,
			Play: core.PlaybackOf(d.Ext, videoCodec.String, audioCodec.String)}
	}

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
	Paths        []string // every copy on disk, the primary one (see primaryFile) first
	Ext          string
	Kind         core.Kind
	Orientation  int
	ThumbVersion int
	VideoCodec   string
	AudioCodec   string
	Duration     int64
}

// AssetFile returns what serving an asset needs, with every file it is in:
// if one copy is on a drive that is not plugged in, another may be.
func (s *Store) AssetFile(ctx context.Context, id int64) (MediaFile, error) {
	var m MediaFile
	err := s.read(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT ext, kind, orientation, thumb_version,
			COALESCE(video_codec, ''), COALESCE(audio_codec, ''), COALESCE(duration_ms, 0)
			FROM assets WHERE id = ? AND `+visible, id).
			Scan(&m.Ext, &m.Kind, &m.Orientation, &m.ThumbVersion, &m.VideoCodec, &m.AudioCodec, &m.Duration)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		m.Paths, err = queryAll(ctx, tx, func(r *sql.Rows) (p string, err error) {
			err = r.Scan(&p)
			return p, err
		}, "SELECT path FROM files WHERE asset_id = ? ORDER BY id", id)
		if err == nil && len(m.Paths) == 0 {
			err = ErrNotFound
		}
		return err
	})
	return m, err
}

// SetFavourite marks or unmarks an asset as a favourite.
func (s *Store) SetFavourite(ctx context.Context, id int64, fav bool) error {
	return affected(s.w.ExecContext(ctx, "UPDATE assets SET favourite = ? WHERE id = ? AND "+present, fav, id))
}

// CountVisible returns how many assets the grid shows.
func (s *Store) CountVisible(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+visible).Scan(&n)
	return n, err
}

// AssetVisible checks the same browse predicate used by grid queries. Media
// handlers call this before serving even a cached thumbnail or video sprite.
func (s *Store) AssetVisible(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := s.r.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM assets WHERE id = ? AND "+visible+")", id).Scan(&ok)
	return ok, err
}

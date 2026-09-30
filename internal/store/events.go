package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"lumen/internal/core"
)

type eventAsset struct {
	id, at, place int64
	thumb         int
	lat, lon      sql.NullFloat64
}

// EventGapHours returns the validated quiet gap used by all event views.
func (s *Store) EventGapHours(ctx context.Context) (int, error) {
	hours, err := s.Int(ctx, core.SettingEventGapHours, core.EventGapDefault)
	if hours < core.EventGapMin || hours > core.EventGapMax {
		hours = core.EventGapDefault
	}
	return hours, err
}

// Events groups the currently visible timeline without changing original
// files. A group starts after a quiet gap or a jump of more than 100 km.
// Grouping on read means new metadata, removed sources and restored files take
// effect without a fragile second indexing queue.
func (s *Store) Events(ctx context.Context, gapHours int) ([]core.Event, error) {
	if gapHours < core.EventGapMin || gapHours > core.EventGapMax {
		gapHours = core.EventGapDefault
	}
	s.eventsCache.Lock()
	defer s.eventsCache.Unlock()
	var revision, coverRevision int64
	var out []core.Event
	err := s.read(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT value, cover_value FROM event_revision WHERE id = 1").Scan(&revision, &coverRevision); err != nil {
			return err
		}
		if s.eventsCache.loaded && s.eventsCache.revision == revision && s.eventsCache.gap == gapHours {
			out = s.eventsCache.items
			if s.eventsCache.coverRevision != coverRevision {
				out = append([]core.Event(nil), out...)
				return refreshEventCovers(ctx, tx, out)
			}
			return nil
		}
		var err error
		out, err = groupEvents(ctx, tx, gapHours)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.eventsCache.revision = revision
	s.eventsCache.coverRevision = coverRevision
	s.eventsCache.gap = gapHours
	s.eventsCache.loaded = true
	s.eventsCache.items = out
	return append([]core.Event(nil), out...), nil
}

func groupEvents(ctx context.Context, tx *sql.Tx, gapHours int) ([]core.Event, error) {
	var out []core.Event
	places := map[int64]string{}
	type namedID struct {
		id   int64
		name string
	}
	placeRows, err := queryAll(ctx, tx, func(r *sql.Rows) (v namedID, err error) {
		err = r.Scan(&v.id, &v.name)
		return v, err
	}, "SELECT id, name FROM places WHERE NOT "+placeBlocked("places.id")) // a blocked place names no event
	if err != nil {
		return nil, err
	}
	for _, row := range placeRows {
		places[row.id] = row.name
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id, a.taken_at, a.lat, a.lon, COALESCE(a.place_id, 0),
		a.thumb_version, COALESCE(n.name, '') FROM assets a
		LEFT JOIN event_names n ON n.anchor_asset_id = a.id WHERE `+visibleA+` ORDER BY a.taken_at, a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var previous, lastLocated eventAsset
	var current core.Event
	var placeCounts map[int64]int
	var customName string
	finish := func() {
		if current.ID == 0 {
			return
		}
		for id, n := range placeCounts {
			if n > placeCounts[current.PlaceID] || (n == placeCounts[current.PlaceID] && id < current.PlaceID) {
				current.PlaceID = id
			}
		}
		current.Name = customName
		if current.Name == "" {
			current.Name = eventName(places[current.PlaceID], current.StartAt, current.EndAt)
		}
		out = append(out, current)
	}
	for rows.Next() {
		var a eventAsset
		var name string
		if err := rows.Scan(&a.id, &a.at, &a.lat, &a.lon, &a.place, &a.thumb, &name); err != nil {
			return nil, err
		}
		if current.ID == 0 || a.at-previous.at > int64(gapHours)*3600 || locationJump(lastLocated, a) {
			finish()
			current = core.Event{ID: a.id, FirstID: a.id, StartAt: a.at}
			placeCounts = map[int64]int{}
			customName = ""
			lastLocated = eventAsset{}
		}
		if customName == "" {
			customName = name
		}
		current.LastID, current.EndAt = a.id, a.at
		current.Count++
		if a.thumb > 0 {
			current.Cover, current.CoverThumb = a.id, a.thumb
		}
		if _, named := places[a.place]; named {
			placeCounts[a.place]++
		}
		previous = a
		if a.lat.Valid && a.lon.Valid {
			lastLocated = a
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	finish()
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func refreshEventCovers(ctx context.Context, tx *sql.Tx, events []core.Event) error {
	if len(events) == 0 {
		return nil
	}
	where, _ := (assetFilter{event: &core.Event{}}).where(0)
	cover, err := tx.PrepareContext(ctx, "SELECT id, thumb_version FROM assets WHERE "+where+
		" AND thumb_version > 0 ORDER BY taken_at DESC, id DESC LIMIT 1")
	if err != nil {
		return err
	}
	defer cover.Close()
	for i := range events {
		event := &events[i]
		_, args := (assetFilter{event: event}).where(0)
		event.Cover, event.CoverThumb = 0, 0
		err := cover.QueryRowContext(ctx, args...).Scan(&event.Cover, &event.CoverThumb)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

func locationJump(a, b eventAsset) bool {
	if !a.lat.Valid || !a.lon.Valid || !b.lat.Valid || !b.lon.Valid {
		return false
	}
	x := math.Sin((b.lat.Float64 - a.lat.Float64) * math.Pi / 360)
	y := math.Sin((b.lon.Float64 - a.lon.Float64) * math.Pi / 360)
	h := x*x + math.Cos(a.lat.Float64*math.Pi/180)*math.Cos(b.lat.Float64*math.Pi/180)*y*y
	return 2*6371*math.Asin(math.Sqrt(math.Min(1, h))) > 100
}

func eventName(place string, start, end int64) string {
	a, b := time.Unix(start, 0).UTC(), time.Unix(end, 0).UTC()
	date := a.Format("2 Jan 2006")
	if a.Format(time.DateOnly) != b.Format(time.DateOnly) {
		date += " – " + b.Format("2 Jan 2006")
	}
	if place != "" {
		return place + ", " + date
	}
	return date
}

// Event returns the current group containing id. Old event links keep working
// when an earlier asset later joins the group, as long as id remains visible.
func (s *Store) Event(ctx context.Context, id int64, gapHours int) (core.Event, error) {
	var at int64
	err := s.r.QueryRowContext(ctx, "SELECT taken_at FROM assets WHERE id = ? AND "+visible, id).Scan(&at)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Event{}, ErrNotFound
		}
		return core.Event{}, err
	}
	events, err := s.Events(ctx, gapHours)
	if err != nil {
		return core.Event{}, err
	}
	for _, event := range events {
		if (at > event.StartAt || (at == event.StartAt && id >= event.FirstID)) &&
			(at < event.EndAt || (at == event.EndAt && id <= event.LastID)) {
			return event, nil
		}
	}
	return core.Event{}, ErrNotFound
}

// SetEventName changes an event's displayed name. An empty name restores the
// generated date/place title. The name follows the current members through
// splits and anchor removal; newly joining media inherits it while grouped.
func (s *Store) SetEventName(ctx context.Context, id int64, name string, gapHours int) error {
	name = strings.TrimSpace(name)
	if name != "" {
		var err error
		name, err = cleanName(name)
		if err != nil {
			return err
		}
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		event, err := s.Event(ctx, id, gapHours)
		if err != nil {
			return err
		}
		where, args := (assetFilter{event: &event}).where(0)
		if name == "" {
			_, err = tx.ExecContext(ctx, "DELETE FROM event_names WHERE anchor_asset_id IN (SELECT id FROM assets WHERE "+where+")", args...)
		} else {
			_, err = tx.ExecContext(ctx, "INSERT INTO event_names(anchor_asset_id, name) SELECT id, ? FROM assets WHERE "+where+
				" ON CONFLICT(anchor_asset_id) DO UPDATE SET name = excluded.name", append([]any{name}, args...)...)
		}
		return err
	})
}

package store

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
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
	return validGap(hours), err
}

// validGap is hours if it is a gap the settings allow, else the default.
func validGap(hours int) int {
	if hours < core.EventGapMin || hours > core.EventGapMax {
		return core.EventGapDefault
	}
	return hours
}

// Events groups the currently visible timeline without changing original
// files. A group starts after a quiet gap or a jump of more than 100 km.
// Grouping on read means new metadata, removed sources and restored files take
// effect without a fragile second indexing queue.
func (s *Store) Events(ctx context.Context, gapHours int) ([]core.Event, error) {
	gapHours = validGap(gapHours)
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
	tagged, err := eventTagRefs(ctx, tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id, a.taken_at, a.lat, a.lon, COALESCE(a.place_id, 0),
		a.thumb_version FROM assets a WHERE `+visibleA+` ORDER BY a.taken_at, a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	split := eventSplitter{gap: int64(gapHours) * 3600}
	var current core.Event
	var placeCounts map[int64]int
	var nameTag, pinnedAt int64 // the name tag shown (the lowest id) and the newest pin held
	var customName string
	var pins []int64 // pinnedAt of each event in out
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
		current.Pinned = pinnedAt != 0
		out = append(out, current)
		pins = append(pins, pinnedAt)
	}
	for rows.Next() {
		var a eventAsset
		if err := rows.Scan(&a.id, &a.at, &a.lat, &a.lon, &a.place, &a.thumb); err != nil {
			return nil, err
		}
		if split.next(a) {
			finish()
			current = core.Event{ID: a.id, FirstID: a.id, StartAt: a.at}
			placeCounts = map[int64]int{}
			nameTag, pinnedAt, customName = 0, 0, ""
		}
		for _, ref := range tagged[a.id] {
			switch ref.kind {
			case eventTagName:
				if nameTag == 0 || ref.id < nameTag {
					nameTag, customName = ref.id, ref.name
				}
			case eventTagPin:
				pinnedAt = max(pinnedAt, ref.createdAt)
			}
		}
		current.LastID, current.EndAt = a.id, a.at
		current.Count++
		if a.thumb > 0 {
			current.Cover, current.CoverThumb = a.id, a.thumb
		}
		if _, named := places[a.place]; named {
			placeCounts[a.place]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	finish()
	// Newest first, with pinned events ahead of the rest, the newest pin first.
	slices.Reverse(out)
	slices.Reverse(pins)
	if slices.ContainsFunc(pins, func(at int64) bool { return at != 0 }) {
		order := make([]int, len(out))
		for i := range order {
			order[i] = i
		}
		slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(pins[b], pins[a]) })
		sorted := make([]core.Event, len(out))
		for i, from := range order {
			sorted[i] = out[from]
		}
		out = sorted
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

// eventSplitter decides where one event ends and the next begins, for photos
// fed to it in timeline order. A new event starts after a quiet gap, or when
// a photo is more than 100 km from the last located one in its event. Both
// grouping a timeline (groupEvents) and settling event tags use it, so the
// two can never disagree about an event.
type eventSplitter struct {
	gap         int64 // seconds of quiet that end an event
	started     bool
	previous    eventAsset
	lastLocated eventAsset
}

// next feeds the next photo and reports whether it starts an event.
func (p *eventSplitter) next(a eventAsset) bool {
	starts := !p.started || a.at-p.previous.at > p.gap || locationJump(p.lastLocated, a)
	if starts {
		p.started = true
		p.lastLocated = eventAsset{}
	}
	p.previous = a
	if a.lat.Valid && a.lon.Valid {
		p.lastLocated = a
	}
	return starts
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
// generated date/place title. The name is a tag held by the event's photos,
// so it follows them through splits and the removal of any one of them, and
// photos that later join the event take it (see settleEvents). The new name
// wins over any other the event's photos held.
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
		where, args := eventAssets(event)
		if _, err := tx.ExecContext(ctx, `DELETE FROM event_members
			WHERE tag_id IN (SELECT id FROM event_tags WHERE kind = 'name')
			AND asset_id IN (SELECT id FROM assets WHERE `+where+")", args...); err != nil {
			return err
		}
		if name != "" {
			res, err := tx.ExecContext(ctx, "INSERT INTO event_tags (kind, name, created_at) VALUES ('name', ?, ?)", name, time.Now().Unix())
			if err != nil {
				return err
			}
			tag, err := res.LastInsertId()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO event_members (tag_id, asset_id) SELECT ?, id FROM assets WHERE "+where,
				append([]any{tag}, args...)...); err != nil {
				return err
			}
		}
		return dropEmptyEventTags(ctx, tx)
	})
}

// SetEventPinned pins or unpins the event holding asset id. A pin is a tag
// held by the event's photos, so it follows the event as it regroups. Pinning
// an event that is pinned already keeps its place among the pins.
func (s *Store) SetEventPinned(ctx context.Context, id int64, pinned bool, gapHours int) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		event, err := s.Event(ctx, id, gapHours)
		if err != nil {
			return err
		}
		where, args := eventAssets(event)
		if !pinned {
			if _, err := tx.ExecContext(ctx, `DELETE FROM event_members
				WHERE tag_id IN (SELECT id FROM event_tags WHERE kind = 'pin')
				AND asset_id IN (SELECT id FROM assets WHERE `+where+")", args...); err != nil {
				return err
			}
			return dropEmptyEventTags(ctx, tx)
		}
		var held bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM event_members m
			JOIN event_tags t ON t.id = m.tag_id AND t.kind = 'pin'
			WHERE m.asset_id IN (SELECT id FROM assets WHERE `+where+"))", args...).Scan(&held); err != nil {
			return err
		}
		if held {
			return nil
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO event_tags (kind, created_at)
			SELECT 'pin', MAX(?, COALESCE(MAX(created_at), 0) + 1) FROM event_tags WHERE kind = 'pin'`, time.Now().Unix())
		if err != nil {
			return err
		}
		tag, err := res.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO event_members (tag_id, asset_id) SELECT ?, id FROM assets WHERE "+where,
			append([]any{tag}, args...)...)
		return err
	})
}

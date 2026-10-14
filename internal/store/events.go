package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"sort"
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
		var err error
		out, revision, coverRevision, err = s.eventsIn(ctx, tx, gapHours)
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

// eventsIn reads the journal and grouping in one snapshot. Its caller holds
// the list mutex, so an older reader cannot overwrite a newer cached list.
func (s *Store) eventsIn(ctx context.Context, tx *sql.Tx, gapHours int) (out []core.Event, revision, covers int64, err error) {
	var full, floor int64
	if err := tx.QueryRowContext(ctx, `SELECT r.value, r.full_value, r.floor, e.cover_value
		FROM event_list_revision r CROSS JOIN event_revision e WHERE r.id = 1 AND e.id = 1`).Scan(&revision, &full, &floor, &covers); err != nil {
		return nil, 0, 0, err
	}
	cache := &s.eventsCache
	fullRegroup := false
	switch {
	case !cache.loaded || cache.gap != gapHours || cache.revision < full || cache.revision < floor || cache.revision > revision:
		out, err = groupEvents(ctx, tx, gapHours, nil)
		fullRegroup = true
	case cache.revision != revision:
		out, err = regroupDirtyEvents(ctx, tx, gapHours, cache.revision, cache.items)
	default:
		out = cache.items
	}
	if err == nil && !fullRegroup && cache.coverRevision != covers {
		// A cover may have changed outside the regrouped windows. Copy before
		// refreshing so a failed read cannot partially change the cache.
		out = append([]core.Event(nil), out...)
		// Pruning retains at least 4033 cover revisions, up to 4096 between batches.
		if cache.coverRevision < covers-4033 || cache.coverRevision > covers {
			err = refreshEventCovers(ctx, tx, out)
		} else {
			err = refreshDirtyEventCovers(ctx, tx, out, cache.coverRevision)
		}
	}
	return out, revision, covers, err
}

func refreshDirtyEventCovers(ctx context.Context, tx *sql.Tx, events []core.Event, revision int64) error {
	times, err := queryAll(ctx, tx, scanID, "SELECT at FROM event_list_cover_dirty WHERE revision > ? ORDER BY at", revision)
	if err != nil {
		return err
	}
	var changed []core.Event
	var positions []int
	for i, event := range events {
		first := sort.Search(len(times), func(i int) bool { return times[i] >= event.StartAt })
		if first < len(times) && times[first] <= event.EndAt {
			changed = append(changed, event)
			positions = append(positions, i)
		}
	}
	if err := refreshEventCovers(ctx, tx, changed); err != nil {
		return err
	}
	for i, position := range positions {
		events[position].Cover = changed[i].Cover
		events[position].CoverThumb = changed[i].CoverThumb
	}
	return nil
}

// regroupDirtyEvents expands through both the old events and the new runs.
// Deleting a bridge can split an old event beyond the run reached from its
// former date; adding a bridge can join runs beyond the old event's edges.
func regroupDirtyEvents(ctx context.Context, tx *sql.Tx, gapHours int, revision int64, cached []core.Event) ([]core.Event, error) {
	times, err := queryAll(ctx, tx, scanID, "SELECT at FROM event_list_dirty WHERE revision > ? ORDER BY at", revision)
	if err != nil {
		return nil, err
	}
	chronological := append([]core.Event(nil), cached...)
	slices.SortFunc(chronological, func(a, b core.Event) int {
		if order := cmp.Compare(a.StartAt, b.StartAt); order != 0 {
			return order
		}
		return cmp.Compare(a.FirstID, b.FirstID)
	})
	st := newSettler(tx, int64(gapHours)*3600)
	var windows [][2]int64
	for _, at := range times {
		if n := len(windows); n > 0 && at <= windows[n-1][1] {
			continue
		}
		lo, hi := at, at
		for {
			before := [2]int64{lo, hi}
			// All events sharing a second must be included, regardless of id or
			// location, because the range query includes that whole second.
			first := sort.Search(len(chronological), func(i int) bool { return chronological[i].EndAt >= lo })
			for i := first; i < len(chronological) && chronological[i].StartAt <= hi; i++ {
				lo = min(lo, chronological[i].StartAt)
				hi = max(hi, chronological[i].EndAt)
			}
			left, _, err := st.runAround(ctx, lo)
			if err != nil {
				return nil, err
			}
			_, right, err := st.runAround(ctx, hi)
			if err != nil {
				return nil, err
			}
			lo, hi = left, right
			if before == [2]int64{lo, hi} {
				break
			}
		}
		if n := len(windows); n > 0 && lo <= windows[n-1][1] {
			windows[n-1][1] = max(windows[n-1][1], hi)
		} else {
			windows = append(windows, [2]int64{lo, hi})
		}
	}
	var retained, changed []core.Event
	for _, event := range cached {
		i := sort.Search(len(windows), func(i int) bool { return windows[i][1] >= event.StartAt })
		if i == len(windows) || windows[i][0] > event.EndAt {
			retained = append(retained, event)
		}
	}
	for _, window := range windows {
		events, err := groupEvents(ctx, tx, gapHours, &window)
		if err != nil {
			return nil, err
		}
		changed = append(changed, events...)
	}
	return orderSplicedEvents(ctx, tx, retained, changed)
}

// orderSplicedEvents restores the full grouper's pin order without reading
// every photo's tags. Retained events already have the right relative order;
// binary insertion only looks up pins encountered around changed events.
func orderSplicedEvents(ctx context.Context, tx *sql.Tx, retained, changed []core.Event) ([]core.Event, error) {
	pins := make(map[int64]int64)
	pinOf := func(event core.Event) (int64, error) {
		if !event.Pinned {
			return 0, nil
		}
		if pin, found := pins[event.ID]; found {
			return pin, nil
		}
		pin, err := eventPin(ctx, tx, event)
		if err == nil {
			pins[event.ID] = pin
		}
		return pin, err
	}
	for _, event := range changed {
		if _, err := pinOf(event); err != nil {
			return nil, err
		}
	}
	compare := func(a, b core.Event, aPin, bPin int64) int {
		if order := cmp.Compare(bPin, aPin); order != 0 {
			return order
		}
		if order := cmp.Compare(b.StartAt, a.StartAt); order != 0 {
			return order
		}
		return cmp.Compare(b.FirstID, a.FirstID)
	}
	slices.SortFunc(changed, func(a, b core.Event) int {
		return compare(a, b, pins[a.ID], pins[b.ID])
	})
	insertions := make(map[int][]core.Event)
	for _, event := range changed {
		lo, hi := 0, len(retained)
		for lo < hi {
			mid := lo + (hi-lo)/2
			pin, err := pinOf(retained[mid])
			if err != nil {
				return nil, err
			}
			if compare(event, retained[mid], pins[event.ID], pin) < 0 {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		insertions[lo] = append(insertions[lo], event)
	}
	out := make([]core.Event, 0, len(retained)+len(changed))
	for i, event := range retained {
		out = append(out, insertions[i]...)
		out = append(out, event)
	}
	return append(out, insertions[len(retained)]...), nil
}

func eventPin(ctx context.Context, tx *sql.Tx, event core.Event) (int64, error) {
	bounds, args := eventBounds(&event)
	var pin int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(t.created_at), 0)
		FROM event_members m JOIN event_tags t ON t.id = m.tag_id AND t.kind = 'pin'
		WHERE m.asset_id IN (SELECT id FROM assets WHERE `+visible+` AND `+bounds+`)`, args...).Scan(&pin)
	return pin, err
}

// groupEvents uses the same grouping rules for the timeline and a gap-bounded
// window. Removing blocked photos only widens gaps, so a base-timeline window
// also contains complete visible events.
func groupEvents(ctx context.Context, tx *sql.Tx, gapHours int, window *[2]int64) ([]core.Event, error) {
	var out []core.Event
	where := visibleA
	var args []any
	if window != nil {
		where += " AND a.taken_at >= ? AND a.taken_at <= ?"
		args = []any{window[0], window[1]}
	}
	places := map[int64]string{}
	type namedID struct {
		id   int64
		name string
	}
	placeQuery := "SELECT id, name FROM places WHERE NOT " + placeBlocked("places.id")
	if window != nil {
		placeQuery += " AND id IN (SELECT a.place_id FROM assets a WHERE " + where + ")"
	}
	placeRows, err := queryAll(ctx, tx, func(r *sql.Rows) (v namedID, err error) {
		err = r.Scan(&v.id, &v.name)
		return v, err
	}, placeQuery, args...) // A blocked place names no event.
	if err != nil {
		return nil, err
	}
	for _, row := range placeRows {
		places[row.id] = row.name
	}
	tagged, err := eventTagRefs(ctx, tx, window)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id, a.taken_at, a.lat, a.lon, COALESCE(a.place_id, 0),
		a.thumb_version FROM assets a WHERE `+where+` ORDER BY a.taken_at, a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	split := eventSplitter{gap: int64(gapHours) * 3600}
	var current core.Event
	var placeCounts map[int64]int
	var pinnedAt int64 // the newest pin the event holds
	var customName string
	var blocked bool // the event holds a photo of a label-mode event block
	var pins []int64 // pinnedAt of each event in out
	finish := func() {
		if current.ID == 0 || blocked {
			return // a blocked event is left out, its photos stay in the library
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
			pinnedAt, customName, blocked = 0, "", false
		}
		// The event shows the name of its earliest photo that has one (the
		// lowest tag id if that photo holds several).
		named := customName != ""
		for _, ref := range tagged[a.id] {
			switch ref.kind {
			case eventTagName:
				if !named {
					customName, named = ref.name, true
				}
			case eventTagPin:
				pinnedAt = max(pinnedAt, ref.createdAt)
			case eventTagBlock:
				blocked = true
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
	// One statement seeks each event's newest thumbnail through the timeline
	// index. This avoids both a query per event and scanning every thumbnail
	// when most events have one near their end.
	bounds := make([][4]int64, len(events))
	for i := range events {
		event := &events[i]
		bounds[i] = [4]int64{event.StartAt, event.EndAt, event.FirstID, event.LastID}
	}
	list, err := json.Marshal(bounds)
	if err != nil {
		return err
	}
	// OFFSET prevents flattening, so each event's JSON bounds are decoded
	// once rather than again for every thumbnail candidate.
	rows, err := tx.QueryContext(ctx, `WITH bounds AS (
			SELECT key AS position, json_extract(value, '$[0]') AS start_at,
			json_extract(value, '$[1]') AS end_at, json_extract(value, '$[2]') AS first_id,
			json_extract(value, '$[3]') AS last_id FROM json_each(?) LIMIT -1 OFFSET 0)
		SELECT e.position, COALESCE(cover.id, 0), COALESCE(cover.thumb_version, 0)
		FROM bounds e LEFT JOIN assets cover ON cover.id = (
			SELECT id FROM assets WHERE `+visible+` AND thumb_version > 0 AND `+
		eventBoundsSQL("e.start_at", "e.end_at", "e.first_id", "e.last_id")+`
			ORDER BY taken_at DESC, id DESC LIMIT 1)`, string(list))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var index int
		var id int64
		var thumb int
		if err := rows.Scan(&index, &id, &thumb); err != nil {
			return err
		}
		events[index].Cover, events[index].CoverThumb = id, thumb
	}
	return rows.Err()
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
	var event core.Event
	err := s.read(ctx, func(tx *sql.Tx) error {
		var err error
		event, err = eventIn(ctx, tx, id, gapHours)
		return err
	})
	return event, err
}

// eventIn resolves one event in its caller's snapshot without the list cache.
// With a long gap and photos shot every day, the window can still span most
// of the library; that worst case is no worse than the former full regroup.
func eventIn(ctx context.Context, tx *sql.Tx, id int64, gapHours int) (core.Event, error) {
	var at int64
	err := tx.QueryRowContext(ctx, "SELECT taken_at FROM assets WHERE id = ? AND "+visible, id).Scan(&at)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Event{}, ErrNotFound
		}
		return core.Event{}, err
	}
	gapHours = validGap(gapHours)
	lo, hi, err := newSettler(tx, int64(gapHours)*3600).runAround(ctx, at)
	if err != nil {
		return core.Event{}, err
	}
	events, err := groupEvents(ctx, tx, gapHours, &[2]int64{lo, hi})
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
		event, err := eventIn(ctx, tx, id, gapHours)
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
		event, err := eventIn(ctx, tx, id, gapHours)
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

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"

	"lumen/internal/core"
)

// Event tags: what the user says about an event. An event is a run of photos
// found on read (groupEvents), so a name, a pin or a block cannot hang off
// the event itself. It hangs off the photos the event held when the user
// set it: the tag's members. A run carries a tag when it holds any member.
//
// Settling keeps the members equal to the runs as the library changes. It
// groups the base timeline (present, unhidden, untrashed photos, ignoring
// blocks, so a block can never change the grouping that defines another
// block) with the splitter groupEvents uses. For each tag and run:
//
//   - a run with an unmoved member carries the tag, and every photo in it
//     becomes a member: a photo that joins an event takes its name, pin or
//     block;
//   - a run with only moved members (photos whose date or place changed
//     since the last settle) carries the tag only if the tag has no unmoved
//     member anywhere, which is a whole event moved together (a time-zone
//     fix). Otherwise they are dropped: the photos left the event.
//
// A split keeps the tag on both halves, since both hold original members.
// A photo that is trashed, hidden or missing is in no run, so it keeps its
// membership until it is back.
const (
	eventTagName  = "name"
	eventTagPin   = "pin"
	eventTagBlock = "block" // made by eventBlockTarget
)

// eventTagRef is one event tag a photo holds, as groupEvents reads them.
type eventTagRef struct {
	id        int64
	kind      string
	name      string
	createdAt int64
}

// eventTagRefs returns the name and pin tags held in the requested window,
// or the whole timeline when the window is nil,
// and the block tags of the events whose block leaves their photos showing
// (label mode). The members of a photo-mode block are all hidden, and the
// event shows none of them, so they are left out.
func eventTagRefs(ctx context.Context, tx *sql.Tx, window *[2]int64) (map[int64][]eventTagRef, error) {
	from := "event_members m JOIN event_tags t ON t.id = m.tag_id"
	where := ""
	var args []any
	if window != nil {
		// The window drives the join, even when a tag holds the whole library.
		from = "assets a CROSS JOIN " + from
		where = visibleA + " AND a.taken_at >= ? AND a.taken_at <= ? AND m.asset_id = a.id AND "
		args = []any{window[0], window[1]}
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.asset_id, t.id, t.kind, COALESCE(t.name, ''), t.created_at
		FROM `+from+` WHERE `+where+`(t.kind IN ('name', 'pin') OR (t.kind = 'block'
			AND EXISTS (SELECT 1 FROM blocks b WHERE b.kind = 'event' AND b.target_id = t.id AND b.mode = 'label')))
		ORDER BY t.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]eventTagRef{}
	for rows.Next() {
		var asset int64
		var ref eventTagRef
		if err := rows.Scan(&asset, &ref.id, &ref.kind, &ref.name, &ref.createdAt); err != nil {
			return nil, err
		}
		out[asset] = append(out[asset], ref)
	}
	return out, rows.Err()
}

// eventBounds is the condition for a photo to lie within the event e: its
// time range, and the ids that bound it where photos share a second. It
// leads with the plain range so the timeline index can serve it.
func eventBounds(e *core.Event) (where string, args []any) {
	return eventBoundsSQL("?", "?", "?", "?"),
		[]any{e.StartAt, e.EndAt, e.StartAt, e.StartAt, e.FirstID, e.EndAt, e.EndAt, e.LastID}
}

// eventBoundsSQL shares the edge rule between bound arguments and the cover
// batch's bound table. Its inputs are SQL expressions, never user values.
func eventBoundsSQL(start, end, first, last string) string {
	return "taken_at >= " + start + " AND taken_at <= " + end +
		" AND (taken_at > " + start + " OR (taken_at = " + start + " AND id >= " + first + "))" +
		" AND (taken_at < " + end + " OR (taken_at = " + end + " AND id <= " + last + "))"
}

// eventAssets is the condition for a photo of the base timeline to lie
// within the event e, whether or not a block covers it.
func eventAssets(e core.Event) (where string, args []any) {
	bounds, args := eventBounds(&e)
	return baseVisibleOf("") + " AND " + bounds, args
}

// dropEmptyEventTags deletes the name and pin tags that no photo holds any
// more. Block tags stay: a block is the user's rule, not a label on photos.
func dropEmptyEventTags(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM event_tags WHERE kind IN ('name', 'pin')
		AND NOT EXISTS (SELECT 1 FROM event_members WHERE tag_id = event_tags.id)`)
	return err
}

// settleDirty settles the event tags if any photo changed since the last
// settle. Store.tx calls it before every commit, so no transaction ever
// commits a photo that joined an event without its tags. With nothing to
// settle it costs one probe of event_dirty (a library with no event tag
// records nothing there).
func settleDirty(ctx context.Context, tx *sql.Tx) error {
	var probe int
	switch err := tx.QueryRowContext(ctx, "SELECT 1 FROM event_dirty LIMIT 1").Scan(&probe); {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return err
	}
	times, err := queryAll(ctx, tx, scanID, "SELECT at FROM event_dirty ORDER BY at")
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM event_dirty"); err != nil {
		return err
	}
	gap, err := eventGapIn(ctx, tx)
	if err != nil {
		return err
	}
	st := newSettler(tx, gap)
	// Dirty times ascend, so a time inside the last window needs no window of
	// its own: a batch from one event finds the event once.
	var windows [][2]int64
	for _, at := range times {
		if n := len(windows); n > 0 && at >= windows[n-1][0] && at <= windows[n-1][1] {
			continue
		}
		lo, hi, err := st.runAround(ctx, at)
		if err != nil {
			return err
		}
		if n := len(windows); n > 0 && lo <= windows[n-1][1] {
			windows[n-1][1] = max(windows[n-1][1], hi)
			continue
		}
		windows = append(windows, [2]int64{lo, hi})
	}
	for _, w := range windows {
		if err := st.window(ctx, w[0], w[1]); err != nil {
			return err
		}
	}
	return st.finish(ctx)
}

// settleAll settles every event tag against the whole timeline, for a change
// to the event gap: runs merge or split everywhere, not around some photo.
func settleAll(ctx context.Context, tx *sql.Tx, gapHours int) error {
	var lo, hi sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT min(taken_at), max(taken_at) FROM assets WHERE "+baseVisibleOf("")).Scan(&lo, &hi); err != nil {
		return err
	}
	if !lo.Valid {
		return nil
	}
	st := newSettler(tx, int64(validGap(gapHours))*3600)
	if err := st.window(ctx, lo.Int64, hi.Int64); err != nil {
		return err
	}
	return st.finish(ctx)
}

// eventGapIn reads the event gap as the transaction sees it, so a settle in
// the transaction that changes the gap uses the new one.
func eventGapIn(ctx context.Context, tx *sql.Tx) (int64, error) {
	hours := core.EventGapDefault
	var value string
	switch err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", core.SettingEventGapHours).Scan(&value); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return 0, err
	default:
		if n, perr := strconv.Atoi(value); perr == nil { // unreadable: as if never set
			hours = n
		}
	}
	return int64(validGap(hours)) * 3600, nil
}

// SetEventGapHours stores the quiet gap that splits events and settles every
// event tag against it in the same transaction, so the tags never lag the
// events a reader sees.
func (s *Store) SetEventGapHours(ctx context.Context, hours int) error {
	s.settingsMu.Lock() // as setSetting: the cache ends as the database does
	defer s.settingsMu.Unlock()
	value := strconv.Itoa(hours)
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			core.SettingEventGapHours, value); err != nil {
			return err
		}
		return settleAll(ctx, tx, hours)
	})
	if err == nil {
		s.settings.Store(core.SettingEventGapHours, value)
	}
	return err
}

// settler settles event tags window by window within one transaction.
type settler struct {
	tx      *sql.Tx
	gap     int64          // seconds
	unmoved map[int64]bool // tag -> held an unmoved member before this settle changed it
}

func newSettler(tx *sql.Tx, gap int64) *settler {
	return &settler{tx: tx, gap: gap, unmoved: map[int64]bool{}}
}

// finish removes the tags the settle (or a photo's deletion) left without members.
func (st *settler) finish(ctx context.Context) error {
	return dropEmptyEventTags(ctx, st.tx)
}

// runAround returns the time span of the runs a change at the time at can
// touch: a run the gap joins to it, on either side. Photos further than the
// gap from each other are never one event, whatever else the grouping says,
// so nothing beyond the span can change.
func (st *settler) runAround(ctx context.Context, at int64) (lo, hi int64, err error) {
	lo, err = st.reach(ctx, at, "taken_at <= ? ORDER BY taken_at DESC", func(prev, cur int64) bool { return prev-cur > st.gap })
	if err != nil {
		return 0, 0, err
	}
	hi, err = st.reach(ctx, at, "taken_at >= ? ORDER BY taken_at", func(prev, cur int64) bool { return cur-prev > st.gap })
	return lo, hi, err
}

// reach walks the timeline from at in the order of query (the timeline index
// serves it) and returns the last time reached before apart says the next
// photo is too far from the one before. It returns at if the first photo is.
func (st *settler) reach(ctx context.Context, at int64, query string, apart func(prev, cur int64) bool) (int64, error) {
	rows, err := st.tx.QueryContext(ctx, "SELECT taken_at FROM assets WHERE "+baseVisibleOf("")+" AND "+query, at)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	last := at
	for rows.Next() {
		var cur int64
		if err := rows.Scan(&cur); err != nil {
			return 0, err
		}
		if apart(last, cur) {
			break
		}
		last = cur
	}
	return last, rows.Err()
}

// window settles every tag that has a member among the photos taken from lo
// to hi (inclusive), whose edges must be at least a gap from any other photo
// (or the whole timeline), so the runs inside are exactly the runs of the
// whole timeline.
func (st *settler) window(ctx context.Context, lo, hi int64) error {
	held, err := st.heldIn(ctx, lo, hi)
	if err != nil || len(held) == 0 {
		return err // most windows hold no tagged photo
	}
	rows, err := st.tx.QueryContext(ctx, `SELECT id, taken_at, lat, lon FROM assets
		WHERE `+baseVisibleOf("")+` AND taken_at >= ? AND taken_at <= ? ORDER BY taken_at, id`, lo, hi)
	if err != nil {
		return err
	}
	defer rows.Close()
	split := eventSplitter{gap: st.gap}
	var runs [][]int64
	for rows.Next() {
		var a eventAsset
		if err := rows.Scan(&a.id, &a.at, &a.lat, &a.lon); err != nil {
			return err
		}
		if split.next(a) {
			runs = append(runs, nil)
		}
		runs[len(runs)-1] = append(runs[len(runs)-1], a.id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, tag := range slices.Sorted(maps.Keys(held)) {
		if err := st.settleTag(ctx, tag, held[tag], runs); err != nil {
			return err
		}
	}
	return nil
}

// heldIn returns, for each tag with a member taken from lo to hi, its
// members there and whether each moved.
func (st *settler) heldIn(ctx context.Context, lo, hi int64) (map[int64]map[int64]bool, error) {
	// The photos drive the join: a tag may hold a whole library of them.
	rows, err := st.tx.QueryContext(ctx, `SELECT m.tag_id, m.asset_id, m.moved FROM assets a
		CROSS JOIN event_members m ON m.asset_id = a.id
		WHERE `+baseVisibleOf("a")+` AND a.taken_at >= ? AND a.taken_at <= ?`, lo, hi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	held := map[int64]map[int64]bool{}
	for rows.Next() {
		var tag, asset int64
		var moved bool
		if err := rows.Scan(&tag, &asset, &moved); err != nil {
			return nil, err
		}
		if held[tag] == nil {
			held[tag] = map[int64]bool{}
		}
		held[tag][asset] = moved
	}
	return held, rows.Err()
}

// settleTag applies the settling rules (see the top of this file) to one
// tag, given the runs of a window and the tag's members in it.
func (st *settler) settleTag(ctx context.Context, tag int64, members map[int64]bool, runs [][]int64) error {
	// Whether the tag has an unmoved member is judged once, before the settle
	// changes any of its members: a whole event that moved must keep its tag
	// in every run it lands in, not only the first.
	if _, seen := st.unmoved[tag]; !seen {
		var has bool
		if err := st.tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM event_members WHERE tag_id = ? AND moved = 0)", tag).Scan(&has); err != nil {
			return err
		}
		st.unmoved[tag] = has
	}
	var add, settled, drop []int64
	for _, run := range runs {
		var unmoved int
		var moved, joiners []int64
		for _, id := range run {
			switch m, held := members[id]; {
			case !held:
				joiners = append(joiners, id)
			case m:
				moved = append(moved, id)
			default:
				unmoved++
			}
		}
		switch {
		case unmoved+len(moved) == 0:
		case unmoved > 0 || !st.unmoved[tag]:
			add = append(add, joiners...)
			settled = append(settled, moved...)
		default:
			drop = append(drop, moved...)
		}
	}
	for _, change := range []struct {
		ids   []int64
		query string
	}{
		{add, "INSERT OR IGNORE INTO event_members (tag_id, asset_id) SELECT ?1, value FROM json_each(?2)"},
		{settled, "UPDATE event_members SET moved = 0 WHERE tag_id = ?1 AND asset_id IN (SELECT value FROM json_each(?2))"},
		{drop, "DELETE FROM event_members WHERE tag_id = ?1 AND asset_id IN (SELECT value FROM json_each(?2))"},
	} {
		if len(change.ids) == 0 {
			continue
		}
		list, err := json.Marshal(change.ids)
		if err != nil {
			return err
		}
		if _, err := st.tx.ExecContext(ctx, change.query, tag, string(list)); err != nil {
			return err
		}
	}
	return nil
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"

	"lumen/internal/core"
)

func assertEventsMatchEveryAsset(t *testing.T, st *Store) {
	t.Helper()
	ctx := t.Context()
	assets, err := queryAll(ctx, st.r, func(rows *sql.Rows) (a eventAsset, err error) {
		err = rows.Scan(&a.id, &a.at)
		return a, err
	}, "SELECT id, taken_at FROM assets ORDER BY id")
	check(t, err)
	for _, gap := range []int{1, 6, 12, 36, 72, 0, 100} {
		events, err := st.Events(ctx, gap)
		check(t, err)
		for _, a := range assets {
			var showing bool
			check(t, st.r.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM assets WHERE id = ? AND "+visible+")", a.id).Scan(&showing))
			var want core.Event
			if showing {
				for _, event := range events {
					if (a.at > event.StartAt || (a.at == event.StartAt && a.id >= event.FirstID)) &&
						(a.at < event.EndAt || (a.at == event.EndAt && a.id <= event.LastID)) {
						want = event
						break
					}
				}
			}
			got, err := st.Event(ctx, a.id, gap)
			if want.ID == 0 {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("gap %d asset %d: got %+v, %v; want ErrNotFound", gap, a.id, got, err)
				}
			} else if err != nil || got != want {
				t.Fatalf("gap %d asset %d: got %+v, %v; want %+v", gap, a.id, got, err, want)
			}
		}
		if _, err := st.Event(ctx, 99999, gap); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing asset at gap %d: %v", gap, err)
		}
	}
}

func TestLocalEventsEqualTheWholeTimelineThroughRandomisedChanges(t *testing.T) {
	for _, seed := range []uint64{1, 42, 20261013} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			f := newEventFixture(t, st)
			rng := rand.New(rand.NewPCG(seed, seed+1))
			writeInTx(t, st, `INSERT INTO places(id, name, region, country, lat, lon)
				VALUES (100001, 'Here', '', '', 0, 0), (100002, 'There', '', '', 0, 0)`)
			var ids []int64
			for i := range 48 {
				hours := float64((i/6)*96) + float64(rng.IntN(4))
				var loc *[2]float64
				if i%3 != 0 {
					loc = &[2]float64{float64(rng.IntN(2)) * 20, 0}
				}
				ids = append(ids, f.addAt(hours, loc))
			}
			writeInTx(t, st, "UPDATE assets SET place_id = 100001 + id%2, thumb_version = id%3")
			f.name(ids[0], "Trip")
			f.pin(ids[6], true)
			f.block(ids[12], core.BlockLabel)
			f.block(ids[18], core.BlockPhotos)
			writeInTx(t, st, "UPDATE assets SET hidden = 1 WHERE id = ?", ids[24])
			check(t, st.TrashAssets(ctx, []int64{ids[30]}))
			_, err := st.SetBlock(ctx, core.BlockPlace, 100001, "", core.BlockLabel)
			check(t, err)
			assertEventsMatchEveryAsset(t, st)
			for round := range 5 {
				for _, operation := range rng.Perm(10) {
					id := ids[rng.IntN(len(ids))]
					switch operation {
					case 0:
						f.redate(float64(rng.IntN(8)*96+rng.IntN(4)), &[2]float64{float64(rng.IntN(2)) * 20, 0}, id)
					case 1:
						writeInTx(t, st, "UPDATE assets SET hidden = 1-hidden WHERE id = ?", id)
					case 2:
						writeInTx(t, st, "UPDATE assets SET trashed_at = CASE WHEN trashed_at IS NULL THEN 1 END WHERE id = ?", id)
					case 3:
						writeInTx(t, st, "UPDATE assets SET missing_since = CASE WHEN missing_since IS NULL THEN 1 END WHERE id = ?", id)
					case 4:
						writeInTx(t, st, "UPDATE assets SET thumb_version = ? WHERE id = ?", rng.IntN(4), id)
					case 5:
						writeInTx(t, st, "UPDATE assets SET place_id = ? WHERE id = ?", 100001+rng.IntN(2), id)
					case 6:
						check(t, st.SetEventGapHours(ctx, []int{1, 6, 12, 36, 72}[rng.IntN(5)]))
					case 7, 8:
						gap, err := st.EventGapHours(ctx)
						check(t, err)
						_, err = st.Event(ctx, id, gap)
						if errors.Is(err, ErrNotFound) {
							break
						}
						check(t, err)
						if operation == 7 {
							check(t, st.SetEventName(ctx, id, fmt.Sprintf("Trip %d", round), gap))
						} else {
							check(t, st.SetEventPinned(ctx, id, rng.IntN(2) == 0, gap))
						}
					case 9:
						blocks, err := st.Blocks(ctx)
						check(t, err)
						if len(blocks) > 2 && rng.IntN(2) == 0 {
							check(t, st.RemoveBlock(ctx, blocks[rng.IntN(len(blocks))].ID))
						} else {
							_, err := st.SetBlock(ctx, core.BlockEvent, id, "", []core.BlockMode{core.BlockLabel, core.BlockPhotos}[rng.IntN(2)])
							if !errors.Is(err, ErrNotFound) {
								check(t, err)
							}
						}
					}
					assertEventsMatchEveryAsset(t, st)
				}
			}
		})
	}
}

func TestEventReadsAndWritesCompleteWhileTheListCacheIsLocked(t *testing.T) {
	for _, operation := range []string{"lookup", "name", "pin", "block", "grid"} {
		t.Run(operation, func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			f := newEventFixture(t, st)
			a := f.add(0)
			f.add(1)
			check(t, errOf(st.Events(ctx, 12)))
			f.redate(0.5, nil, a)
			st.eventsCache.Lock()
			done := make(chan error, 1)
			go func() {
				switch operation {
				case "lookup":
					done <- errOf(st.Event(ctx, a, 12))
				case "name":
					done <- st.SetEventName(ctx, a, "Trip", 12)
				case "pin":
					done <- st.SetEventPinned(ctx, a, true, 12)
				case "block":
					done <- errOf(st.SetBlock(ctx, core.BlockEvent, a, "", core.BlockLabel))
				case "grid":
					done <- errOf(st.DayItems(ctx, "2026-01-01", "2026-01-01", core.View{Event: a}, core.TagConfidenceDefault))
				}
			}()
			select {
			case err := <-done:
				st.eventsCache.Unlock()
				check(t, err)
			case <-time.After(5 * time.Second):
				st.eventsCache.Unlock()
				check(t, <-done)
				t.Fatal("operation waited for the whole-timeline cache mutex")
			}
		})
	}
}

func TestEventLookupKeepsTheSnapshotThatReadItsAnchor(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	f := newEventFixture(t, st)
	a := f.add(0)
	b := f.add(1)
	want := f.event(a)
	st.r.SetMaxOpenConns(1)
	conn, err := st.r.Conn(ctx)
	check(t, err)
	defer func() { _ = conn.Close() }()
	var changed sync.Once
	check(t, conn.Raw(func(raw any) error {
		sqlite, ok := raw.(*sqlite3.SQLiteConn)
		if !ok {
			return errors.New("unexpected SQLite connection")
		}
		return sqlite.RegisterFunc("read_event_time", func(at int64) (int64, error) {
			var changeErr error
			changed.Do(func() {
				changeErr = st.tx(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", b)
					return err
				})
			})
			return at, changeErr
		}, false)
	}))
	check(t, errOf(conn.ExecContext(ctx, `PRAGMA query_only = OFF;
		CREATE TEMP VIEW assets AS SELECT id, read_event_time(taken_at) AS taken_at,
		lat, lon, place_id, thumb_version, hidden, trashed_at, missing_since FROM main.assets;
		PRAGMA query_only = ON;`)))
	check(t, conn.Close())
	got, err := st.Event(ctx, a, 12)
	check(t, err)
	if got != want {
		t.Fatalf("lookup mixed snapshots: got %+v, want %+v", got, want)
	}
	var hidden bool
	check(t, st.r.QueryRowContext(ctx, "SELECT hidden FROM assets WHERE id = ?", b).Scan(&hidden))
	if !hidden {
		t.Fatal("concurrent visibility change did not commit")
	}
}

func TestEventNamePinAndBlockUseTheWritersOwnTimeline(t *testing.T) {
	for _, operation := range []string{"name", "pin", "block"} {
		t.Run(operation, func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			f := newEventFixture(t, st)
			a, b := f.add(0), f.add(100)
			// Only the writer sees the second photo join the first. Reading from
			// the pool would resolve the old single-photo bounds instead.
			conn, err := st.w.Conn(ctx)
			check(t, err)
			check(t, errOf(conn.ExecContext(ctx, fmt.Sprintf(`CREATE TEMP VIEW assets AS SELECT id,
				CASE WHEN id = %d THEN %d ELSE taken_at END AS taken_at,
				lat, lon, place_id, thumb_version, hidden, trashed_at, missing_since FROM main.assets`, b, at(0)))))
			check(t, conn.Close())
			switch operation {
			case "name":
				check(t, st.SetEventName(ctx, a, "Trip", 12))
			case "pin":
				check(t, st.SetEventPinned(ctx, a, true, 12))
			case "block":
				check(t, errOf(st.SetBlock(ctx, core.BlockEvent, a, "", core.BlockLabel)))
			}
			members, err := queryAll(ctx, st.r, scanID, "SELECT asset_id FROM event_members ORDER BY asset_id")
			check(t, err)
			if !slices.Equal(members, []int64{a, b}) {
				t.Fatalf("%s used another snapshot: members %v, want %v", operation, members, []int64{a, b})
			}
		})
	}
}

func perEventCovers(ctx context.Context, tx *sql.Tx, events []core.Event) error {
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
		if err := cover.QueryRowContext(ctx, args...).Scan(&event.Cover, &event.CoverThumb); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

func TestRefreshedEventCoversEqualThePerEventAnswers(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	f := newEventFixture(t, st)
	a := f.addAt(0, &[2]float64{0, 0})
	b := f.addAt(1, &[2]float64{0, 0})
	c := f.addAt(1, &[2]float64{20, 0})
	d := f.add(1)
	f.add(100) // This event has no thumbnail yet.
	e := f.add(200)
	g := f.add(300)
	writeInTx(t, st, "UPDATE assets SET thumb_version = id WHERE id IN (?, ?, ?, ?, ?, ?)", a, b, c, d, e, g)
	f.pin(a, true)
	f.block(e, core.BlockLabel)
	f.block(g, core.BlockPhotos)
	_, err := st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\p4.jpg`, core.BlockPhotos)
	check(t, err)
	for _, thumb := range []int{0, 5, 0} {
		writeInTx(t, st, "UPDATE assets SET thumb_version = ? WHERE id = ?", thumb, b)
		got, err := st.Events(ctx, 12)
		check(t, err)
		want := slices.Clone(got)
		check(t, st.read(ctx, func(tx *sql.Tx) error {
			if err := perEventCovers(ctx, tx, want); err != nil {
				return err
			}
			return refreshEventCovers(ctx, tx, got)
		}))
		if !slices.Equal(got, want) {
			t.Fatalf("cover refresh differs: got %+v, want %+v", got, want)
		}
		if len(got) != 3 || got[0].ID != a || got[1].Cover != 0 || got[2].Cover != c {
			t.Fatalf("fixture lost pin, empty cover or shared-second edge: %+v", got)
		}
	}
}

func TestWindowTagReadsUseTheTimelineRange(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	f := newEventFixture(t, st)
	a := f.add(0)
	f.add(100)
	f.name(a, "Trip")
	check(t, st.read(ctx, func(tx *sql.Tx) error {
		refs, err := eventTagRefs(ctx, tx, &[2]int64{at(100), at(100)})
		if err != nil {
			return err
		}
		if len(refs) != 0 {
			t.Fatalf("window loaded tags from outside it: %v", refs)
		}
		return nil
	}))
	// Inspect the range used to drive the local membership join.
	rows, err := st.r.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT m.tag_id FROM assets a
		CROSS JOIN event_members m ON m.asset_id = a.id WHERE `+visibleA+` AND a.taken_at >= ? AND a.taken_at <= ?`, at(0), at(1))
	check(t, err)
	defer rows.Close()
	var ranged bool
	for rows.Next() {
		var node, parent, unused int
		var detail string
		check(t, rows.Scan(&node, &parent, &unused, &detail))
		ranged = ranged || strings.Contains(detail, "SEARCH a USING INDEX assets_timeline (taken_at>")
	}
	check(t, rows.Err())
	if !ranged {
		t.Fatal("local tag membership did not use the timeline range")
	}
}

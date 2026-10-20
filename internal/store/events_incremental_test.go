package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mattn/go-sqlite3"

	"lumen/internal/core"
)

type eventListSnapshot struct {
	items            []core.Event
	revision, covers int64
}

// compareEventLists keeps a cache for each gap, so alternating gaps cannot
// accidentally turn every assertion into a comparison of two full regroups.
func compareEventLists(t *testing.T, st *Store, caches map[int]eventListSnapshot, gaps []int) {
	t.Helper()
	st.eventsCache.Lock()
	defer st.eventsCache.Unlock()
	check(t, st.read(t.Context(), func(tx *sql.Tx) error {
		for _, gap := range gaps {
			cache, loaded := caches[gap]
			st.eventsCache.loaded = loaded
			st.eventsCache.items = cache.items
			st.eventsCache.revision = cache.revision
			st.eventsCache.coverRevision = cache.covers
			st.eventsCache.gap = validGap(gap)
			got, revision, covers, err := st.eventsIn(t.Context(), tx, validGap(gap))
			if err != nil {
				return err
			}
			want, err := groupEvents(t.Context(), tx, validGap(gap), nil)
			if err != nil {
				return err
			}
			if !slices.Equal(got, want) {
				for i := range min(len(got), len(want)) {
					if got[i] != want[i] {
						t.Fatalf("gap %d revision %d event %d: incremental %+v, full %+v (lengths %d, %d)", gap, revision, i, got[i], want[i], len(got), len(want))
					}
				}
				t.Fatalf("gap %d revision %d: incremental length %d, full length %d", gap, revision, len(got), len(want))
			}
			caches[gap] = eventListSnapshot{items: got, revision: revision, covers: covers}
			st.eventsCache.items = got
			st.eventsCache.revision = revision
			st.eventsCache.coverRevision = covers
			st.eventsCache.loaded = true
		}
		return nil
	}))
}

func TestIncrementalEventListsEqualFullRegroupsAfterEveryRandomisedChange(t *testing.T) {
	t.Parallel()
	for _, seed := range []uint64{1, 42, 20261013} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			st, ctx := openTest(t), t.Context()
			f := newEventFixture(t, st)
			rng := rand.New(rand.NewPCG(seed, seed+1))
			writeInTx(t, st, `INSERT INTO places(id, name, region, country, lat, lon)
				VALUES (100001, 'Here', '', '', 0, 0), (100002, 'There', '', '', 20, 0)`)
			var ids []int64
			for i := range 24 {
				ids = append(ids, f.addAt(float64(i/4*96+i%4), &[2]float64{float64(i%2) * 20, 0}))
			}
			writeInTx(t, st, "UPDATE assets SET place_id = 100001 + id%2, thumb_version = id%3")
			var gaps []int
			for gap := core.EventGapMin; gap <= core.EventGapMax; gap++ {
				gaps = append(gaps, gap)
			}
			gaps = append(gaps, 0, 100)
			caches := make(map[int]eventListSnapshot)
			compareEventLists(t, st, caches, gaps)
			for step := range 1000 {
				id := ids[rng.IntN(len(ids))]
				operation := rng.IntN(18)
				switch operation {
				case 0:
					f.redate(float64(rng.IntN(8)*96+rng.IntN(16)), &[2]float64{float64(rng.IntN(2)) * 20, 0}, id)
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
					check(t, st.SetEventGapHours(ctx, 1+rng.IntN(72)))
				case 7, 8:
					gap, err := st.EventGapHours(ctx)
					check(t, err)
					if operation == 7 {
						name := ""
						if rng.IntN(2) == 0 {
							name = fmt.Sprintf("Trip %d", step)
						}
						err = st.SetEventName(ctx, id, name, gap)
					} else {
						err = st.SetEventPinned(ctx, id, rng.IntN(2) == 0, gap)
					}
					if !errors.Is(err, ErrNotFound) {
						check(t, err)
					}
				case 9:
					check(t, errUnlessNotFound(st.SetBlock(ctx, core.BlockEvent, id, "", []core.BlockMode{core.BlockLabel, core.BlockPhotos}[rng.IntN(2)])))
				case 10:
					blocks, err := st.Blocks(ctx)
					check(t, err)
					if len(blocks) != 0 {
						check(t, st.RemoveBlock(ctx, blocks[rng.IntN(len(blocks))].ID))
					}
				case 11:
					check(t, errOf(st.SetBlock(ctx, core.BlockPlace, int64(100001+rng.IntN(2)), "", []core.BlockMode{core.BlockLabel, core.BlockPhotos}[rng.IntN(2)])))
				case 12:
					check(t, errOf(st.SetBlock(ctx, core.BlockFolder, 0, fmt.Sprintf(`C:\Photos\p%d.jpg`, rng.IntN(f.n)+1), core.BlockPhotos)))
				case 13:
					if len(ids) < 32 {
						ids = append(ids, f.add(float64(rng.IntN(8)*96+rng.IntN(4))))
					}
				case 14:
					if len(ids) > 8 {
						writeInTx(t, st, "DELETE FROM assets WHERE id = ?", id)
						ids = slices.DeleteFunc(ids, func(candidate int64) bool { return candidate == id })
					}
				case 15:
					writeInTx(t, st, "UPDATE places SET name = ? WHERE id = ?", fmt.Sprintf("Place %d", step), 100001+rng.IntN(2))
				case 16:
					writeInTx(t, st, "UPDATE assets SET hidden = 0, trashed_at = NULL, missing_since = NULL WHERE id = ?", id)
				case 17:
					f.redate(0, nil, id)
				}
				compareEventLists(t, st, caches, gaps)
			}
		})
	}
}

func errUnlessNotFound(_ int64, err error) error {
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func TestIncrementalEventListsReadOnlyChangedWindows(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		for _, change := range []string{"date", "added", "cover"} {
			t.Run(fmt.Sprintf("%t/%s", blocked, change), func(t *testing.T) {
				st, ctx := openTest(t), t.Context()
				f := newEventFixture(t, st)
				a, b, untouched := f.add(0), f.add(1), f.add(100)
				if blocked {
					check(t, errOf(st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)))
				}
				before, err := st.Events(ctx, 12)
				check(t, err)
				want := append([]core.Event(nil), before...)
				switch change {
				case "date":
					f.redate(0.5, nil, b)
					want[1].EndAt = at(0.5)
				case "added":
					f.add(0.5)
					want[1].Count++
				case "cover":
					writeInTx(t, st, "UPDATE assets SET thumb_version = 1 WHERE id = ?", b)
					want[1].Cover, want[1].CoverThumb = b, 1
				}
				st.r.SetMaxOpenConns(1)
				conn, err := st.r.Conn(ctx)
				check(t, err)
				defer func() { _ = conn.Close() }()
				check(t, conn.Raw(func(raw any) error {
					sqlite, ok := raw.(*sqlite3.SQLiteConn)
					if !ok {
						return errors.New("unexpected SQLite connection")
					}
					return sqlite.RegisterFunc("read_changed_thumb", func(id int64, thumb int) (int, error) {
						if id == untouched {
							return 0, errors.New("regroup read an untouched event")
						}
						return thumb, nil
					}, false)
				}))
				check(t, errOf(conn.ExecContext(ctx, `PRAGMA query_only = OFF;
				CREATE TEMP VIEW assets AS SELECT id, taken_at, lat, lon, place_id,
				read_changed_thumb(id, thumb_version) AS thumb_version,
				kind, hidden, trashed_at, missing_since FROM main.assets;
				PRAGMA query_only = ON;`)))
				check(t, conn.Close())
				got, err := st.Events(ctx, 12)
				check(t, err)
				if !slices.Equal(got, want) || got[1].ID != a {
					t.Fatalf("unexpected incremental list: %+v", got)
				}
			})
		}
	}
}

func TestIncrementalEventListsDoNotRescanEveryRetainedPin(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	writeInTx(t, st, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<256)
		INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at)
		SELECT x, printf('pin-%d', x), 'photo', '.jpg', 1767261600+x*345600, 0 FROM n`)
	writeInTx(t, st, "INSERT INTO event_tags(id, kind, created_at) SELECT id, 'pin', id FROM assets")
	writeInTx(t, st, "INSERT INTO event_members(tag_id, asset_id) SELECT id, id FROM assets")
	want, err := st.Events(ctx, 12)
	check(t, err)
	writeInTx(t, st, "UPDATE assets SET taken_at = taken_at + 60 WHERE id = 128")
	for i := range want {
		if want[i].ID == 128 {
			want[i].StartAt += 60
			want[i].EndAt += 60
		}
	}
	st.r.SetMaxOpenConns(1)
	conn, err := st.r.Conn(ctx)
	check(t, err)
	defer func() { _ = conn.Close() }()
	var reads int
	check(t, conn.Raw(func(raw any) error {
		sqlite, ok := raw.(*sqlite3.SQLiteConn)
		if !ok {
			return errors.New("unexpected SQLite connection")
		}
		return sqlite.RegisterFunc("read_retained_pin", func(pin int64) (int64, error) {
			reads++
			if reads > 64 {
				return 0, errors.New("read too many retained pins")
			}
			return pin, nil
		}, false)
	}))
	check(t, errOf(conn.ExecContext(ctx, `PRAGMA query_only = OFF;
		CREATE TEMP VIEW event_tags AS SELECT id, kind, name,
		read_retained_pin(created_at) AS created_at FROM main.event_tags;
		PRAGMA query_only = ON;`)))
	check(t, conn.Close())
	got, err := st.Events(ctx, 12)
	check(t, err)
	if !slices.Equal(got, want) {
		t.Fatal("pin insertion changed the cached order")
	}
}

func TestIncrementalEventListsMatchSnapshotsWithConcurrentReadersAndAWriter(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	f := newEventFixture(t, st)
	f.add(0)
	var committed atomic.Int64
	var changingVisibility atomic.Bool
	committed.Store(1)
	var readers sync.WaitGroup
	done := make(chan struct{})
	for range 3 {
		readers.Go(func() {
			caches := make(map[int]eventListSnapshot)
			for {
				select {
				case <-done:
					return
				default:
				}
				minimum := committed.Load()
				countWasStable := !changingVisibility.Load()
				events, err := st.Events(ctx, 12)
				if err != nil {
					t.Error(err)
					return
				}
				var count int64
				for _, event := range events {
					count += int64(event.Count)
				}
				if countWasStable && !changingVisibility.Load() && count < minimum {
					t.Errorf("list predates its call: %d photos, at least %d committed", count, minimum)
					return
				}
				compareEventLists(t, st, caches, []int{1, 12, 36, 72})
			}
		})
	}
	defer func() {
		close(done)
		readers.Wait()
	}()
	for i := range 100 {
		f.add(float64(i*96 + i%3))
		committed.Add(1)
	}
	changingVisibility.Store(true)
	for step := range 500 {
		id := int64(step%100 + 1)
		switch step % 10 {
		case 0:
			f.redate(float64(step%8*96), &[2]float64{float64(step%3) * 20, 0}, id)
		case 1:
			writeInTx(t, st, "UPDATE assets SET hidden = 1-hidden WHERE id = ?", id)
		case 2:
			writeInTx(t, st, "UPDATE assets SET trashed_at = CASE WHEN trashed_at IS NULL THEN 1 END WHERE id = ?", id)
		case 3:
			writeInTx(t, st, "UPDATE assets SET missing_since = CASE WHEN missing_since IS NULL THEN 1 END WHERE id = ?", id)
		case 4:
			writeInTx(t, st, "UPDATE assets SET hidden = 0, trashed_at = NULL, missing_since = NULL, thumb_version = thumb_version + 1 WHERE id = ?", id)
		case 5:
			gap, err := st.EventGapHours(ctx)
			check(t, err)
			err = st.SetEventName(ctx, id, fmt.Sprintf("Trip %d", step), gap)
			if !errors.Is(err, ErrNotFound) {
				check(t, err)
			}
			err = st.SetEventPinned(ctx, id, step%20 == 5, gap)
			if !errors.Is(err, ErrNotFound) {
				check(t, err)
			}
		case 6:
			check(t, errUnlessNotFound(st.SetBlock(ctx, core.BlockEvent, id, "", core.BlockPhotos)))
		case 7:
			check(t, errOf(st.SetBlock(ctx, core.BlockFolder, 0, fmt.Sprintf(`C:\Photos\p%d.jpg`, id), core.BlockPhotos)))
		case 8:
			blocks, err := st.Blocks(ctx)
			check(t, err)
			for _, block := range blocks {
				check(t, st.RemoveBlock(ctx, block.ID))
			}
		case 9:
			check(t, st.SetEventGapHours(ctx, step%72+1))
		}
	}
}

func TestIncrementalEventListsFallBackAfterDirtyHistoryIsPruned(t *testing.T) {
	st := openTest(t)
	f := newEventFixture(t, st)
	a := f.add(0)
	f.add(100)
	caches := make(map[int]eventListSnapshot)
	compareEventLists(t, st, caches, []int{12})
	writeInTx(t, st, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<4200)
		INSERT INTO assets(content_hash, kind, ext, taken_at, added_at)
		SELECT printf('history-%d', x), 'photo', '.jpg', ? + x*86400, 0 FROM n`, at(1000))
	f.redate(200, nil, a)
	var floor int64
	check(t, st.r.QueryRowContext(t.Context(), "SELECT floor FROM event_list_revision WHERE id = 1").Scan(&floor))
	if floor <= caches[12].revision || countRows(t, st, "event_list_dirty") > 8192 {
		t.Fatal("dirty history was not bounded")
	}
	compareEventLists(t, st, caches, []int{12})
	writeInTx(t, st, "UPDATE assets SET thumb_version = 1")
	if countRows(t, st, "event_list_cover_dirty") > 4096 {
		t.Fatal("cover history was not bounded")
	}
	compareEventLists(t, st, caches, []int{12})
}

func TestEventListMigrationKeepsExistingHistoryWhenReapplied(t *testing.T) {
	st := openTest(t)
	f := newEventFixture(t, st)
	a := f.add(0)
	f.add(100)
	caches := make(map[int]eventListSnapshot)
	compareEventLists(t, st, caches, []int{12})
	body, err := migrationFS.ReadFile("migrations/0033_event_list_dirty.sql")
	check(t, err)
	check(t, st.runTx(t.Context(), false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), string(body))
		return err
	}))
	var revision int64
	check(t, st.r.QueryRowContext(t.Context(), "SELECT value FROM event_list_revision WHERE id = 1").Scan(&revision))
	if revision != caches[12].revision {
		t.Fatal("reapplying the migration reset the history clock")
	}
	f.redate(0.5, nil, a)
	compareEventLists(t, st, caches, []int{12})
}

// BenchmarkEventListSmallChanges isolates list reads after one added or moved
// photo. The lead can add these cases to the shared latency gate.
func BenchmarkEventListSmallChanges(b *testing.B) {
	ctx := b.Context()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	mustBench(b, err)
	b.Cleanup(func() { mustBench(b, st.Close()) })
	mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<?)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, thumb_version)
			SELECT x, printf('%016x', x), 'photo', '.jpg', ? + (x/30)*86400 + x%30*60, 0, 1 FROM n`, largeLibrarySize, fixtureStart)
		return err
	}))
	for _, size := range []int{10000, 100000} {
		for _, change := range []string{"added", "moved"} {
			b.Run(fmt.Sprintf("%d/%s", size, change), func(b *testing.B) {
				// Hide the upper range to compare scaling on the same seeded database.
				mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "UPDATE assets SET hidden = (id > ?) WHERE id <= ?", size, largeLibrarySize)
					return err
				}))
				mustBench(b, errOf(st.Events(ctx, 12)))
				var serial int64
				for b.Loop() {
					b.StopTimer()
					serial++
					mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
						if change == "moved" {
							_, err := tx.ExecContext(ctx, "UPDATE assets SET taken_at = ? WHERE id = 100", at(96)+serial%2*86400)
							return err
						}
						_, err := tx.ExecContext(ctx, `INSERT INTO assets(content_hash, kind, ext, taken_at, added_at)
							VALUES (?, 'photo', '.jpg', ?, 0)`, fmt.Sprintf("added-%d", serial), at(96))
						return err
					}))
					b.StartTimer()
					mustBench(b, errOf(st.Events(ctx, 12)))
					b.StopTimer()
					if change == "added" {
						mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
							_, err := tx.ExecContext(ctx, "DELETE FROM assets WHERE id > ?", largeLibrarySize)
							return err
						}))
					}
					b.StartTimer()
				}
			})
		}
	}
}

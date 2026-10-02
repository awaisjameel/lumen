package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

// fixtureStart is where the event fixtures' hours count from.
const fixtureStart = 1_767_261_600 // 2026-01-01 10:00 UTC

// eventFixture adds photos at hours from fixtureStart, one transaction to
// ingest each and one to date it, as the indexer does.
type eventFixture struct {
	t   *testing.T
	st  *Store
	src int64
	n   int
}

func newEventFixture(t *testing.T, st *Store) *eventFixture {
	t.Helper()
	src, err := st.AddSource(context.Background(), `C:\Photos`)
	check(t, err)
	return &eventFixture{t: t, st: st, src: src.ID}
}

func at(hours float64) int64 { return fixtureStart + int64(hours*3600) }

// add ingests a photo taken hours after the start and returns its id.
func (f *eventFixture) add(hours float64) int64 {
	f.t.Helper()
	return f.addAt(hours, nil)
}

// addAt is add with a location.
func (f *eventFixture) addAt(hours float64, loc *[2]float64) int64 {
	f.t.Helper()
	ctx := context.Background()
	f.n++
	name := fmt.Sprintf("p%d", f.n)
	check(f.t, errOf(f.st.Ingest(ctx, []IngestItem{item(f.src, `C:\Photos\`+name+`.jpg`, name)})))
	var id int64
	check(f.t, f.st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", []byte(name)).Scan(&id))
	f.redate(hours, loc, id)
	return id
}

// redate gives photos the date (and place) the metadata stage would read, in
// one transaction.
func (f *eventFixture) redate(hours float64, loc *[2]float64, ids ...int64) {
	f.t.Helper()
	rs := make([]MetadataResult, len(ids))
	for i, id := range ids {
		rs[i] = MetadataResult{AssetID: id, TakenAt: at(hours)}
		if loc != nil {
			rs[i].Lat, rs[i].Lon = &loc[0], &loc[1]
		}
	}
	check(f.t, f.st.SaveMetadata(context.Background(), "m", rs))
}

// event is the event holding id, as the views show it.
func (f *eventFixture) event(id int64) core.Event {
	f.t.Helper()
	gap, err := f.st.EventGapHours(context.Background())
	check(f.t, err)
	e, err := f.st.Event(context.Background(), id, gap)
	check(f.t, err)
	return e
}

// name renames the event holding id.
func (f *eventFixture) name(id int64, name string) {
	f.t.Helper()
	gap, err := f.st.EventGapHours(context.Background())
	check(f.t, err)
	check(f.t, f.st.SetEventName(context.Background(), id, name, gap))
}

func (f *eventFixture) pin(id int64, pinned bool) {
	f.t.Helper()
	gap, err := f.st.EventGapHours(context.Background())
	check(f.t, err)
	check(f.t, f.st.SetEventPinned(context.Background(), id, pinned, gap))
}

// tagsOf lists the event tags a photo holds, as "name:Trip" and "pin".
func tagsOf(t *testing.T, st *Store, id int64) []string {
	t.Helper()
	tags, err := queryAll(context.Background(), st.r, func(r *sql.Rows) (tag string, err error) {
		var kind, name string
		err = r.Scan(&kind, &name)
		if name != "" {
			kind += ":" + name
		}
		return kind, err
	}, `SELECT t.kind, COALESCE(t.name, '') FROM event_members m JOIN event_tags t ON t.id = m.tag_id
		WHERE m.asset_id = ? ORDER BY t.id`, id)
	check(t, err)
	return tags
}

func countRows(t *testing.T, st *Store, table string) int {
	t.Helper()
	var n int
	check(t, st.r.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n))
	return n
}

// writeInTx runs sql in a write transaction, as an indexer batch does.
func writeInTx(t *testing.T, st *Store, query string, args ...any) {
	t.Helper()
	check(t, st.tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), query, args...)
		return err
	}))
}

func TestASplitKeepsAnEventsNameOnBothHalvesAndAMergeCarriesIt(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a, b, c := f.add(0), f.add(2), f.add(4)
	f.name(a, "Trip")
	if n := countRows(t, st, "event_members"); n != 3 {
		t.Fatalf("%d members, want the event's three photos", n)
	}

	check(t, st.SetEventGapHours(ctx, 1)) // each photo is its own event now
	events, err := st.Events(ctx, 1)
	check(t, err)
	if len(events) != 3 {
		t.Fatalf("events at a one hour gap = %+v", events)
	}
	for _, e := range events {
		if e.Name != "Trip" {
			t.Errorf("a half of the split is named %q", e.Name)
		}
	}

	check(t, st.SetEventGapHours(ctx, 12))
	events, err = st.Events(ctx, 12)
	check(t, err)
	if len(events) != 1 || events[0].Name != "Trip" || events[0].Count != 3 {
		t.Fatalf("events after the gap came back = %+v", events)
	}
	for _, id := range []int64{a, b, c} {
		if got := tagsOf(t, st, id); !slices.Equal(got, []string{"name:Trip"}) {
			t.Errorf("photo %d holds %v", id, got)
		}
	}
}

func TestAGapChangeSettlesInTheTransactionThatStoresIt(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a, b := f.add(0), f.add(2)
	f.name(a, "Trip")
	check(t, st.SetEventGapHours(ctx, 1)) // two events, both named Trip
	f.name(a, "First")
	f.name(b, "Second")
	// Merging them again settles both names onto both photos. A failing
	// settle must take the new gap back with it.
	writeInTx(t, st, `CREATE TRIGGER refuse_settle BEFORE INSERT ON event_members BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	if err := st.SetEventGapHours(ctx, 12); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("a refused settle: err = %v", err)
	}
	if gap, _ := st.EventGapHours(ctx); gap != 1 {
		t.Fatalf("the gap is %d after a failed settle, want the old 1", gap)
	}
	var stored string
	check(t, st.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", core.SettingEventGapHours).Scan(&stored))
	if stored != "1" {
		t.Fatalf("stored gap %q after a failed settle", stored)
	}
	writeInTx(t, st, "DROP TRIGGER refuse_settle")
	check(t, st.SetEventGapHours(ctx, 12))
	if got := tagsOf(t, st, a); !slices.Equal(got, []string{"name:First", "name:Second"}) {
		t.Fatalf("the merged event's first photo holds %v, want both names", got)
	}
	events, err := st.Events(ctx, 12)
	check(t, err)
	if len(events) != 1 || events[0].Name != "First" {
		t.Fatalf("merged event = %+v, want the earlier name", events)
	}
}

func TestAPhotoThatJoinsAnEventTakesItsNameAndPinInTheTransactionThatAddsIt(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a := f.add(0)
	f.add(1)
	f.name(a, "Lunch")
	f.pin(a, true)

	// Ingested with a date inside the event: joins in the ingest transaction.
	joiner := item(f.src, `C:\Photos\joiner.jpg`, "joiner")
	joiner.TakenAt = at(0.5)
	check(t, errOf(st.Ingest(ctx, []IngestItem{joiner})))
	var id int64
	check(t, st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", []byte("joiner")).Scan(&id))
	if got := tagsOf(t, st, id); !slices.Equal(got, []string{"name:Lunch", "pin"}) {
		t.Fatalf("a photo ingested into the event holds %v", got)
	}

	// Another is ingested elsewhere and dated into the event later. If
	// settling left the transaction, a failure there could not undo the date.
	late := item(f.src, `C:\Photos\late.jpg`, "late")
	late.TakenAt = at(300)
	check(t, errOf(st.Ingest(ctx, []IngestItem{late})))
	var lateID int64
	check(t, st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", []byte("late")).Scan(&lateID))
	if got := tagsOf(t, st, lateID); len(got) != 0 {
		t.Fatalf("a photo far from the event holds %v", got)
	}
	writeInTx(t, st, `CREATE TRIGGER refuse_settle BEFORE INSERT ON event_members BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	if err := st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: lateID, TakenAt: at(0.7)}}); err == nil {
		t.Fatal("a re-dating whose settle failed was committed")
	}
	var taken int64
	check(t, st.r.QueryRowContext(ctx, "SELECT taken_at FROM assets WHERE id = ?", lateID).Scan(&taken))
	if taken != at(300) {
		t.Fatalf("the failed transaction left the new date: %d", taken)
	}
	writeInTx(t, st, "DROP TRIGGER refuse_settle")
	f.redate(0.7, nil, lateID)
	if got := tagsOf(t, st, lateID); !slices.Equal(got, []string{"name:Lunch", "pin"}) {
		t.Fatalf("a photo dated into the event holds %v", got)
	}
	if e := f.event(a); e.Count != 4 || !e.Pinned || e.Name != "Lunch" {
		t.Fatalf("event = %+v", e)
	}
}

func TestAPhotoRedatedIntoAnotherEventLeavesItsOldEventsTags(t *testing.T) {
	st := openTest(t)
	f := newEventFixture(t, st)
	a, b := f.add(0), f.add(1)
	c, d := f.add(48), f.add(49)
	f.name(a, "Morning")
	f.pin(a, true)
	f.name(c, "Evening")

	f.redate(48.5, nil, b)
	if got := tagsOf(t, st, b); !slices.Equal(got, []string{"name:Evening"}) {
		t.Fatalf("a photo that moved to the evening event holds %v", got)
	}
	for id, want := range map[int64][]string{a: {"name:Morning", "pin"}, c: {"name:Evening"}, d: {"name:Evening"}} {
		if got := tagsOf(t, st, id); !slices.Equal(got, want) {
			t.Errorf("photo %d holds %v, want %v", id, got, want)
		}
	}

	// Moved to an event of its own, it holds nothing; moved back into one, it
	// joins as any newcomer does.
	f.redate(200, nil, b)
	if got := tagsOf(t, st, b); len(got) != 0 {
		t.Fatalf("a photo that moved to an event of its own holds %v", got)
	}
	f.redate(48.2, nil, b)
	if got := tagsOf(t, st, b); !slices.Equal(got, []string{"name:Evening"}) {
		t.Fatalf("a photo that moved into the event again holds %v", got)
	}
}

func TestAWholeEventRedatedTogetherKeepsItsTags(t *testing.T) {
	st := openTest(t)
	f := newEventFixture(t, st)
	ids := []int64{f.add(0), f.add(1), f.add(2)}
	f.name(ids[0], "Wedding")
	f.pin(ids[0], true)
	other := f.add(100)
	f.name(other, "Other")

	f.redate(10*24, nil, ids...) // a time-zone fix, in one batch
	for _, id := range ids {
		if got := tagsOf(t, st, id); !slices.Equal(got, []string{"name:Wedding", "pin"}) {
			t.Errorf("photo %d holds %v after the whole event moved", id, got)
		}
	}
	if e := f.event(ids[0]); e.Name != "Wedding" || !e.Pinned || e.Count != 3 {
		t.Fatalf("event after the move = %+v", e)
	}
	if got := tagsOf(t, st, other); !slices.Equal(got, []string{"name:Other"}) {
		t.Fatalf("another event's photo holds %v", got)
	}
	// The move is settled: one of them moving alone now leaves.
	f.redate(30*24, nil, ids[0])
	if got := tagsOf(t, st, ids[0]); len(got) != 0 {
		t.Fatalf("a photo that left the moved event holds %v", got)
	}
}

func TestATrashedMemberKeepsItsMembershipWhileTrashed(t *testing.T) {
	st := openTest(t)
	f := newEventFixture(t, st)
	a, b := f.add(0), f.add(1)
	f.name(a, "Trip")
	for _, id := range []int64{a, b} {
		writeInTx(t, st, "UPDATE assets SET trashed_at = 1 WHERE id = ?", id)
	}
	if n := countRows(t, st, "event_members"); n != 2 {
		t.Fatalf("%d members while the whole event is trashed, want 2", n)
	}
	if n := countRows(t, st, "event_tags"); n != 1 {
		t.Fatalf("%d tags while every member is trashed, want 1", n)
	}
	// While it is away, the photos around it regroup; it is in no event.
	c := f.add(0.5)
	if got := tagsOf(t, st, c); len(got) != 0 {
		t.Fatalf("a photo that joined where the trashed event was holds %v", got)
	}
	writeInTx(t, st, "UPDATE assets SET trashed_at = NULL WHERE id = ?", a)
	if got := tagsOf(t, st, c); !slices.Equal(got, []string{"name:Trip"}) {
		t.Fatalf("a photo beside the restored one holds %v", got)
	}
	if e := f.event(a); e.Name != "Trip" || e.Count != 2 {
		t.Fatalf("event with the restored photo = %+v", e)
	}
	// Deleting every photo is the end of the name.
	writeInTx(t, st, "DELETE FROM assets")
	if n := countRows(t, st, "event_tags"); n != 0 {
		t.Fatalf("%d tags left when no photo remains", n)
	}
}

func TestPinnedEventsSortFirstNewestPinFirstFollowTheEventAndSurviveReopening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	ctx := context.Background()
	st, err := Open(path)
	check(t, err)
	f := newEventFixture(t, st)
	e1, e2, e3, e4 := f.add(0), f.add(48), f.add(96), f.add(144)
	order := func() []int64 {
		t.Helper()
		events, err := st.Events(ctx, core.EventGapDefault)
		check(t, err)
		ids := make([]int64, len(events))
		for i, e := range events {
			ids[i] = e.ID
		}
		return ids
	}
	if got := order(); !slices.Equal(got, []int64{e4, e3, e2, e1}) {
		t.Fatalf("unpinned order = %v", got)
	}
	f.pin(e1, true)
	f.pin(e3, true)
	f.pin(e1, true) // already pinned: keeps its place
	want := []int64{e3, e1, e4, e2}
	if got := order(); !slices.Equal(got, want) {
		t.Fatalf("pinned order = %v, want %v", got, want)
	}
	events, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if !events[0].Pinned || !events[1].Pinned || events[2].Pinned || events[3].Pinned {
		t.Fatalf("pinned flags = %+v", events)
	}
	// The pin follows the event: a photo joining e3 is in the pinned event.
	f.add(97)
	if events, err = st.Events(ctx, core.EventGapDefault); err != nil || events[0].ID != e3 || events[0].Count != 2 || !events[0].Pinned {
		t.Fatalf("pinned event after a photo joined = %+v, %v", events, err)
	}
	check(t, st.Close())

	st, err = Open(path)
	check(t, err)
	defer st.Close()
	if got := order(); !slices.Equal(got, want) {
		t.Fatalf("order after reopening = %v, want %v", got, want)
	}
	f.st = st
	f.pin(e3, false)
	if got := order(); !slices.Equal(got, []int64{e1, e4, e3, e2}) {
		t.Fatalf("order after unpinning = %v", got)
	}
	if n := countRows(t, st, "event_tags"); n != 1 {
		t.Fatalf("%d tags after unpinning one of two pins, want 1", n)
	}
	if err := st.SetEventPinned(ctx, 999, true, core.EventGapDefault); err == nil {
		t.Fatal("pinned an event that is not there")
	}
}

func TestRenamingAnEventReplacesNamesItsPhotosHeldAndAnEmptyNameClearsThem(t *testing.T) {
	st := openTest(t)
	f := newEventFixture(t, st)
	a, b := f.add(0), f.add(20) // two events at the default gap
	f.name(a, "First")
	f.name(b, "Second")
	check(t, st.SetEventGapHours(context.Background(), 24)) // one event holding both
	if got := tagsOf(t, st, b); !slices.Equal(got, []string{"name:First", "name:Second"}) {
		t.Fatalf("merged photo holds %v", got)
	}
	f.name(a, "Merged")
	for _, id := range []int64{a, b} {
		if got := tagsOf(t, st, id); !slices.Equal(got, []string{"name:Merged"}) {
			t.Errorf("photo %d holds %v after the rename", id, got)
		}
	}
	if n := countRows(t, st, "event_tags"); n != 1 {
		t.Fatalf("%d tags, want only the new name: the others have no photo", n)
	}
	f.name(a, "")
	if n := countRows(t, st, "event_members") + countRows(t, st, "event_tags"); n != 0 {
		t.Fatalf("an empty name left %d rows", n)
	}
}

func TestSettlingSplitsEventsAtLocationJumpsExactlyAsGroupingDoes(t *testing.T) {
	st := openTest(t)
	f := newEventFixture(t, st)
	lahore, islamabad := &[2]float64{31.5546, 74.3572}, &[2]float64{33.6938, 73.0652}
	a := f.addAt(0, lahore)
	f.addAt(1, lahore)
	c := f.addAt(2, islamabad)
	f.name(a, "Lahore day")
	if got := tagsOf(t, st, c); len(got) != 0 {
		t.Fatalf("the photo in the other city holds %v", got)
	}
	// Two more arrive: one in Lahore, and one in Islamabad between them in time.
	// Grouping puts the Islamabad one with c, and the Lahore one with a and b.
	g := f.addAt(1.2, islamabad)
	e := f.addAt(1.1, lahore)
	if got := tagsOf(t, st, e); !slices.Equal(got, []string{"name:Lahore day"}) {
		t.Fatalf("a photo that joined the Lahore event holds %v", got)
	}
	for _, id := range []int64{g, c} {
		if got := tagsOf(t, st, id); len(got) != 0 {
			t.Errorf("photo %d in the Islamabad event holds %v", id, got)
		}
	}
	events, err := st.Events(context.Background(), core.EventGapDefault)
	check(t, err)
	if len(events) != 2 || events[0].Count != 2 || events[1].Count != 3 || events[1].Name != "Lahore day" || events[1].ID != a {
		t.Fatalf("events = %+v", events)
	}
}

func TestSettlingLooksOnlyAtTheRunsAroundAChange(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	for _, h := range []float64{0, 1, 2, 30, 31, 60, 80} {
		f.add(h)
	}
	tx, err := st.r.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	check(t, err)
	defer func() { _ = tx.Rollback() }()
	settle := newSettler(tx, 12*3600)
	for _, c := range []struct {
		name   string
		change float64
		lo, hi float64
	}{
		{"inside a run", 31, 30, 31},
		{"in a hole too wide for either side", 15, 15, 15},
		{"in a hole beside the run before", 10, 0, 10},
		{"in a hole beside the run after", 20, 20, 31},
		{"in a hole beside two runs", 70, 60, 80},
	} {
		lo, hi, err := settle.runAround(ctx, at(c.change))
		check(t, err)
		if lo != at(c.lo) || hi != at(c.hi) {
			t.Errorf("%s (%gh): window %gh to %gh, want %gh to %gh", c.name, c.change,
				float64(lo-fixtureStart)/3600, float64(hi-fixtureStart)/3600, c.lo, c.hi)
		}
	}
}

func TestSettlingLeavesEventsAwayFromTheChangeUntouched(t *testing.T) {
	st := openTest(t)
	f := newEventFixture(t, st)
	far1, far2 := f.add(0), f.add(1)
	f.name(far1, "Far")
	near := f.add(100)
	f.name(near, "Near")
	// Mark the far event's photos moved. If settling regrouped it, it would
	// clear the marks (the tag has no unmoved member left to object).
	check(t, errOf(st.w.ExecContext(context.Background(), "UPDATE event_members SET moved = 1 WHERE asset_id IN (?, ?)", far1, far2)))
	f.add(100.5) // a change in the near event
	var moved int
	check(t, st.r.QueryRowContext(context.Background(), "SELECT count(*) FROM event_members WHERE moved = 1").Scan(&moved))
	if moved != 2 {
		t.Fatalf("%d members still marked moved, want the far event's two: it was regrouped", moved)
	}
}

func TestNothingIsRecordedOrSettledWhileNoEventHasATag(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a := f.add(0)
	dirty := func(tx *sql.Tx) int {
		t.Helper()
		var n int
		check(t, tx.QueryRowContext(ctx, "SELECT count(*) FROM event_dirty").Scan(&n))
		return n
	}
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE assets SET taken_at = taken_at + 1 WHERE id = ?", a); err != nil {
			return err
		}
		if n := dirty(tx); n != 0 {
			t.Errorf("%d times recorded with no event tag", n)
		}
		return nil
	}))
	f.name(a, "Trip")
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE assets SET taken_at = taken_at + 1 WHERE id = ?", a); err != nil {
			return err
		}
		if n := dirty(tx); n == 0 {
			t.Error("a change was not recorded while an event tag exists")
		}
		return nil
	}))
	if n := countRows(t, st, "event_dirty"); n != 0 {
		t.Fatalf("%d times left unsettled after the transaction", n)
	}
	// Dates the same event as a batch of two do: one row per time.
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", a)
		if n := dirty(tx); n != 1 {
			t.Errorf("%d times recorded for one photo changing visibility, want 1", n)
		}
		return err
	}))
}

// settleBenchmarkStore has 100,000 photos in events of 100 one minute apart,
// two days between events, and (with tagged) four events named or pinned,
// one of them the last, where the batches land.
func settleBenchmarkStore(b *testing.B, tagged bool) (*Store, context.Context, int64) {
	b.Helper()
	st, ctx := benchmarkOrganizationStore(b)
	if _, err := st.w.ExecContext(ctx, `UPDATE assets SET taken_at = 1767261600 + ((id - 1) / 100) * 172800 + ((id - 1) % 100) * 60`); err != nil {
		b.Fatal(err)
	}
	if _, err := st.w.ExecContext(ctx, "INSERT INTO sources (id, path, added_at) VALUES (1, 'C:\\Photos', 0)"); err != nil {
		b.Fatal(err)
	}
	if tagged {
		for i, id := range []int64{1, 5001, 50_001, 100_000} {
			if i%2 == 0 {
				if err := st.SetEventName(ctx, id, fmt.Sprintf("Event %d", i), core.EventGapDefault); err != nil {
					b.Fatal(err)
				}
			} else if err := st.SetEventPinned(ctx, id, true, core.EventGapDefault); err != nil {
				b.Fatal(err)
			}
		}
	}
	return st, ctx, int64(1767261600) + (99_999/100)*172800 // when the last event starts
}

// ingestBatchBenchmark times Ingest of 128 photos taken just after the
// library's last photo, deleting them again outside the timer.
func ingestBatchBenchmark(ctx context.Context, b *testing.B, st *Store, firstAt int64) {
	b.Helper()
	batch := make([]IngestItem, 128)
	serial := 0
	for b.Loop() {
		b.StopTimer()
		for i := range batch {
			serial++
			name := fmt.Sprintf("new-%d", serial)
			batch[i] = item(1, `C:\Photos\`+name+`.jpg`, name)
			batch[i].TakenAt = firstAt + int64(i)*30
		}
		b.StartTimer()
		if _, err := st.Ingest(ctx, batch); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := st.tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DELETE FROM assets WHERE id > 100000")
			return err
		}); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// BenchmarkIngestBatchWithEventTags is an ingest-sized write: 128 new photos
// joining the library's last event, on 100,000 photos, with and without event
// tags (a named and a pinned event each among them), and without the triggers
// that record changes for settling. With no tag a transaction pays one probe
// for the settle, and each photo one more in its trigger.
func BenchmarkIngestBatchWithEventTags(b *testing.B) {
	for _, c := range []struct {
		name     string
		tagged   bool
		triggers bool
	}{{"no triggers", false, false}, {"no tags", false, true}, {"tags", true, true}} {
		b.Run(c.name, func(b *testing.B) {
			st, ctx, lastEvent := settleBenchmarkStore(b, c.tagged)
			if !c.triggers {
				for _, trigger := range []string{"event_dirty_asset_added", "event_dirty_asset_deleted", "event_dirty_asset_changed", "event_member_moved"} {
					if _, err := st.w.ExecContext(ctx, "DROP TRIGGER "+trigger); err != nil {
						b.Fatal(err)
					}
				}
			}
			ingestBatchBenchmark(ctx, b, st, lastEvent+7200) // soon after the last event's last photo
		})
	}
}

// BenchmarkSettleOneHugeEvent is the worst case for settling: every photo is
// in one run, because the library is a continuous minute-by-minute stream,
// and a batch lands in it. The run is regrouped from end to end.
func BenchmarkSettleOneHugeEvent(b *testing.B) {
	for _, tagged := range []bool{false, true} {
		b.Run(map[bool]string{false: "no tags", true: "tags"}[tagged], func(b *testing.B) {
			st, ctx := benchmarkOrganizationStore(b)
			if _, err := st.w.ExecContext(ctx, "INSERT INTO sources (id, path, added_at) VALUES (1, 'C:\\Photos', 0)"); err != nil {
				b.Fatal(err)
			}
			if tagged {
				if err := st.SetEventName(ctx, 1, "Everything", core.EventGapDefault); err != nil {
					b.Fatal(err)
				}
			}
			ingestBatchBenchmark(ctx, b, st, 1767261600+3*3600)
		})
	}
}

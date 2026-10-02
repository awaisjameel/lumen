package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"lumen/internal/core"
)

// block blocks the event holding id, in mode, and returns the block's id.
func (f *eventFixture) block(id int64, mode core.BlockMode) int64 {
	f.t.Helper()
	blockID, err := f.st.SetBlock(context.Background(), core.BlockEvent, id, "", mode)
	check(f.t, err)
	return blockID
}

// addVideo is add for a video.
func (f *eventFixture) addVideo(hours float64) int64 {
	f.t.Helper()
	ctx := context.Background()
	f.n++
	name := fmt.Sprintf("clip%d", f.n)
	clip := item(f.src, `C:\Photos\`+name+`.mp4`, name)
	clip.Kind = core.KindVideo
	check(f.t, errOf(f.st.Ingest(ctx, []IngestItem{clip})))
	var id int64
	check(f.t, f.st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", []byte(name)).Scan(&id))
	f.redate(hours, nil, id)
	return id
}

// visibleIDs is the photos the library shows, by id.
func visibleIDs(t *testing.T, st *Store) []int64 {
	t.Helper()
	ids, err := queryAll(context.Background(), st.r, scanID, "SELECT id FROM assets WHERE "+visible+" ORDER BY id")
	check(t, err)
	return ids
}

// wantVisible fails unless exactly the photos want are shown.
func wantVisible(t *testing.T, st *Store, when string, want ...int64) {
	t.Helper()
	slices.Sort(want)
	if got := visibleIDs(t, st); !slices.Equal(got, want) {
		t.Fatalf("%s: the library shows %v, want %v", when, got, want)
	}
}

func TestAPhotoThatJoinsABlockedEventIsHiddenInTheTransactionThatAddsOrRedatesIt(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a := f.add(0)
	f.add(1)
	far := f.add(300)
	f.block(a, core.BlockPhotos)
	wantVisible(t, st, "after blocking the event", far)

	// Ingested with a date inside the event: hidden as it commits.
	joiner := item(f.src, `C:\Photos\joiner.jpg`, "joiner")
	joiner.TakenAt = at(0.5)
	check(t, errOf(st.Ingest(ctx, []IngestItem{joiner})))
	wantVisible(t, st, "after a photo was ingested into the event", far)

	// Dated into the event later. If the settle left the transaction, a
	// failure there could not undo the date and the photo would show.
	late := f.add(200)
	wantVisible(t, st, "before the late photo is dated into the event", far, late)
	writeInTx(t, st, `CREATE TRIGGER refuse_settle BEFORE INSERT ON event_members BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	if err := st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: late, TakenAt: at(0.7)}}); err == nil {
		t.Fatal("a re-dating whose settle failed was committed")
	}
	wantVisible(t, st, "after the re-dating was refused", far, late)
	writeInTx(t, st, "DROP TRIGGER refuse_settle")
	f.redate(0.7, nil, late)
	wantVisible(t, st, "after a photo was dated into the event", far)

	if n := countRows(t, st, "event_members"); n != 4 {
		t.Fatalf("the block holds %d photos, want the 2 it had and the 2 that joined", n)
	}
}

func TestTheVisibilityRevisionMovesWhenAPhotoJoinsOrLeavesAPhotoModeEventBlock(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a := f.add(0)
	far := f.add(300)
	other := f.add(600)
	moved := func(what string, change func(), want bool) {
		t.Helper()
		before, err := st.VisibilityRevision(ctx)
		check(t, err)
		change()
		after, err := st.VisibilityRevision(ctx)
		check(t, err)
		if got := after != before; got != want {
			t.Errorf("%s: revision moved = %v, want %v", what, got, want)
		}
	}
	moved("blocking the event", func() { f.block(a, core.BlockPhotos) }, true)
	moved("a photo joining it", func() { f.redate(0.5, nil, far) }, true)
	moved("a photo leaving it", func() { f.redate(300, nil, far) }, true)
	moved("a photo moving between other events", func() { f.redate(601, nil, other) }, false)
}

func TestAPhotoRedatedOutOfABlockedEventComesBackAndIsHiddenAgainWhenItReturns(t *testing.T) {
	st := openTest(t)
	f := newEventFixture(t, st)
	a, b, c := f.add(0), f.add(1), f.add(2)
	d := f.add(100)
	f.block(a, core.BlockPhotos)
	wantVisible(t, st, "after blocking the event", d)

	f.redate(100.5, nil, b) // into the next event, which is not blocked
	wantVisible(t, st, "after one photo moved to another event", b, d)
	if got := tagsOf(t, st, b); len(got) != 0 {
		t.Fatalf("the leaver holds %v", got)
	}
	f.redate(500, nil, c) // to an event of its own
	wantVisible(t, st, "after another moved to an event of its own", b, c, d)

	f.redate(1, nil, b) // and back: joins as any newcomer does
	wantVisible(t, st, "after the first photo came back", c, d)
	if got := tagsOf(t, st, b); !slices.Equal(got, []string{"block"}) {
		t.Fatalf("the returning photo holds %v", got)
	}
	if got := tagsOf(t, st, a); !slices.Equal(got, []string{"block"}) {
		t.Fatalf("a photo that stayed holds %v", got)
	}
}

func TestASplitByATrashedPhotoKeepsBothHalvesOfABlockedEventHidden(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a, middle, c := f.add(0), f.add(8), f.add(16) // 8 h apart: one event of 12 h gaps
	f.block(a, core.BlockPhotos)
	check(t, st.TrashAssets(ctx, []int64{middle})) // 16 h between the others: two events
	wantVisible(t, st, "with the bridging photo in the Trash")
	for _, id := range []int64{a, c, middle} {
		if got := tagsOf(t, st, id); !slices.Equal(got, []string{"block"}) {
			t.Errorf("photo %d holds %v after the split", id, got)
		}
	}

	// A photo that lands beside either half joins it.
	f.add(16.5)
	wantVisible(t, st, "after a photo landed beside the second half")
	check(t, st.RestoreAssets(ctx, []int64{middle}))
	wantVisible(t, st, "after the bridging photo came back")
	if n := countRows(t, st, "event_members"); n != 4 {
		t.Fatalf("the block holds %d photos, want 4", n)
	}
}

func TestAGapChangeSettlesABlockedEventInTheTransactionThatStoresIt(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a := f.add(0)
	f.add(5)
	f.add(10)
	d := f.add(30)
	f.block(a, core.BlockPhotos)
	wantVisible(t, st, "after blocking the event", d)

	// Splitting keeps every half blocked.
	check(t, st.SetEventGapHours(ctx, 4))
	wantVisible(t, st, "after the event was split into three", d)

	// Merging it with the next event takes that in, and a failed settle takes
	// the gap back.
	writeInTx(t, st, `CREATE TRIGGER refuse_settle BEFORE INSERT ON event_members BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	if err := st.SetEventGapHours(ctx, 24); err == nil {
		t.Fatal("a gap change whose settle failed was committed")
	}
	wantVisible(t, st, "after a refused gap change", d)
	writeInTx(t, st, "DROP TRIGGER refuse_settle")
	check(t, st.SetEventGapHours(ctx, 24))
	wantVisible(t, st, "after the event merged with the next")
	if got := tagsOf(t, st, d); !slices.Equal(got, []string{"block"}) {
		t.Fatalf("the photo the merge took in holds %v", got)
	}
}

func TestEventBlocksNamesAndPinsShareOneIdentityThroughASplitAndALeaver(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a, b, c, d := f.add(0), f.add(6), f.add(12), f.add(18)
	other := f.add(100)
	f.name(a, "Trip")
	f.pin(a, true)
	f.block(a, core.BlockPhotos)
	every := []string{"name:Trip", "pin", "block"}
	for _, id := range []int64{a, b, c, d} {
		if got := tagsOf(t, st, id); !slices.Equal(got, every) {
			t.Fatalf("photo %d holds %v, want %v", id, got, every)
		}
	}

	// One photo is dated into another event: it leaves the name, the pin and
	// the block together and shows again.
	f.redate(100.5, nil, b)
	if got := tagsOf(t, st, b); len(got) != 0 {
		t.Fatalf("the leaver holds %v", got)
	}
	wantVisible(t, st, "after the leaver", b, other)

	// The rest split in three: each half keeps all of them.
	check(t, st.SetEventGapHours(ctx, 5))
	for _, id := range []int64{a, c, d} {
		if got := tagsOf(t, st, id); !slices.Equal(got, every) {
			t.Fatalf("photo %d holds %v after the split, want %v", id, got, every)
		}
	}
	wantVisible(t, st, "after the split", b, other)

	// Unblocking leaves the name and the pin on all three events.
	check(t, st.RemoveBlock(ctx, f.blockID(t)))
	wantVisible(t, st, "after unblocking", a, b, c, d, other)
	events, err := st.Events(ctx, 5)
	check(t, err)
	var named int
	for _, e := range events {
		if e.Name == "Trip" && e.Pinned {
			named++
		}
	}
	if named != 3 {
		t.Fatalf("%d events are named and pinned after unblocking, want 3: %+v", named, events)
	}
	if got := tagsOf(t, st, a); !slices.Equal(got, []string{"name:Trip", "pin"}) {
		t.Fatalf("an unblocked photo holds %v", got)
	}
}

// blockID is the id of the only block there is.
func (f *eventFixture) blockID(t *testing.T) int64 {
	t.Helper()
	blocks, err := f.st.Blocks(context.Background())
	check(t, err)
	if len(blocks) != 1 {
		t.Fatalf("%d blocks, want 1", len(blocks))
	}
	return blocks[0].ID
}

func TestLabelModeHidesTheEventButNotItsPhotosAndUnblockingRestoresIt(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a, b := f.add(0), f.add(1)
	c := f.add(100)
	f.name(a, "Wedding")
	f.block(a, core.BlockLabel)

	events, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(events) != 1 || events[0].ID != c {
		t.Fatalf("events = %+v, want only the unblocked one", events)
	}
	for _, id := range []int64{a, b} {
		if _, err := st.Event(ctx, id, core.EventGapDefault); !errors.Is(err, ErrNotFound) {
			t.Errorf("Event(%d) = %v, want ErrNotFound", id, err)
		}
		if ok, err := st.AssetVisible(ctx, id, false); err != nil || !ok {
			t.Errorf("photo %d visible = %v, %v: a label block leaves the photos", id, ok, err)
		}
	}
	wantVisible(t, st, "after a label block", a, b, c)

	// The event stays away as it regroups: a joiner is not an event of its own.
	joiner := f.add(0.5)
	events, err = st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(events) != 1 {
		t.Fatalf("a photo joining a blocked event made %+v", events)
	}
	wantVisible(t, st, "after a photo joined", a, b, c, joiner)
	// One that leaves it is an ordinary photo again.
	f.redate(100.5, nil, b)
	if events, err = st.Events(ctx, core.EventGapDefault); err != nil || len(events) != 1 || events[0].Count != 2 {
		t.Fatalf("after a photo moved to the other event: %+v, %v", events, err)
	}

	check(t, st.RemoveBlock(ctx, f.blockID(t)))
	events, err = st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(events) != 2 {
		t.Fatalf("events after unblocking = %+v, want 2", events)
	}
	if e := f.event(a); e.Name != "Wedding" || e.Count != 2 {
		t.Fatalf("the unblocked event = %+v, want its name and photos back", e)
	}
	wantVisible(t, st, "after unblocking", a, b, c, joiner)
}

func TestASwitchBetweenLabelAndPhotoModeHidesAndShowsThePhotosOfTheSameBlock(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a, b := f.add(0), f.add(1)
	label := f.block(a, core.BlockLabel)
	wantVisible(t, st, "label mode", a, b)

	// The photos are visible, but the block is found through them.
	id, created, err := st.SetBlockWithCreated(ctx, core.BlockEvent, b, "", core.BlockPhotos, true)
	check(t, err)
	if created || id != label {
		t.Fatalf("blocking the event again: id %d, created %v; want block %d unchanged", id, created, label)
	}
	wantVisible(t, st, "photo mode")
	// Now they are hidden, the block is still found through them.
	if _, err := st.Event(ctx, a, core.EventGapDefault); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a hidden event is found: %v", err)
	}
	id, created, err = st.SetBlockWithCreated(ctx, core.BlockEvent, a, "", core.BlockLabel, true)
	check(t, err)
	if created || id != label {
		t.Fatalf("switching back: id %d, created %v; want block %d unchanged", id, created, label)
	}
	wantVisible(t, st, "label mode again", a, b)
	if n := countRows(t, st, "event_tags"); n != 1 {
		t.Fatalf("%d event tags after switching modes, want 1", n)
	}

	// Without the PIN's permission to change a rule, a switch is refused and
	// the same mode asked again is not.
	if _, _, err := st.SetBlockWithCreated(ctx, core.BlockEvent, a, "", core.BlockPhotos, false); !errors.Is(err, ErrBlockChangeLocked) {
		t.Fatalf("a locked switch: %v", err)
	}
	if _, created, err := st.SetBlockWithCreated(ctx, core.BlockEvent, a, "", core.BlockLabel, false); err != nil || created {
		t.Fatalf("the same mode again: created %v, %v", created, err)
	}
}

func TestBlocksNamesAnEventBlockByTheNameItHadWhenItWasBlocked(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a := f.add(0)
	f.name(a, "Wedding")
	wedding := f.block(a, core.BlockLabel)
	plain := f.add(100)
	generated := f.event(plain).Name
	other := f.block(plain, core.BlockPhotos)

	// It stays so as the event regroups and gains photos.
	f.add(0.5)
	check(t, st.SetEventGapHours(ctx, 1))

	blocks, err := st.Blocks(ctx)
	check(t, err)
	names := map[int64]string{}
	for _, blk := range blocks {
		if blk.Kind != core.BlockEvent || blk.TargetID == 0 {
			t.Errorf("block = %+v, want an event block with a target", blk)
		}
		names[blk.ID] = blk.Name
	}
	if names[wedding] != "Wedding" || names[other] != generated {
		t.Fatalf("names = %v, want %q and %q", names, "Wedding", generated)
	}
}

func TestBlockingAnEventAgainFromAnyOfItsPhotosMakesNoSecondBlock(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a, b := f.add(0), f.add(1)
	first := f.block(a, core.BlockPhotos)
	for _, id := range []int64{a, b} {
		got, created, err := st.SetBlockWithCreated(ctx, core.BlockEvent, id, "", core.BlockPhotos, false)
		check(t, err)
		if created || got != first {
			t.Fatalf("blocking again from photo %d: block %d, created %v; want %d unchanged", id, got, created, first)
		}
	}
	if n, m := countRows(t, st, "event_tags"), countRows(t, st, "blocks"); n != 1 || m != 1 {
		t.Fatalf("%d event tags and %d blocks, want 1 each", n, m)
	}
}

func TestBlockingAnEventNeedsAPhotoThatIsInOne(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a, b := f.add(0), f.add(1)
	check(t, st.TrashAssets(ctx, []int64{b}))
	for _, id := range []int64{b, 9999} {
		if _, err := st.SetBlock(ctx, core.BlockEvent, id, "", core.BlockPhotos); !errors.Is(err, ErrNotFound) {
			t.Errorf("blocking the event of photo %d: %v, want ErrNotFound", id, err)
		}
	}
	if n, m := countRows(t, st, "event_tags"), countRows(t, st, "blocks"); n != 0 || m != 0 {
		t.Fatalf("a refused block left %d event tags and %d blocks", n, m)
	}
	wantVisible(t, st, "after refused blocks", a)
}

func TestRemovingAnEventBlockTakesItsTagWithIt(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a, b := f.add(0), f.add(1)
	f.name(a, "Trip")
	id := f.block(a, core.BlockPhotos)
	if n := countRows(t, st, "event_tags"); n != 2 {
		t.Fatalf("%d event tags with a name and a block, want 2", n)
	}
	check(t, st.RemoveBlock(ctx, id))
	if n, m := countRows(t, st, "event_members"), countRows(t, st, "event_tags"); n != 2 || m != 1 {
		t.Fatalf("%d members and %d tags after unblocking, want the name's 2 and 1", n, m)
	}
	wantVisible(t, st, "after unblocking", a, b)
}

func TestAVideoInABlockedEventIsNeitherShownNorServed(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	f := newEventFixture(t, st)
	a := f.add(0)
	clip := f.addVideo(0.5)
	blockID := f.block(a, core.BlockPhotos)

	if ok, err := st.AssetVisible(ctx, clip, false); err != nil || ok {
		t.Fatalf("a blocked video is servable = %v, %v", ok, err)
	}
	if _, err := st.AssetFile(ctx, clip, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a blocked video's file: %v, want ErrNotFound", err)
	}
	// The Blocked screen's browser, while unlocked, may open it.
	if _, err := st.AssetFile(ctx, clip, true); err != nil {
		t.Fatalf("the video while the Blocked screen is unlocked: %v", err)
	}
	// A video that joins is covered too.
	joiner := f.addVideo(0.7)
	if ok, err := st.AssetVisible(ctx, joiner, false); err != nil || ok {
		t.Fatalf("a video that joined is servable = %v, %v", ok, err)
	}
	check(t, st.RemoveBlock(ctx, blockID))
	if m, err := st.AssetFile(ctx, clip, false); err != nil || m.Kind != core.KindVideo {
		t.Fatalf("an unblocked video's file = %+v, %v", m, err)
	}
}

// BenchmarkBlockedTimelineWithAnEventBlock is BenchmarkBlockedTimeline's
// count on 100,000 photos in events of 100, with no block and with one event
// blocked in photo mode: the new branch of blockMatch must not slow the
// predicate down.
func BenchmarkBlockedTimelineWithAnEventBlock(b *testing.B) {
	for _, blocked := range []bool{false, true} {
		b.Run(map[bool]string{false: "no block", true: "one event block"}[blocked], func(b *testing.B) {
			st, ctx, _ := settleBenchmarkStore(b, false)
			want := 100_000
			if blocked {
				if _, err := st.SetBlock(ctx, core.BlockEvent, 5001, "", core.BlockPhotos); err != nil {
					b.Fatal(err)
				}
				want -= 100
			}
			for b.Loop() {
				if n, err := st.CountVisible(ctx); err != nil || n != want {
					b.Fatalf("visible = %d, %v; want %d", n, err, want)
				}
			}
		})
	}
}

// BenchmarkIngestBatchIntoABlockedEvent is BenchmarkIngestBatchWithEventTags's
// batch landing in the library's last event, when that event is blocked: the
// settle adds the 128 photos to the block.
func BenchmarkIngestBatchIntoABlockedEvent(b *testing.B) {
	st, ctx, lastEvent := settleBenchmarkStore(b, false)
	if _, err := st.SetBlock(ctx, core.BlockEvent, 100_000, "", core.BlockPhotos); err != nil {
		b.Fatal(err)
	}
	ingestBatchBenchmark(ctx, b, st, lastEvent+7200)
}

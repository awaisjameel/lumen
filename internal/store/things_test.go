package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

// downgradeToV18 puts st's schema back to version 18: it drops what migration
// 0019 added and gives back event_names, empty, as 0011 made it. The
// upgrade tests use it to build the library an older Lumen left.
func downgradeToV18(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	// This fixture deliberately removes settlement's schema until the next
	// Open, so build the old database through the raw pool as migrations do.
	st.w.execContext = st.w.DB.ExecContext
	// The kept counts (migration 0037) read tables this version lacks, so they
	// go with their triggers.
	for _, table := range []string{"day_counts", "tag_counts", "person_counts_faces", "person_counts", "job_counts", "count_rebuild", "trash_count"} {
		triggers, err := queryAll(ctx, st.r, func(r *sql.Rows) (name string, err error) { err = r.Scan(&name); return name, err }, "SELECT name FROM sqlite_master WHERE type = 'trigger' AND name LIKE ? || '_%'", table)
		check(t, err)
		for _, name := range triggers {
			check(t, errOf(st.w.ExecContext(ctx, "DROP TRIGGER "+name)))
		}
		check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE "+table)))
	}
	restoreBlockRevisionTriggers(ctx, t, st)
	for _, table := range []string{"blocked_assets", "blocked_dirty", "blocked_rebuild"} {
		check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE "+table)))
	}
	selectionTriggers, err := queryAll(ctx, st.r, func(r *sql.Rows) (name string, err error) { err = r.Scan(&name); return name, err }, "SELECT name FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'selection_%'")
	check(t, err)
	for _, name := range selectionTriggers {
		check(t, errOf(st.w.ExecContext(ctx, "DROP TRIGGER "+name)))
	}
	check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE selection_revision")))
	for _, trigger := range []string{"duplicate_asset_changed", "duplicate_file_added", "duplicate_file_deleted", "duplicate_file_changed", "duplicate_original_date_changed"} {
		check(t, errOf(st.w.ExecContext(ctx, "DROP TRIGGER "+trigger)))
	}
	check(t, errOf(st.w.ExecContext(ctx, "ALTER TABLE event_revision DROP COLUMN duplicate_value")))
	for _, column := range []string{"display_rotation", "display_flip", "display_version", "original_taken_at", "original_date_from", "location_corrected", "original_lat", "original_lon", "original_place_id"} {
		check(t, errOf(st.w.ExecContext(ctx, "ALTER TABLE assets DROP COLUMN "+column)))
	}
	check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE playback_history")))
	check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE export_jobs")))
	check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE export_batches")))
	// Migration 0023's trigger on blocks reads event_tags, so it goes too: a
	// version 18 library has no such trigger, and the replay makes it again.
	for _, trigger := range []string{"block_event_target_removed", "event_tag_added", "event_tag_changed", "event_tag_deleted", "event_member_added",
		"event_member_deleted", "event_dirty_asset_added", "event_dirty_asset_deleted", "event_dirty_asset_changed",
		"event_member_moved"} {
		check(t, errOf(st.w.ExecContext(ctx, "DROP TRIGGER "+trigger)))
	}
	for _, drop := range []string{"INDEX tags_vocab_name", "TABLE event_members", "TABLE event_tags", "TABLE event_dirty"} {
		check(t, errOf(st.w.ExecContext(ctx, "DROP "+drop)))
	}
	for _, column := range []string{"vocab_name", "pinned_at"} {
		check(t, errOf(st.w.ExecContext(ctx, "ALTER TABLE tags DROP COLUMN "+column)))
	}
	check(t, errOf(st.w.ExecContext(ctx, `CREATE TABLE event_names (
			anchor_asset_id INTEGER PRIMARY KEY REFERENCES assets(id) ON DELETE CASCADE,
			name TEXT NOT NULL);
		CREATE TRIGGER event_name_added AFTER INSERT ON event_names BEGIN
			UPDATE event_revision SET value = value + 1 WHERE id = 1; END;
		CREATE TRIGGER event_name_changed AFTER UPDATE ON event_names BEGIN
			UPDATE event_revision SET value = value + 1 WHERE id = 1; END;
		CREATE TRIGGER event_name_deleted AFTER DELETE ON event_names BEGIN
			UPDATE event_revision SET value = value + 1 WHERE id = 1; END;
		PRAGMA user_version = 18`)))
}

// restoreBlockRevisionTriggers takes the original definitions from a scratch
// schema, so downgrade fixtures need not maintain copies of revision SQL.
func restoreBlockRevisionTriggers(ctx context.Context, t *testing.T, st *Store) {
	t.Helper()
	legacy, err := sql.Open(driverName, ":memory:")
	check(t, err)
	defer func() { _ = legacy.Close() }()
	legacy.SetMaxOpenConns(1)
	names, err := migrationFS.ReadDir("migrations")
	check(t, err)
	for _, name := range names {
		if name.Name() >= "0032" {
			break
		}
		body, err := migrationFS.ReadFile("migrations/" + name.Name())
		check(t, err)
		check(t, errOf(legacy.ExecContext(ctx, string(body))))
	}
	triggers, err := queryAll(ctx, st.r, func(r *sql.Rows) (name string, err error) { err = r.Scan(&name); return name, err },
		`SELECT name FROM sqlite_master WHERE type = 'trigger'
		AND (instr(sql, 'blocked_dirty') > 0 OR instr(sql, 'blocked_rebuild') > 0 OR name LIKE 'blocked_%')`)
	check(t, err)
	for _, name := range triggers {
		var original string
		err := legacy.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?", name).Scan(&original)
		if !errors.Is(err, sql.ErrNoRows) {
			check(t, err)
		}
		check(t, errOf(st.w.ExecContext(ctx, "DROP TRIGGER "+name)))
		if original != "" {
			check(t, errOf(st.w.ExecContext(ctx, original)))
		}
	}
}

// thingNamed is the Thing the list shows under name.
func thingNamed(t *testing.T, st *Store, name string) core.Tag {
	t.Helper()
	things, err := st.Things(context.Background(), core.TagConfidenceDefault)
	check(t, err)
	for _, thing := range things {
		if thing.Name == name {
			return thing
		}
	}
	t.Fatalf("no Thing named %q in %+v", name, things)
	return core.Tag{}
}

func thingNames(t *testing.T, st *Store) []string {
	t.Helper()
	things, err := st.Things(context.Background(), core.TagConfidenceDefault)
	check(t, err)
	names := make([]string, len(things))
	for i, thing := range things {
		names[i] = thing.Name
	}
	return names
}

func syncOne(t *testing.T, st *Store, labels ...VocabLabel) {
	t.Helper()
	check(t, st.SyncVocabulary(context.Background(), labels))
}

func label(name string) VocabLabel {
	return VocabLabel{Name: name, Prompts: []string{"a photo of " + name}}
}

func autoTag(t *testing.T, st *Store, id int64) AutoTag {
	t.Helper()
	tags, err := st.AutoTags(context.Background())
	check(t, err)
	for _, tag := range tags {
		if tag.ID == id {
			return tag
		}
	}
	t.Fatalf("tag %d is no longer applied by the AI: %+v", id, tags)
	return AutoTag{}
}

func TestRenamedBuiltinThingShowsAndMatchesUnderItsNewNameOnly(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	syncOne(t, st, label("Beaches"), label("Dogs"))
	beaches, _, err := st.TagByName(ctx, "beaches")
	check(t, err)
	check(t, st.RenameTag(ctx, beaches, "  Seaside  "))

	renamed := thingNamed(t, st, "Seaside")
	if renamed.ID != beaches || renamed.Original != "Beaches" || renamed.Kind != core.TagBuiltin {
		t.Fatalf("renamed Thing = %+v, want id %d with original Beaches", renamed, beaches)
	}
	if slices.Contains(thingNames(t, st), "Beaches") {
		t.Fatalf("the old name is still listed: %v", thingNames(t, st))
	}
	if dogs := thingNamed(t, st, "Dogs"); dogs.Original != "" {
		t.Fatalf("a built-in nobody renamed reports an original: %+v", dogs)
	}
	// The AI keeps looking for what it always did.
	auto := autoTag(t, st, beaches)
	if auto.Name != "Seaside" || auto.VocabName != "Beaches" || !auto.Builtin || !slices.Equal(auto.Prompts, []string{"a photo of Beaches"}) {
		t.Fatalf("renamed built-in as the AI sees it = %+v", auto)
	}

	// The search box follows the new name and forgets the old one.
	if got, _, err := st.TagByName(ctx, "SEASIDE"); err != nil || got != beaches {
		t.Fatalf("TagByName(new) = %d, %v", got, err)
	}
	if _, _, err := st.TagByName(ctx, "Beaches"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TagByName(old) = %v, want ErrNotFound", err)
	}
	if got, err := st.SearchTags(ctx, "seasi", 5); err != nil || len(got) != 1 || got[0].ID != beaches {
		t.Fatalf("SearchTags(new) = %+v, %v", got, err)
	}
	if got, err := st.SearchTags(ctx, "beach", 5); err != nil || len(got) != 0 {
		t.Fatalf("SearchTags(old) = %+v, %v", got, err)
	}
	targets := func(query string) []string {
		found, err := st.SearchNames(ctx, query)
		check(t, err)
		var names []string
		for _, target := range found {
			if target.Kind == core.BlockTag {
				names = append(names, target.Name)
			}
		}
		return names
	}
	if got := targets("a seaside walk"); !slices.Equal(got, []string{"Seaside"}) {
		t.Fatalf("search names for the new name = %v", got)
	}
	if got := targets("beaches at dawn"); len(got) != 0 {
		t.Fatalf("search names still match the old name: %v", got)
	}

	// An empty name gives the built-in its own back.
	check(t, st.RenameTag(ctx, beaches, ""))
	if reset := thingNamed(t, st, "Beaches"); reset.ID != beaches || reset.Original != "" {
		t.Fatalf("reset Thing = %+v", reset)
	}
	if _, _, err := st.TagByName(ctx, "Seaside"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the name before the reset still matches: %v", err)
	}
}

func TestRenamingAThingRefusesTakenAndBlockedNamesAndBadNames(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	syncOne(t, st, label("Beaches"), label("Dogs"))
	beaches, _, err := st.TagByName(ctx, "Beaches")
	check(t, err)
	trip, err := st.CreateTag(ctx, "Été", core.TagManual, nil)
	check(t, err)
	secret, err := st.CreateTag(ctx, "Secret", core.TagManual, nil)
	check(t, err)
	_, err = st.SetBlock(ctx, core.BlockTag, secret, "", core.BlockLabel)
	check(t, err)

	for _, c := range []struct {
		name string
		id   int64
		to   string
		want error
	}{
		{"a name another Thing has", beaches, "dogs", ErrTagExists},
		{"a name another Thing has in a letter SQLite does not fold", beaches, "ÉTÉ", ErrTagExists},
		{"a blocked Thing's name", beaches, "secret", ErrBlockedName},
		{"a name that is too long", beaches, strings.Repeat("x", core.MaxNameLen+1), ErrInvalidName},
		{"no name for a Thing the user made", trip, "  ", ErrInvalidName},
		{"a Thing that is gone", 999, "Anything", ErrNotFound},
	} {
		if err := st.RenameTag(ctx, c.id, c.to); !errors.Is(err, c.want) {
			t.Errorf("renaming to %s: err = %v, want %v", c.name, err, c.want)
		}
	}
	// A refused rename changes nothing, and a Thing may be renamed in case only.
	if got := thingNamed(t, st, "Beaches"); got.Original != "" {
		t.Fatalf("a refused rename left %+v", got)
	}
	check(t, st.RenameTag(ctx, trip, "été"))
	thingNamed(t, st, "été")
}

func TestRenamingAUserLabelKeepsItsPrompts(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	id, err := st.CreateTag(ctx, "Eiffel Tower", core.TagLabel, []string{"the Eiffel Tower"})
	check(t, err)
	check(t, st.RenameTag(ctx, id, "Paris tower"))
	auto := autoTag(t, st, id)
	if auto.Name != "Paris tower" || auto.VocabName != "" || auto.Builtin || !slices.Equal(auto.Prompts, []string{"the Eiffel Tower"}) {
		t.Fatalf("renamed label = %+v", auto)
	}
	if thing := thingNamed(t, st, "Paris tower"); thing.Original != "" || thing.Kind != core.TagLabel {
		t.Fatalf("renamed label in the list = %+v", thing)
	}
}

func TestVocabularyUpdatesKeepRenamedAndPinnedBuiltinsWithTheirMatches(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	syncOne(t, st, label("Beaches"), label("Dogs"), label("Cats"), label("Cars"))
	id := func(name string) int64 {
		t.Helper()
		tag, _, err := st.TagByName(ctx, name)
		check(t, err)
		return tag
	}
	beaches, dogs, cats, cars := id("Beaches"), id("Dogs"), id("Cats"), id("Cars")
	for _, tag := range []int64{beaches, dogs, cats, cars} {
		check(t, st.SetTagScores(ctx, tag, []AssetScore{{AssetID: 1, Score: 0.9}}))
	}
	check(t, st.SetTagVectors(ctx, beaches, "m", []float32{1, 0}))
	check(t, st.RenameTag(ctx, beaches, "Seaside"))
	check(t, st.SetTagPinned(ctx, dogs, true))
	check(t, st.RenameTag(ctx, cats, "Kittens"))
	check(t, st.SetTagPinned(ctx, cats, true))

	// The vocabulary changes the prompts of Beaches and Dogs and retires the
	// other two (and Cars, which nobody touched).
	changed := VocabLabel{Name: "Beaches", Prompts: []string{"sand and sea"}}
	pinned := VocabLabel{Name: "Dogs", Prompts: []string{"a barking dog"}}
	syncOne(t, st, changed, pinned, label("Birds"))

	if got := autoTag(t, st, beaches); got.Name != "Seaside" || !slices.Equal(got.Prompts, changed.Prompts) || got.Model != "" {
		t.Fatalf("renamed built-in after a prompt change = %+v, want its name and the new prompts to be learned", got)
	}
	if got := autoTag(t, st, dogs); got.Name != "Dogs" || !slices.Equal(got.Prompts, pinned.Prompts) {
		t.Fatalf("pinned built-in after a prompt change = %+v", got)
	}
	// Retired, but renamed and pinned: it keeps its prompts and its matches.
	if got := autoTag(t, st, cats); got.Name != "Kittens" || got.VocabName != "Cats" || !slices.Equal(got.Prompts, []string{"a photo of Cats"}) {
		t.Fatalf("renamed, pinned and retired built-in = %+v", got)
	}
	if _, _, err := st.TagByName(ctx, "Cars"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a built-in nobody touched survived its retirement: %v", err)
	}
	for _, tag := range []int64{beaches, dogs, cats} {
		var matches int
		check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM asset_tags WHERE tag_id = ? AND source = 'ai'", tag).Scan(&matches))
		if matches != 1 {
			t.Errorf("tag %d kept %d AI matches, want 1", tag, matches)
		}
	}

	// Retired and only renamed, then reset: nothing keeps it, so it goes.
	syncOne(t, st, changed, pinned, label("Birds"))
	check(t, st.RenameTag(ctx, cats, ""))
	check(t, st.SetTagPinned(ctx, cats, false))
	syncOne(t, st, changed, pinned, label("Birds"))
	if _, _, err := st.TagByName(ctx, "Cats"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a retired built-in the user let go of stayed: %v", err)
	}
}

func TestVocabularyKeepsARetiredBuiltinThatIsOnlyRenamedOrOnlyPinned(t *testing.T) {
	for _, c := range []struct {
		name  string
		keep  func(*Store, int64) error
		other string
	}{
		{"renamed", func(st *Store, id int64) error { return st.RenameTag(context.Background(), id, "Mine") }, "Mine"},
		{"pinned", func(st *Store, id int64) error { return st.SetTagPinned(context.Background(), id, true) }, "Old"},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, ctx := openTest(t), context.Background()
			syncOne(t, st, label("Old"))
			id, _, err := st.TagByName(ctx, "Old")
			check(t, err)
			check(t, c.keep(st, id))
			syncOne(t, st, label("New"))
			if got := autoTag(t, st, id); got.Name != c.other || got.VocabName != "Old" {
				t.Fatalf("retired built-in = %+v", got)
			}
		})
	}
}

func TestANewBuiltinWhoseNameTheUserTookIsAddedBesideIt(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	syncOne(t, st, label("Dogs"))
	dogs, _, err := st.TagByName(ctx, "Dogs")
	check(t, err)
	check(t, st.RenameTag(ctx, dogs, "Puppies"))
	check(t, st.SetTagPinned(ctx, dogs, true))

	syncOne(t, st, label("Dogs"), label("Puppies"))
	own := thingNamed(t, st, "Puppies")
	if own.ID != dogs || own.Original != "Dogs" || !own.Pinned {
		t.Fatalf("the user's Thing changed: %+v", own)
	}
	added := thingNamed(t, st, "Puppies (built-in)")
	if added.ID == dogs || added.Original != "Puppies" || added.Kind != core.TagBuiltin {
		t.Fatalf("the new built-in = %+v", added)
	}
	if got := autoTag(t, st, added.ID); got.VocabName != "Puppies" || !slices.Equal(got.Prompts, []string{"a photo of Puppies"}) {
		t.Fatalf("the new built-in as the AI sees it = %+v", got)
	}
	// It is found by its vocabulary name from now on, whatever its name.
	syncOne(t, st, label("Dogs"), VocabLabel{Name: "Puppies", Prompts: []string{"young dogs"}})
	if got := autoTag(t, st, added.ID); !slices.Equal(got.Prompts, []string{"young dogs"}) {
		t.Fatalf("the vocabulary update missed the built-in under its second name: %+v", got)
	}
	if got := thingNames(t, st); len(got) != 2 {
		t.Fatalf("Things = %v, want the two", got)
	}
}

func TestANewBuiltinTakesOverAThingTheUserMadeOfTheSameName(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	id, err := st.CreateTag(ctx, "Cats", core.TagManual, nil)
	check(t, err)
	syncOne(t, st, label("Cats"))
	if got := autoTag(t, st, id); got.Name != "Cats" || got.VocabName != "Cats" || !got.Builtin {
		t.Fatalf("a Thing the user made, named like a new built-in = %+v", got)
	}
}

func TestVocabularySpellingChangeOfOnlyCaseKeepsTheBuiltin(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	syncOne(t, st, label("Wild Cats"))
	id, _, err := st.TagByName(ctx, "wild cats")
	check(t, err)
	syncOne(t, st, label("Wild cats"))
	if got := autoTag(t, st, id); got.Name != "Wild cats" || got.VocabName != "Wild cats" {
		t.Fatalf("built-in after a respelling = %+v", got)
	}
	if names := thingNames(t, st); len(names) != 1 {
		t.Fatalf("Things = %v, want the one", names)
	}
}

func TestPinnedThingsSortFirstNewestPinFirstAndSurviveReopening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	ctx := context.Background()
	st, err := Open(path)
	check(t, err)
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	syncOne(t, st, label("Beaches"), label("Dogs"), label("Cats"), label("Birds"))
	ids := map[string]int64{}
	for _, name := range []string{"Beaches", "Dogs", "Cats", "Birds"} {
		id, _, err := st.TagByName(ctx, name)
		check(t, err)
		ids[name] = id
	}
	// The most used first when nothing is pinned.
	check(t, st.SetTagScores(ctx, ids["Cats"], []AssetScore{{AssetID: 1, Score: 0.9}}))
	if got := thingNames(t, st); got[0] != "Cats" {
		t.Fatalf("unpinned order = %v", got)
	}
	// Pinned within the same second still order by when they were pinned.
	check(t, st.SetTagPinned(ctx, ids["Dogs"], true))
	check(t, st.SetTagPinned(ctx, ids["Birds"], true))
	check(t, st.SetTagPinned(ctx, ids["Dogs"], true)) // already pinned: keeps its place
	want := []string{"Birds", "Dogs", "Cats", "Beaches"}
	if got := thingNames(t, st); !slices.Equal(got, want) {
		t.Fatalf("pinned order = %v, want %v", got, want)
	}
	check(t, st.Close())

	st, err = Open(path)
	check(t, err)
	defer st.Close()
	if got := thingNames(t, st); !slices.Equal(got, want) {
		t.Fatalf("order after reopening = %v, want %v", got, want)
	}
	if !thingNamed(t, st, "Dogs").Pinned || thingNamed(t, st, "Cats").Pinned {
		t.Fatal("pinned flags did not survive reopening")
	}
	check(t, st.SetTagPinned(ctx, ids["Birds"], false))
	if got := thingNames(t, st); !slices.Equal(got, []string{"Dogs", "Cats", "Beaches", "Birds"}) {
		t.Fatalf("order after unpinning = %v", got)
	}
	if err := st.SetTagPinned(ctx, 999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pinning a Thing that is gone: %v", err)
	}
}

// A v18 library holds event names per photo, and built-in Things without
// vocabulary names: the upgrade gives both their new shape.
func TestUpgradeFromV18KeepsEventNamesAndBuiltinIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	ctx := context.Background()
	st, err := Open(path)
	check(t, err)
	f := newEventFixture(t, st)
	trip := []int64{f.add(0), f.add(1), f.add(2)}
	lunch := []int64{f.add(40), f.add(41)}
	other := f.add(100)
	syncOne(t, st, label("Beaches"))
	manual, err := st.CreateTag(ctx, "Holiday", core.TagManual, nil)
	check(t, err)
	check(t, st.Close())

	// Rebuild the library as v18 left it, names in event_names on every photo
	// of each named event, and the same name on two events.
	st, err = Open(path)
	check(t, err)
	downgradeToV18(t, st)
	for _, id := range trip {
		check(t, errOf(st.w.ExecContext(ctx, "INSERT INTO event_names (anchor_asset_id, name) VALUES (?, 'Beach trip')", id)))
	}
	for _, id := range append(slices.Clone(lunch), other) {
		check(t, errOf(st.w.ExecContext(ctx, "INSERT INTO event_names (anchor_asset_id, name) VALUES (?, 'Lunch')", id)))
	}
	check(t, st.Close())

	st, err = Open(path)
	check(t, err)
	defer st.Close()
	var version int
	check(t, st.r.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version))
	if want := latestMigration(t); version != want {
		t.Fatalf("user_version = %d after the upgrade, want %d", version, want)
	}
	var tables int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name = 'event_names'").Scan(&tables))
	if tables != 0 {
		t.Fatal("event_names survived the upgrade")
	}
	var distinct, members int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*), (SELECT count(*) FROM event_members) FROM event_tags WHERE kind = 'name'").Scan(&distinct, &members))
	if distinct != 2 || members != 6 {
		t.Fatalf("%d name tags with %d members, want 2 with 6", distinct, members)
	}
	events, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	names := map[int64]string{}
	for _, e := range events {
		names[e.ID] = e.Name
	}
	for id, want := range map[int64]string{trip[0]: "Beach trip", lunch[0]: "Lunch", other: "Lunch"} {
		if names[id] != want {
			t.Errorf("event %d is named %q, want %q (events: %+v)", id, names[id], want, events)
		}
	}
	// The earliest named photo's name still wins where a run holds two.
	var first string
	check(t, st.r.QueryRowContext(ctx, "SELECT name FROM event_tags ORDER BY id LIMIT 1").Scan(&first))
	if first != "Beach trip" {
		t.Fatalf("lowest name tag is %q, want the earliest photo's name", first)
	}

	beaches := thingNamed(t, st, "Beaches")
	if got := autoTag(t, st, beaches.ID); got.VocabName != "Beaches" {
		t.Fatalf("built-in after the upgrade = %+v, want its vocabulary name", got)
	}
	var vocab *string
	check(t, st.r.QueryRowContext(ctx, "SELECT vocab_name FROM tags WHERE id = ?", manual).Scan(&vocab))
	if vocab != nil {
		t.Fatalf("a Thing the user made has the vocabulary name %q", *vocab)
	}
}

func TestANewBuiltinNamedLikeAUserLabelLeavesTheLabelAndItsPromptsAlone(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	id, err := st.CreateTag(ctx, "Cats", core.TagLabel, []string{"my own cat"})
	check(t, err)
	syncOne(t, st, label("Cats"))
	if got := autoTag(t, st, id); got.Builtin || got.VocabName != "" || !slices.Equal(got.Prompts, []string{"my own cat"}) {
		t.Fatalf("the user's label = %+v, want it untouched", got)
	}
	added := thingNamed(t, st, "Cats (built-in)")
	if added.ID == id || added.Kind != core.TagBuiltin {
		t.Fatalf("the new built-in = %+v", added)
	}
}

func TestVocabularyRespellingOfARenamedBuiltinUpdatesItInPlace(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	syncOne(t, st, label("Wild Cats"))
	id, _, err := st.TagByName(ctx, "Wild Cats")
	check(t, err)
	check(t, st.RenameTag(ctx, id, "Kitties"))
	syncOne(t, st, VocabLabel{Name: "Wild cats", Prompts: []string{"a lynx"}})
	got := autoTag(t, st, id)
	if got.Name != "Kitties" || got.VocabName != "Wild cats" || !slices.Equal(got.Prompts, []string{"a lynx"}) {
		t.Fatalf("renamed built-in after a respelling = %+v", got)
	}
	if names := thingNames(t, st); !slices.Equal(names, []string{"Kitties"}) {
		t.Fatalf("Things = %v, want the one", names)
	}
}

// Where a run holds two names, it shows the one on its earliest named photo,
// as it did before event tags, whatever the tags' ids say.
func TestAnEventShowsTheNameOfItsEarliestNamedPhotoAfterTheUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	ctx := context.Background()
	st, err := Open(path)
	check(t, err)
	f := newEventFixture(t, st)
	first := f.add(0)
	earlier, later := f.add(48), f.add(49)
	check(t, st.Close())
	st, err = Open(path)
	check(t, err)
	downgradeToV18(t, st)
	for id, name := range map[int64]string{first: "Repeated", earlier: "Earlier", later: "Repeated"} {
		check(t, errOf(st.w.ExecContext(ctx, "INSERT INTO event_names (anchor_asset_id, name) VALUES (?, ?)", id, name)))
	}
	check(t, st.Close())
	st, err = Open(path)
	check(t, err)
	defer st.Close()
	events, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(events) != 2 || events[0].Name != "Earlier" || events[1].Name != "Repeated" {
		t.Fatalf("events = %+v, want the day 3 event to show Earlier", events)
	}
}

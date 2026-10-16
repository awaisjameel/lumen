package store

import (
	"context"
	"database/sql"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"lumen/internal/core"
)

func TestDuplicateRevisionUpgradesAnExistingLibrary(t *testing.T) {
	ctx, path := t.Context(), filepath.Join(t.TempDir(), "library.db")
	legacy, err := sql.Open(driverName, path+"?_foreign_keys=1")
	check(t, err)
	t.Cleanup(func() { _ = legacy.Close() })
	names, err := migrationFS.ReadDir("migrations")
	check(t, err)
	for _, name := range names {
		if name.Name() >= "0033" {
			break
		}
		body, err := migrationFS.ReadFile("migrations/" + name.Name())
		check(t, err)
		check(t, errOf(legacy.ExecContext(ctx, string(body))))
	}
	check(t, errOf(legacy.ExecContext(ctx, `INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0);
		INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, phash, width, height, thumb_version)
		VALUES (1, 'a', 'photo', '.jpg', 1, 0, 7, 1920, 1080, 1), (2, 'b', 'photo', '.jpg', 2, 0, 7, 1920, 1080, 1);
		INSERT INTO files(asset_id, source_id, path, size, mtime)
		VALUES (1, 1, 'C:\Photos\a.jpg', 1, 0), (2, 1, 'C:\Photos\b.jpg', 1, 0);
		PRAGMA user_version = 32`)))
	check(t, legacy.Close())
	st, err := Open(path)
	check(t, err)
	t.Cleanup(func() { check(t, st.Close()) })
	groups, err := st.DuplicateGroups(ctx)
	check(t, err)
	if len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf("upgrade changed existing photos: %+v", groups)
	}
	before := st.duplicatesCache.revision
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE assets SET phash = NULL WHERE id = 2`)
		return err
	}))
	groups, err = st.DuplicateGroups(ctx)
	check(t, err)
	if len(groups) != 0 || st.duplicatesCache.revision == before {
		t.Fatalf("hash write did not invalidate the upgraded library: %+v", groups)
	}
}

func duplicateCacheFixture(t *testing.T) *Store {
	t.Helper()
	st := openTest(t)
	ctx := t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{
		item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\copy.jpg`, "a"),
		item(src.ID, `C:\Photos\b.jpg`, "b"), item(src.ID, `C:\Photos\c.jpg`, "c"),
	})))
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE assets SET phash = CASE id WHEN 3 THEN 9223372036854775807 ELSE 7 END,
			thumb_version = 1, width = 1920, height = 1080, taken_at = id`)
		return err
	}))
	return st
}

func freshDuplicateGroups(t *testing.T, st *Store) []core.DuplicateGroup {
	t.Helper()
	var groups []core.DuplicateGroup
	check(t, st.read(t.Context(), func(tx *sql.Tx) error {
		var err error
		groups, err = duplicateGroups(t.Context(), tx)
		return err
	}))
	return groups
}

func TestDuplicateCacheInvalidatesEveryAnswerInput(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"hash", `UPDATE assets SET phash = NULL WHERE id = 2`},
		{"width", `UPDATE assets SET width = 1080 WHERE id = 2`},
		{"height", `UPDATE assets SET height = 1920 WHERE id = 2`},
		{"thumbnail", `UPDATE assets SET thumb_version = 0 WHERE id = 2`},
		{"kind", `UPDATE assets SET kind = 'video' WHERE id = 2`},
		{"extension", `UPDATE assets SET ext = '.arw' WHERE id = 2`},
		{"camera", `UPDATE assets SET camera = 'Camera' WHERE id = 2`},
		{"EXIF date", `UPDATE assets SET date_from = 'exif' WHERE id = 2`},
		{"retained EXIF date", `UPDATE assets SET original_date_from = 'exif' WHERE id = 2`},
		{"capture date", `UPDATE assets SET taken_at = 50 WHERE id = 2`},
		{"latitude", `UPDATE assets SET lat = 1 WHERE id = 2`},
		{"longitude", `UPDATE assets SET lon = 1 WHERE id = 2`},
		{"added date", `UPDATE assets SET added_at = 50 WHERE id = 2`},
		{"hidden", `UPDATE assets SET hidden = 1 WHERE id = 2`},
		{"trashed", `UPDATE assets SET trashed_at = 50 WHERE id = 2`},
		{"missing", `UPDATE assets SET missing_since = 50 WHERE id = 2`},
		{"file path", `UPDATE files SET path = 'C:\Photos\renamed.jpg' WHERE id = 1`},
		{"file size", `UPDATE files SET size = 50 WHERE id = 1`},
		{"file time", `UPDATE files SET mtime = 50000000000 WHERE id = 1`},
		{"file assignment", `UPDATE files SET asset_id = 2 WHERE id = 2`},
		{"file insertion", `INSERT INTO files(asset_id, source_id, path, size, mtime) VALUES (2, 1, 'C:\Photos\new.jpg', 9, 1000000000)`},
		{"file deletion", `DELETE FROM files WHERE id = 2`},
		{"asset insertion", `INSERT INTO assets(content_hash, kind, ext, taken_at, added_at, phash, width, height, thumb_version)
			VALUES ('new', 'photo', '.jpg', 8, 0, 7, 1920, 1080, 1)`},
		{"asset deletion", `DELETE FROM assets WHERE id = 2`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := duplicateCacheFixture(t)
			check(t, errOf(st.DuplicateGroups(t.Context())))
			before := st.duplicatesCache.revision
			check(t, st.tx(t.Context(), func(tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(), tc.query)
				return err
			}))
			groups, err := st.DuplicateGroups(t.Context())
			check(t, err)
			if st.duplicatesCache.revision == before {
				t.Fatal("the answer input did not invalidate the cached revision")
			}
			if want := freshDuplicateGroups(t, st); !reflect.DeepEqual(groups, want) {
				t.Fatalf("cached = %+v, fresh = %+v", groups, want)
			}
		})
	}
}

func TestDuplicateCacheReusesAnUnchangedAnswer(t *testing.T) {
	st := duplicateCacheFixture(t)
	check(t, errOf(st.DuplicateGroups(t.Context())))
	if !st.duplicatesCache.loaded {
		t.Fatal("the first answer was not cached")
	}
	before := &st.duplicatesCache.groups[0]
	check(t, st.tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE assets SET phash = phash, width = width;
			UPDATE files SET path = path, size = size, mtime = mtime`)
		return err
	}))
	check(t, errOf(st.DuplicateGroups(t.Context())))
	if &st.duplicatesCache.groups[0] != before {
		t.Fatal("an unchanged answer was rebuilt")
	}
}

func TestDuplicateCacheDoesNotExposeItsSlicesToCallers(t *testing.T) {
	st := duplicateCacheFixture(t)
	want := freshDuplicateGroups(t, st)
	groups, err := st.DuplicateGroups(t.Context())
	check(t, err)
	groups[0].Items[0].Paths[0] = "changed"
	groups[0].Items[0].Mtimes[0] = -1
	groups[0].Items[0].ID = -1
	groups[0].Kind = core.DuplicateSimilar
	groups[1].Items = nil
	got, err := st.DuplicateGroups(t.Context())
	check(t, err)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("caller changed the cached answer: %+v", got)
	}
	// Exact and similar groups can share an asset, but not mutable path slices.
	got[0].Items[0].Paths[0] = "changed"
	for _, it := range got[1].Items {
		if it.ID == got[0].Items[0].ID && it.Paths[0] == "changed" {
			t.Fatal("two returned groups share path storage")
		}
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 10 {
				answer, err := st.DuplicateGroups(t.Context())
				if err != nil {
					t.Error(err)
					return
				}
				if !reflect.DeepEqual(answer, want) {
					t.Error("concurrent caller received a changed answer")
				}
				answer[0].Items[0].Paths[0] = "changed"
			}
		})
	}
	workers.Wait()
}

func TestDuplicateCacheFollowsEveryBlockCoverageInput(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE assets SET phash = 7, width = 1920, height = 1080, thumb_version = 1`)
		return err
	}))
	r := rand.New(rand.NewPCG(83, 97))
	changes := blockedOracleChanges()
	for round := range 3 {
		for _, change := range changes {
			check(t, errOf(st.DuplicateGroups(ctx)))
			check(t, st.tx(ctx, func(tx *sql.Tx) error {
				var args []any
				if change.args != nil {
					args = change.args(r)
				}
				_, err := tx.ExecContext(ctx, change.sql, args...)
				return err
			}))
			groups, err := st.DuplicateGroups(ctx)
			check(t, err)
			if !reflect.DeepEqual(groups, freshDuplicateGroups(t, st)) {
				t.Fatalf("round %d after %s returned stale groups", round, change.name)
			}
		}
	}
}

func TestDuplicateCacheReadsTheRevisionAfterWaitingForABuild(t *testing.T) {
	st := duplicateCacheFixture(t)
	check(t, errOf(st.DuplicateGroups(t.Context())))
	st.duplicatesCache.mu.Lock()
	started := make(chan struct{})
	finished := make(chan []core.DuplicateGroup, 1)
	go func() {
		close(started)
		groups, err := st.DuplicateGroups(t.Context())
		if err != nil {
			t.Error(err)
		}
		finished <- groups
	}()
	<-started
	err := st.tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE assets SET hidden = 1 WHERE id = 2`)
		return err
	})
	st.duplicatesCache.mu.Unlock()
	check(t, err)
	if got, want := <-finished, freshDuplicateGroups(t, st); !reflect.DeepEqual(got, want) {
		t.Fatalf("waiting caller got a stale answer: %+v", got)
	}
}

func TestDuplicateCacheFollowsFolderAndPhotoBlocks(t *testing.T) {
	for _, kind := range []core.BlockKind{core.BlockAsset, core.BlockFolder} {
		t.Run(string(kind), func(t *testing.T) {
			st := duplicateCacheFixture(t)
			check(t, errOf(st.DuplicateGroups(t.Context())))
			path, id := "", int64(2)
			if kind == core.BlockFolder {
				path, id = `C:\Photos`, 0
			}
			block, err := st.SetBlock(t.Context(), kind, id, path, core.BlockPhotos)
			check(t, err)
			groups, err := st.DuplicateGroups(t.Context())
			check(t, err)
			if !reflect.DeepEqual(groups, freshDuplicateGroups(t, st)) {
				t.Fatal("blocking did not refresh the answer")
			}
			check(t, st.RemoveBlock(t.Context(), block))
			groups, err = st.DuplicateGroups(t.Context())
			check(t, err)
			if !reflect.DeepEqual(groups, freshDuplicateGroups(t, st)) {
				t.Fatal("unblocking did not refresh the answer")
			}
		})
	}
}

func TestDuplicateGroupingCollapsesThousandsOfIdenticalHashes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	photos := identicalDuplicatePhotos(6000)
	for i := range photos {
		photos[i].Width, photos[i].Height = 10000+i, 10000
	}
	groups, err := groupCandidates(ctx, photos)
	check(t, err)
	if len(groups) != 1 || len(groups[0].Items) != len(photos) {
		t.Fatalf("got %d groups, want one with %d photos", len(groups), len(photos))
	}
}

func TestDuplicateGroupingRetainsEveryShapeOfAnIdenticalHash(t *testing.T) {
	photos := identicalDuplicatePhotos(4)
	photos[0].Width, photos[0].Height = 200, 100
	photos[1].Width, photos[1].Height = 100, 100
	photos[2].Width, photos[2].Height, photos[2].hash = 200, 100, 3
	photos[3].Width, photos[3].Height, photos[3].Ext = 200, 100, ".arw"
	groups, err := groupCandidates(t.Context(), photos)
	check(t, err)
	if len(groups) != 1 || len(groups[0].Items) != 2 || groups[0].Items[0].ID != 1 || groups[0].Items[1].ID != 3 {
		t.Fatalf("got %+v, want the two wide JPEG photos", groups)
	}
}

func TestDuplicateGroupingPreservesThePreviousAlgorithmOnRandomisedPhotos(t *testing.T) {
	r := rand.New(rand.NewPCG(17, 29))
	for round := range 20 {
		photos := randomPhotos(500)
		for i := range photos {
			photos[i].Width, photos[i].Height = 95+r.IntN(30), 100
			photos[i].TakenAt = r.Int64N(10)
			photos[i].copies = 1 + r.IntN(3)
			if i%7 == 0 {
				photos[i].Ext = ".arw"
			}
			if i%3 == 0 {
				photos[i].hash = uint64(r.IntN(8))
			}
		}
		groups, err := groupCandidates(t.Context(), photos)
		check(t, err)
		want, err := referenceGroupDuplicates(t.Context(), photos)
		check(t, err)
		if !reflect.DeepEqual(groups, want) {
			t.Fatalf("round %d changed the previous groups", round)
		}
	}
}

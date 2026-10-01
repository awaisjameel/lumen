package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/core"
)

func TestDamagedPINEncodingReturnsTheInvalidPINSentinel(t *testing.T) {
	for _, key := range []string{"block_pin_salt", "block_pin_hash"} {
		t.Run(key, func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			for _, setting := range []string{"block_pin_salt", "block_pin_hash"} {
				check(t, errOf(st.w.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES (?, ?)", setting, strings.Repeat("00", 32))))
			}
			for _, damaged := range []string{"not-hex", "0"} {
				check(t, errOf(st.w.ExecContext(ctx, "UPDATE settings SET value = ? WHERE key = ?", damaged, key)))
				valid, err := st.VerifyBlockPIN(ctx, "123456")
				if valid || !errors.Is(err, ErrInvalidPIN) {
					t.Fatalf("damaged %s = %q: valid %v, error %v; want invalid PIN", key, damaged, valid, err)
				}
			}
		})
	}
}

func TestVocabularyUpdatesPreserveBlockedThingsAndTheirMatches(t *testing.T) {
	for _, mode := range []core.BlockMode{core.BlockLabel, core.BlockPhotos} {
		t.Run(string(mode), func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			src, err := st.AddSource(ctx, `C:\Photos`)
			check(t, err)
			check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\private.jpg`, "private")})))
			check(t, st.SyncVocabulary(ctx, []VocabLabel{{Name: "Private", Prompts: []string{"private"}}}))
			tag, _, err := st.TagByName(ctx, "Private")
			check(t, err)
			check(t, st.SetTagScores(ctx, tag, []AssetScore{{AssetID: 1, Score: 1}}))
			block, err := st.SetBlock(ctx, core.BlockTag, tag, "", mode)
			check(t, err)
			before, err := st.CountVisible(ctx)
			check(t, err)
			labels := []VocabLabel{{Name: "Public", Prompts: []string{"public"}}}
			check(t, st.SyncVocabulary(ctx, labels))
			blocks, err := st.Blocks(ctx)
			check(t, err)
			if len(blocks) != 1 || blocks[0].ID != block || blocks[0].TargetID != tag || blocks[0].Mode != mode {
				t.Fatalf("vocabulary update lost the user's block: %+v", blocks)
			}
			after, err := st.CountVisible(ctx)
			check(t, err)
			if after != before {
				t.Fatalf("vocabulary update changed blocked visibility: %d -> %d", before, after)
			}
			auto, err := st.AutoTags(ctx)
			check(t, err)
			if len(auto) != 2 || auto[0].ID != tag || len(auto[0].Prompts) != 1 || auto[0].Prompts[0] != "private" {
				t.Fatalf("blocked Thing no longer matches future photos: %+v", auto)
			}
			check(t, st.RemoveBlock(ctx, block))
			check(t, st.SyncVocabulary(ctx, labels))
			if _, _, err := st.TagByName(ctx, "Private"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("unblocked obsolete Thing was not retired: %v", err)
			}
		})
	}
}

func TestPersonBlockModesAndOverlappingAssetBlock(t *testing.T) {
	st, ctx, a, b, _ := peopleFixture(t)
	faceID := facesOf(t, st, a)[0].ID
	personID, err := st.NameFaces(ctx, []int64{faceID}, "Ali")
	check(t, err)
	personBlock, err := st.SetBlock(ctx, core.BlockPerson, personID, "", core.BlockLabel)
	check(t, err)
	if n, _ := st.CountVisible(ctx); n != 3 {
		t.Fatalf("label-only block hid %d assets", 3-n)
	}
	if named, _ := st.NamedPeople(ctx); len(named) != 0 {
		t.Fatalf("blocked name in search: %+v", named)
	}
	if _, err := st.PersonByName(ctx, "Ali"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blocked person offered for merge: %v", err)
	}
	if _, err := st.NameFaces(ctx, []int64{facesOf(t, st, b)[0].ID}, "Ali"); !errors.Is(err, ErrBlockedName) {
		t.Fatalf("blocked person accepted in face naming: %v", err)
	}
	if faces := facesOf(t, st, a); len(faces) != 1 || faces[0].Person != 0 {
		// a's other face remains; the blocked person's box is absent.
		t.Fatalf("blocked face in details: %+v", faces)
	}
	_, err = st.SetBlock(ctx, core.BlockPerson, personID, "", core.BlockPhotos)
	check(t, err)
	if n, _ := st.CountVisible(ctx); n != 2 {
		t.Fatalf("photo block count = %d, want 2", n)
	}
	if ok, _ := st.AssetVisible(ctx, a); ok {
		t.Fatal("person's photo remains visible")
	}
	if _, err := st.AssetFile(ctx, a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blocked media file: %v", err)
	}
	assetBlock, err := st.SetBlock(ctx, core.BlockAsset, a, "", core.BlockLabel)
	check(t, err)
	check(t, st.RemoveBlock(ctx, personBlock))
	if ok, _ := st.AssetVisible(ctx, a); ok {
		t.Fatal("overlapping asset block lost after person unblock")
	}
	check(t, st.RemoveBlock(ctx, assetBlock))
	if n, _ := st.CountVisible(ctx); n != 3 {
		t.Fatalf("unblocked count = %d, want 3", n)
	}
	if ok, _ := st.AssetVisible(ctx, b); !ok {
		t.Fatal("unrelated asset hidden")
	}
}

func TestFolderAndThingBlocksFollowNewMedia(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{
		item(src.ID, `C:\Photos\private\one.jpg`, "one"),
		item(src.ID, `C:\Photos\public\two.jpg`, "two"),
	})))
	id, err := st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\private`, core.BlockPhotos)
	check(t, err)
	if n, _ := st.CountVisible(ctx); n != 1 {
		t.Fatalf("folder block count = %d, want 1", n)
	}
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\private\three.jpg`, "three")})))
	if n, _ := st.CountVisible(ctx); n != 1 {
		t.Fatalf("new file escaped folder block: count %d", n)
	}
	check(t, st.RemoveBlock(ctx, id))
	if n, _ := st.CountVisible(ctx); n != 3 {
		t.Fatalf("unblocked folder count = %d", n)
	}
	var asset int64
	check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", `C:\Photos\public\two.jpg`).Scan(&asset))
	tag, err := st.CreateTag(ctx, "Private", core.TagManual, nil)
	check(t, err)
	check(t, st.TagAsset(ctx, asset, tag))
	_, err = st.SetBlock(ctx, core.BlockTag, tag, "", core.BlockPhotos)
	check(t, err)
	if ok, _ := st.AssetVisible(ctx, asset); ok {
		t.Fatal("tagged asset escaped Thing block")
	}
	if tags, err := st.SearchTags(ctx, "Private", 5); err != nil || len(tags) != 0 {
		t.Fatalf("blocked Thing suggested: %+v, %v", tags, err)
	}
}

func TestPersonBlockFollowsMerge(t *testing.T) {
	st, ctx, a, b, _ := peopleFixture(t)
	from, err := st.NameFaces(ctx, []int64{facesOf(t, st, a)[0].ID}, "Ali")
	check(t, err)
	into, err := st.NameFaces(ctx, []int64{facesOf(t, st, b)[0].ID}, "Sara")
	check(t, err)
	_, err = st.SetBlock(ctx, core.BlockPerson, from, "", core.BlockPhotos)
	check(t, err)
	check(t, st.MergePeople(ctx, from, into))
	if blocked, err := st.IsBlocked(ctx, core.BlockPerson, into); err != nil || !blocked {
		t.Fatalf("merged identity block = %v, %v", blocked, err)
	}
	for _, id := range []int64{a, b} {
		if ok, err := st.AssetVisible(ctx, id); err != nil || ok {
			t.Fatalf("merged photo %d visible: %v, %v", id, ok, err)
		}
	}
}

func TestFolderBlockBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, source, folder, inside, outside string
	}{
		{"drive root", `C:\`, `C:\`, `C:\photo.jpg`, `D:\photo.jpg`},
		{"nested folder", `C:\Photos`, `C:\Photos\private`, `c:\photos\PRIVATE\one.jpg`, `C:\Photos\private-copy\two.jpg`},
		{"UNC share", `\\server\photos`, `\\server\photos`, `\\SERVER\PHOTOS\one.jpg`, `\\server\photos-copy\two.jpg`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			src, err := st.AddSource(ctx, tc.source)
			check(t, err)
			check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, tc.inside, "inside"), item(src.ID, tc.outside, "outside")})))
			_, err = st.SetBlock(ctx, core.BlockFolder, 0, tc.folder, core.BlockPhotos)
			check(t, err)
			if n, err := st.CountVisible(ctx); err != nil || n != 1 {
				t.Fatalf("visible count = %d, %v; want only the outside file", n, err)
			}
			var id int64
			check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", tc.inside).Scan(&id))
			if ok, err := st.AssetVisible(ctx, id); err != nil || ok {
				t.Fatalf("descendant visible = %v, %v", ok, err)
			}
		})
	}
}

func TestBlockPIN(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	if configured, err := st.HasBlockPIN(ctx); err != nil || configured {
		t.Fatalf("initial PIN state: %v, %v", configured, err)
	}
	if err := st.CreateBlockPIN(ctx, "1234"); !errors.Is(err, ErrInvalidPIN) {
		t.Fatalf("short PIN: %v", err)
	}
	check(t, st.CreateBlockPIN(ctx, "123456"))
	if err := st.CreateBlockPIN(ctx, "654321"); !errors.Is(err, ErrPINExists) {
		t.Fatalf("replaced PIN: %v", err)
	}
	if ok, err := st.VerifyBlockPIN(ctx, "000000"); err != nil || ok {
		t.Fatalf("wrong PIN: %v, %v", ok, err)
	}
	if ok, err := st.VerifyBlockPIN(ctx, "123456"); err != nil || !ok {
		t.Fatalf("correct PIN: %v, %v", ok, err)
	}
	for _, pin := range []string{"", "12345", "1234567890123", "12345a"} {
		if ok, err := st.VerifyBlockPIN(ctx, pin); err != nil || ok {
			t.Fatalf("invalid PIN %q: %v, %v", pin, ok, err)
		}
	}
	for _, key := range []string{"block_pin_salt", "block_pin_hash"} {
		var original string
		check(t, st.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&original))
		_, err := st.w.ExecContext(ctx, "UPDATE settings SET value = '00' WHERE key = ?", key)
		check(t, err)
		if ok, err := st.VerifyBlockPIN(ctx, "123456"); ok || !errors.Is(err, ErrInvalidPIN) {
			t.Fatalf("damaged %s: %v, %v", key, ok, err)
		}
		_, err = st.w.ExecContext(ctx, "UPDATE settings SET value = ? WHERE key = ?", original, key)
		check(t, err)
	}
}

func TestUpgradeConvertsUserHiddenLabelsButNotSink(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := sql.Open(driverName, path+"?_foreign_keys=1")
	check(t, err)
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	check(t, err)
	for version, name := range names[:13] {
		body, err := migrationFS.ReadFile(name)
		check(t, err)
		tx, err := db.BeginTx(ctx, nil)
		check(t, err)
		_, err = tx.ExecContext(ctx, string(body))
		check(t, err)
		_, err = tx.ExecContext(ctx, "PRAGMA user_version = "+fmt.Sprint(version+1))
		check(t, err)
		check(t, tx.Commit())
	}
	_, err = db.ExecContext(ctx, `INSERT INTO people(id, name, hidden, created_at) VALUES (1, 'Ali', 1, 1);
		INSERT INTO tags(id, name, kind, hidden, created_at) VALUES
			(1, 'Trip', 'manual', 1, 1), (2, 'Other objects', 'builtin', 1, 1);`)
	check(t, err)
	check(t, db.Close())
	st, err := Open(path)
	check(t, err)
	defer st.Close()
	blocks, err := st.Blocks(ctx)
	check(t, err)
	if len(blocks) != 2 || blocks[0].Mode != core.BlockPhotos || blocks[1].Mode != core.BlockPhotos {
		t.Fatalf("converted blocks = %+v, want person and manual Thing", blocks)
	}
	if blocked, err := st.IsBlocked(ctx, core.BlockTag, 2); err != nil || blocked {
		t.Fatalf("sink converted to block: %v, %v", blocked, err)
	}
}

func TestSmartAlbumBlockTracksRuleAndNewMatches(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})))
	var a, b int64
	check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", `C:\Photos\a.jpg`).Scan(&a))
	check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", `C:\Photos\b.jpg`).Scan(&b))
	tag, err := st.CreateTag(ctx, "Trips", core.TagManual, nil)
	check(t, err)
	check(t, st.TagAsset(ctx, a, tag))
	album, err := st.CreateAlbum(ctx, "Trips", core.AlbumSmart, core.AlbumRule{Tag: tag}, nil)
	check(t, err)
	blockID, err := st.SetBlock(ctx, core.BlockAlbum, album, "", core.BlockPhotos)
	check(t, err)
	if ok, _ := st.AssetVisible(ctx, a); ok {
		t.Fatal("smart album member remains visible")
	}
	if ok, _ := st.AssetVisible(ctx, b); !ok {
		t.Fatal("nonmember hidden")
	}
	var before, after int64
	check(t, st.r.QueryRowContext(ctx, "SELECT value FROM event_revision WHERE id = 1").Scan(&before))
	check(t, st.TagAsset(ctx, b, tag))
	check(t, st.r.QueryRowContext(ctx, "SELECT value FROM event_revision WHERE id = 1").Scan(&after))
	if after <= before {
		t.Fatal("smart album membership did not invalidate visibility caches")
	}
	if ok, _ := st.AssetVisible(ctx, b); ok {
		t.Fatal("new smart album member escaped block")
	}
	check(t, st.RemoveBlock(ctx, blockID))
	if n, _ := st.CountVisible(ctx); n != 2 {
		t.Fatalf("unblocked smart album count = %d", n)
	}
}

func TestPlaceLabelBlockKeepsPhotoButRemovesPlaceDetail(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	_, err = st.w.ExecContext(ctx, `INSERT INTO places(id, name, region, country, lat, lon)
		VALUES (1, 'Lahore', 'Punjab', 'Pakistan', 31.5, 74.3)`)
	check(t, err)
	_, err = st.w.ExecContext(ctx, "UPDATE assets SET place_id = 1")
	check(t, err)
	blockID, err := st.SetBlock(ctx, core.BlockPlace, 1, "", core.BlockLabel)
	check(t, err)
	if n, _ := st.CountVisible(ctx); n != 1 {
		t.Fatalf("label-only place block hid photo: %d", n)
	}
	detail, err := st.AssetDetail(ctx, 1)
	check(t, err)
	if detail.PlaceID != 0 {
		t.Fatalf("blocked place leaked in detail: %d", detail.PlaceID)
	}
	check(t, st.RemoveBlock(ctx, blockID))
	detail, err = st.AssetDetail(ctx, 1)
	check(t, err)
	if detail.PlaceID != 1 {
		t.Fatalf("unblocked place not restored: %d", detail.PlaceID)
	}
}

func TestSmartAlbumPersonChangesInvalidateVisibility(t *testing.T) {
	st, ctx, a, b, _ := peopleFixture(t)
	person, err := st.NameFaces(ctx, []int64{facesOf(t, st, a)[0].ID}, "Ali")
	check(t, err)
	album, err := st.CreateAlbum(ctx, "Ali photos", core.AlbumSmart, core.AlbumRule{Person: person}, nil)
	check(t, err)
	_, err = st.SetBlock(ctx, core.BlockAlbum, album, "", core.BlockPhotos)
	check(t, err)
	face := facesOf(t, st, b)[0].ID
	for _, assigned := range []bool{true, false} {
		before, err := st.VisibilityRevision(ctx)
		check(t, err)
		if assigned {
			_, err = st.NameFaces(ctx, []int64{face}, "Ali")
		} else {
			err = st.RejectFaces(ctx, person, []int64{face})
		}
		check(t, err)
		after, err := st.VisibilityRevision(ctx)
		check(t, err)
		if after <= before {
			t.Fatal("person smart album membership did not invalidate visibility")
		}
		if visible, err := st.AssetVisible(ctx, b); err != nil || visible == assigned {
			t.Fatalf("assigned = %v, visible = %v, %v", assigned, visible, err)
		}
	}
}

// The visibility predicate runs over the whole library for timeline counts.
// Keep a representative blocked run measurable as block kinds are added.
func BenchmarkBlockedTimeline(b *testing.B) {
	ctx := context.Background()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	_, err = st.w.ExecContext(ctx, `WITH RECURSIVE n(x) AS
		(SELECT 1 UNION ALL SELECT x + 1 FROM n WHERE x < 100000)
		INSERT INTO assets(content_hash, kind, ext, taken_at, added_at)
		SELECT printf('%016x', x), 'photo', '.jpg', x, x FROM n`)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := st.SetBlock(ctx, core.BlockAsset, 1, "", core.BlockPhotos); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if n, err := st.CountVisible(ctx); err != nil || n != 99999 {
			b.Fatalf("visible = %d, %v", n, err)
		}
	}
}

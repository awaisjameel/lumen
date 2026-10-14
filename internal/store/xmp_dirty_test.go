package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"lumen/internal/core"
)

func clearXMPWork(t *testing.T, f xmpFixture) {
	t.Helper()
	batch, err := f.st.XMPDirtyAssets(f.ctx, 0, 100)
	check(t, err)
	check(t, f.st.SaveXMP(f.ctx, XMPChanges{Completed: batch.Pending}))
}

func dirtyXMPIDs(t *testing.T, f xmpFixture) []int64 {
	t.Helper()
	batch, err := f.st.XMPDirtyAssets(f.ctx, 0, 100)
	check(t, err)
	ids := make([]int64, len(batch.Pending))
	for i, m := range batch.Pending {
		ids[i] = m.AssetID
	}
	return ids
}

func TestXMPDirtyWorkNamesOnlyTheAssetsAnInputTouches(t *testing.T) {
	for _, name := range []string{
		"tag applied", "tag removed", "tag score", "tag moved", "tag renamed", "tag hidden", "tag deleted",
		"face placed", "face removed", "face box", "face moved", "person named", "person removed", "people merged",
		"favourite", "dimensions", "video kind", "hidden", "trash", "missing", "restored",
		"file added", "file removed", "file renamed", "file moved",
		"tag label block", "person label block", "asset block", "folder block", "block removed", "block retargeted",
		"face covered", "face uncovered", "face coverage moved", "album membership", "event membership", "place",
		"confidence", "confidence deleted", "confidence key", "enabled again", "export off", "unrelated settings",
	} {
		t.Run(name, func(t *testing.T) {
			f := newXMPFixture(t)
			st, ctx := f.st, f.ctx
			check(t, st.SetXMP(ctx, true))
			want := []int64{f.a}
			var query string
			var args []any
			var change func() error
			switch name {
			case "tag applied":
				change = func() error { return st.TagAsset(ctx, f.a, f.dogs) }
			case "tag removed":
				change = func() error { return st.UntagAsset(ctx, f.a, f.holiday) }
			case "tag score":
				query, args = "UPDATE asset_tags SET score = 0.8 WHERE asset_id = ? AND tag_id = ?", []any{f.a, f.beaches}
			case "tag moved":
				query, args = "UPDATE asset_tags SET asset_id = ? WHERE asset_id = ? AND tag_id = ?", []any{f.b, f.a, f.beaches}
				want = []int64{f.a, f.b}
			case "tag renamed":
				change = func() error { return st.RenameTag(ctx, f.holiday, "Trip") }
			case "tag hidden":
				query, args = "UPDATE tags SET hidden = 1 WHERE id = ?", []any{f.beaches}
			case "tag deleted":
				query, args = "DELETE FROM tags WHERE id = ?", []any{f.holiday}
			case "face placed":
				change = func() error { return errOf(st.NameFaces(ctx, []int64{facesOf(t, st, f.b)[0].ID}, "Sara")) }
				want = []int64{f.b}
			case "face removed":
				query, args = "DELETE FROM faces WHERE id = ?", []any{f.aliFace}
			case "face box":
				query, args = "UPDATE faces SET x = 0.2 WHERE id = ?", []any{f.aliFace}
			case "face moved":
				query, args = "UPDATE faces SET asset_id = ? WHERE id = ?", []any{f.b, f.aliFace}
				want = []int64{f.a, f.b}
			case "person named":
				change = func() error { return st.NamePerson(ctx, f.ali, "Alison") }
			case "person removed":
				query, args = "DELETE FROM people WHERE id = ?", []any{f.ali}
			case "people merged":
				other, err := st.NameFaces(ctx, []int64{facesOf(t, st, f.b)[0].ID}, "Sara")
				check(t, err)
				change = func() error { return st.MergePeople(ctx, other, f.ali) }
				want = []int64{f.b}
			case "favourite":
				change = func() error { return st.SetFavourite(ctx, f.a, true) }
			case "dimensions":
				query, args = "UPDATE assets SET width = 1000 WHERE id = ?", []any{f.a}
			case "video kind":
				query, args = "UPDATE assets SET kind = 'video' WHERE id = ?", []any{f.a}
			case "hidden", "trash", "missing", "restored":
				column := map[string]string{"hidden": "hidden", "trash": "trashed_at", "missing": "missing_since", "restored": "hidden"}[name]
				value := 1
				if name == "restored" {
					check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", f.a)))
					value = 0
				}
				query, args = "UPDATE assets SET "+column+" = ? WHERE id = ?", []any{value, f.a}
			case "file added":
				query, args = `INSERT INTO files(asset_id, source_id, path, size, mtime) VALUES (?, 1, 'C:\Photos\copy.jpg', 1, 0)`, []any{f.a}
			case "file removed":
				query, args = "DELETE FROM files WHERE asset_id = ?", []any{f.a}
			case "file renamed":
				query, args = `UPDATE files SET path = 'C:\Photos\renamed.jpg' WHERE asset_id = ?`, []any{f.a}
			case "file moved":
				query, args = "UPDATE files SET asset_id = ? WHERE asset_id = ?", []any{f.b, f.a}
				want = []int64{f.a, f.b}
			case "tag label block":
				change = func() error { return errOf(st.SetBlock(ctx, core.BlockTag, f.beaches, "", core.BlockLabel)) }
			case "person label block":
				change = func() error { return errOf(st.SetBlock(ctx, core.BlockPerson, f.ali, "", core.BlockLabel)) }
			case "asset block":
				change = func() error { return errOf(st.SetBlock(ctx, core.BlockAsset, f.a, "", core.BlockPhotos)) }
			case "folder block":
				change = func() error { return errOf(st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos`, core.BlockPhotos)) }
				want = []int64{f.a, f.b, f.c}
			case "block removed", "block retargeted":
				block, err := st.SetBlock(ctx, core.BlockAsset, f.a, "", core.BlockPhotos)
				check(t, err)
				change = func() error { return st.RemoveBlock(ctx, block) }
				if name == "block retargeted" {
					query, args, change = "UPDATE blocks SET target_id = ? WHERE id = ?", []any{f.b, block}, nil
					want = []int64{f.a, f.b}
				}
			case "face covered", "face uncovered", "face coverage moved":
				check(t, errOf(st.w.ExecContext(ctx, `INSERT INTO face_blocks(id, asset_id, at_ms, x, y, w, h, lookalikes) VALUES (1, ?, 0, 0, 0, 1, 1, 0)`, f.a)))
				query, args = "INSERT INTO face_block_faces(face_block_id, face_id, asset_id) VALUES (1, ?, ?)", []any{f.aliFace, f.a}
				if name != "face covered" {
					check(t, errOf(st.w.ExecContext(ctx, query, args...)))
					query, args = "DELETE FROM face_block_faces WHERE face_block_id = 1", nil
					if name == "face coverage moved" {
						query, args = "UPDATE face_block_faces SET face_id = ?, asset_id = ? WHERE face_block_id = 1", []any{facesOf(t, st, f.b)[0].ID, f.b}
						want = []int64{f.a, f.b}
					}
				}
			case "album membership":
				album, err := st.CreateAlbum(ctx, "Private", core.AlbumManual, core.AlbumRule{}, []int64{f.a})
				check(t, err)
				check(t, errOf(st.SetBlock(ctx, core.BlockAlbum, album, "", core.BlockPhotos)))
				change = func() error { return st.SetAlbumItem(ctx, f.b, album, true) }
				want = []int64{f.b}
			case "event membership":
				query = "INSERT INTO event_tags(id, kind, created_at) VALUES (1, 'block', 0)"
				check(t, errOf(st.w.ExecContext(ctx, query)))
				check(t, errOf(st.w.ExecContext(ctx, "INSERT INTO blocks(kind, target_id, mode, name, created_at) VALUES ('event', 1, 'photos', 'Private', 0)")))
				query, args = "INSERT INTO event_members(tag_id, asset_id) VALUES (1, ?)", []any{f.a}
			case "place":
				check(t, errOf(st.w.ExecContext(ctx, "INSERT INTO places(id, name, region, country, lat, lon) VALUES (1, 'Private', '', '', 0, 0)")))
				check(t, errOf(st.SetBlock(ctx, core.BlockPlace, 1, "", core.BlockPhotos)))
				query, args = "UPDATE assets SET place_id = 1 WHERE id = ?", []any{f.a}
			case "confidence", "confidence deleted", "confidence key":
				change = func() error { return st.SetFloat(ctx, core.SettingTagConfidence, 0.1) }
				if name != "confidence" {
					check(t, st.SetFloat(ctx, core.SettingTagConfidence, 0.1))
					query, args, change = "DELETE FROM settings WHERE key = ?", []any{core.SettingTagConfidence}, nil
					if name == "confidence key" {
						query = "UPDATE settings SET key = 'old_confidence' WHERE key = ?"
					}
				}
				want = []int64{f.a, f.b, f.c}
			case "enabled again":
				check(t, st.SetXMP(ctx, false))
				change = func() error { return st.SetXMP(ctx, true) }
				want = []int64{f.a, f.b, f.c}
			case "export off":
				check(t, st.SetXMP(ctx, false))
				change = func() error { return st.TagAsset(ctx, f.c, f.holiday) }
				want = nil
			case "unrelated settings":
				// Ratings and captions have no asset fields and are not part of today's packet.				query = "INSERT INTO settings(key, value) VALUES ('rating', '3'), ('caption', 'Holiday')"
				want = nil
			}
			clearXMPWork(t, f)
			if change != nil {
				check(t, change())
			} else {
				check(t, errOf(st.w.ExecContext(ctx, query, args...)))
			}
			if got := dirtyXMPIDs(t, f); !slices.Equal(got, want) {
				t.Fatalf("marked %v; want exactly %v", got, want)
			}
		})
	}
}

func TestXMPAcknowledgementKeepsChangesMadeAfterItsSnapshot(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetXMP(f.ctx, true))
	batch, err := f.st.XMPDirtyAssets(f.ctx, 0, 100)
	check(t, err)
	check(t, f.st.TagAsset(f.ctx, f.b, f.holiday))
	check(t, f.st.SaveXMP(f.ctx, XMPChanges{Completed: batch.Pending}))
	if got := dirtyXMPIDs(t, f); !slices.Equal(got, []int64{f.b}) {
		t.Fatalf("acknowledgement lost newer work: %v", got)
	}
}

func TestXMPDirtyWorkSurvivesClosingTheDatabaseBeforeAcknowledgement(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "library.db")
	st, err := Open(path)
	check(t, err)
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at) VALUES (1, 'a', 'photo', '.jpg', 0, 0)")
		return err
	}))
	check(t, st.SetXMP(ctx, true))
	before, err := st.XMPDirtyAssets(ctx, 0, 1)
	check(t, err)
	check(t, st.Close())
	st, err = Open(path)
	check(t, err)
	t.Cleanup(func() { check(t, st.Close()) })
	after, err := st.XMPDirtyAssets(ctx, 0, 1)
	check(t, err)
	if len(before.Pending) != 1 || !slices.Equal(before.Pending, after.Pending) {
		t.Fatalf("work changed across restart: %v -> %v", before.Pending, after.Pending)
	}
}

func TestXMPDirtyPagingIncludesEmptyAndDeletedAssets(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetXMP(f.ctx, true))
	check(t, errOf(f.st.w.ExecContext(f.ctx, "DELETE FROM assets WHERE id = ?", f.a)))
	var got []int64
	for after := int64(0); ; {
		batch, err := f.st.XMPDirtyAssets(f.ctx, after, 1)
		check(t, err)
		if len(batch.Pending) == 0 {
			break
		}
		after = batch.Pending[0].AssetID
		got = append(got, after)
	}
	if !slices.Equal(got, []int64{f.a, f.b, f.c}) {
		t.Fatalf("paging lost work: %v", got)
	}
}

func TestMigration0035CanRunAgainOverACurrentSchema(t *testing.T) {
	st := openTest(t)
	body, err := migrationFS.ReadFile("migrations/0035_xmp_dirty.sql")
	check(t, err)
	check(t, errOf(st.w.ExecContext(context.Background(), string(body))))
	ctx := t.Context()
	check(t, errOf(st.w.ExecContext(ctx, "INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at) VALUES (1, 'a', 'photo', '.jpg', 0, 0)")))
	check(t, st.SetXMP(ctx, true))
	batch, err := st.XMPDirtyAssets(ctx, 0, 1)
	check(t, err)
	if len(batch.Pending) != 1 || batch.Pending[0].AssetID != 1 {
		t.Fatalf("replayed migration did not queue the full export: %+v", batch.Pending)
	}
}

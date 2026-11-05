package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"lumen/internal/core"
)

func TestAlbumEditsRefuseConflictsAndReusedIdentities(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	id, err := st.CreateAlbum(ctx, "Original", core.AlbumSmart, core.AlbumRule{}, nil)
	check(t, err)
	read := func() core.Album { t.Helper(); a, err := album(ctx, st.r, id); check(t, err); return a }
	original := read()
	if len(original.Version.Identity) != 32 || len(original.Version.Revision) != 64 {
		t.Fatalf("version = %+v", original.Version)
	}
	rule := core.AlbumRule{Favourites: true}
	edit := core.AlbumEdit{ID: id, Version: original.Version, Name: "Changed", Rule: &rule}
	saved, err := st.SaveAlbum(ctx, edit)
	check(t, err)
	if saved != read().Version || saved.Identity != original.Version.Identity || saved.Revision == original.Version.Revision {
		t.Fatalf("saved = %+v", saved)
	}
	// A lost response can be retried without issuing another UPDATE.
	check(t, errOf(st.w.ExecContext(ctx, `CREATE TRIGGER refuse_album_edit BEFORE UPDATE ON albums BEGIN SELECT RAISE(ABORT, 'replayed write'); END`)))
	retry, err := st.SaveAlbum(ctx, edit)
	check(t, err)
	if retry != saved {
		t.Fatalf("retry = %+v; want %+v", retry, saved)
	}
	edit.Name = "Stale draft"
	if _, err := st.SaveAlbum(ctx, edit); !errors.Is(err, ErrAlbumChanged) {
		t.Fatalf("conflict = %v", err)
	}
	check(t, errOf(st.w.ExecContext(ctx, `DROP TRIGGER refuse_album_edit`)))
	check(t, st.DeleteAlbum(ctx, id))
	replacement, err := st.CreateAlbum(ctx, "Original", core.AlbumSmart, core.AlbumRule{}, nil)
	check(t, err)
	if replacement != id {
		t.Fatalf("fixture did not reuse id: %d != %d", replacement, id)
	}
	if read().Version.Identity == original.Version.Identity {
		t.Fatal("replacement reused entity identity")
	}
	edit.Name, edit.Rule = "Original", &original.Rule
	if _, err := st.SaveAlbum(ctx, edit); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replacement edit = %v", err)
	}
	if read().Name != "Original" {
		t.Fatal("old draft changed replacement")
	}
}

func TestAlbumNameEditsPreserveRulesAndRespectPrivacy(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	tag, err := st.CreateTag(ctx, "Private", core.TagManual, nil)
	check(t, err)
	rule := core.AlbumRule{Tag: tag, Favourites: true}
	id, err := st.CreateAlbum(ctx, "Smart", core.AlbumSmart, rule, nil)
	check(t, err)
	a, err := album(ctx, st.r, id)
	check(t, err)
	version, err := st.SaveAlbum(ctx, core.AlbumEdit{ID: id, Version: a.Version, Name: "Renamed"})
	check(t, err)
	a, err = album(ctx, st.r, id)
	check(t, err)
	if a.Rule != rule || a.Version != version {
		t.Fatalf("rename = %+v", a)
	}
	_, err = st.SetBlock(ctx, core.BlockTag, tag, "", core.BlockLabel)
	check(t, err)
	if _, err := st.SaveAlbum(ctx, core.AlbumEdit{ID: id, Version: version, Name: "Hidden reference", Rule: &rule}); !errors.Is(err, ErrAlbumReference) {
		t.Fatalf("hidden reference = %v", err)
	}
	// A rename introduces no reference and keeps the server's exact rule.
	version, err = st.SaveAlbum(ctx, core.AlbumEdit{ID: id, Version: version, Name: "Name only"})
	check(t, err)
	_, err = st.SetBlock(ctx, core.BlockAlbum, id, "", core.BlockLabel)
	check(t, err)
	if _, err := st.SaveAlbum(ctx, core.AlbumEdit{ID: id, Version: version, Name: "Blocked"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden album = %v", err)
	}
	if _, err := st.SaveAlbum(ctx, core.AlbumEdit{ID: id, Name: "Missing version"}); !errors.Is(err, ErrInvalidAlbum) {
		t.Fatalf("missing version = %v", err)
	}
}

func TestAlbumIdentityMigrationPreservesDefinitionsAndMemberships(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := sql.Open(driverName, path+"?_foreign_keys=1")
	check(t, err)
	t.Cleanup(func() { _ = db.Close() })
	names, err := migrationFS.ReadDir("migrations")
	check(t, err)
	for _, name := range names {
		if name.Name() >= "0040" {
			break
		}
		body, err := migrationFS.ReadFile("migrations/" + name.Name())
		check(t, err)
		check(t, errOf(db.ExecContext(ctx, string(body))))
	}
	check(t, errOf(db.ExecContext(ctx, `INSERT INTO albums(id, name, kind, rule_json, created_at) VALUES (42, 'Existing', 'manual', '{}', 1);
 INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at) VALUES (23, 'existing-asset', 'photo', '.jpg', 1, 1);
 INSERT INTO album_items(album_id, asset_id, added_at) VALUES (42, 23, 1);
 PRAGMA user_version = 39`)))
	check(t, db.Close())
	st, err := Open(path)
	check(t, err)
	t.Cleanup(func() { check(t, st.Close()) })
	a, err := album(ctx, st.r, 42)
	check(t, err)
	if a.ID != 42 || a.Name != "Existing" || a.Kind != core.AlbumManual || len(a.Version.Identity) != 32 {
		t.Fatalf("migrated = %+v", a)
	}
	var member int64
	check(t, st.r.QueryRowContext(ctx, `SELECT asset_id FROM album_items WHERE album_id = 42`).Scan(&member))
	if member != 23 {
		t.Fatalf("migrated member = %d", member)
	}
	before := a.Version
	a, err = album(ctx, st.r, 42)
	check(t, err)
	if a.Version != before {
		t.Fatal("reading changed identity")
	}
}

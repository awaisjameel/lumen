package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

func assertBlockedOracle(ctx context.Context, t *testing.T, db querier) {
	t.Helper()
	want, err := queryAll(ctx, db, scanID, `SELECT a.id FROM assets a WHERE EXISTS
		(SELECT 1 FROM blocks blk WHERE blk.mode = 'photos' AND `+blockMatch("a", "blk")+`) ORDER BY a.id`)
	check(t, err)
	got, err := queryAll(ctx, db, scanID, "SELECT asset_id FROM blocked_assets ORDER BY asset_id")
	check(t, err)
	if !slices.Equal(got, want) {
		t.Fatalf("materialised blocks = %v, original predicate = %v", got, want)
	}
	oldVisible, err := queryAll(ctx, db, scanID, "SELECT a.id FROM assets a WHERE "+baseVisibleOf("a")+` AND NOT EXISTS
		(SELECT 1 FROM blocks blk WHERE blk.mode = 'photos' AND `+blockMatch("a", "blk")+`) ORDER BY a.id`)
	check(t, err)
	newVisible, err := queryAll(ctx, db, scanID, "SELECT a.id FROM assets a WHERE "+visibleA+" ORDER BY a.id")
	check(t, err)
	if !slices.Equal(newVisible, oldVisible) {
		t.Fatalf("visible assets = %v, original predicate = %v", newVisible, oldVisible)
	}
	var pending int
	check(t, db.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM blocked_dirty) + (SELECT count(*) FROM blocked_rebuild)").Scan(&pending))
	if pending != 0 {
		t.Fatalf("settled coverage left %d dirty markers", pending)
	}
}

func TestVisibilityUsesOnlyAPrimaryKeyProbeForPhotoBlocks(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	for _, where := range []string{visible, visibleA, visibleX} {
		alias := "assets"
		switch where {
		case visibleA:
			alias = "a"
		case visibleX:
			alias = "x"
		}
		plan, err := queryAll(ctx, st.r, func(r *sql.Rows) (detail string, err error) {
			var id, parent, unused int
			err = r.Scan(&id, &parent, &unused, &detail)
			return detail, err
		}, "EXPLAIN QUERY PLAN SELECT "+alias+".id FROM assets "+alias+" WHERE "+where)
		check(t, err)
		joined := strings.Join(plan, "\n")
		if !strings.Contains(joined, "SEARCH blocked_assets USING INTEGER PRIMARY KEY") || strings.Contains(where, "FROM blocks") {
			t.Fatalf("visibility must probe blocked_assets by primary key: %s\n%s", where, joined)
		}
	}
}

// assertUnavailableOracle checks that search leaves out exactly the assets
// the user cannot browse.
func assertUnavailableOracle(ctx context.Context, t *testing.T, st *Store) {
	t.Helper()
	got, err := st.UnavailableAssets(ctx)
	check(t, err)
	want, err := queryAll(ctx, st.r, scanID, "SELECT id FROM assets WHERE NOT ("+visible+")")
	check(t, err)
	if len(got) != len(want) || slices.ContainsFunc(want, func(id int64) bool { return !got[id] }) {
		t.Fatalf("search leaves out %v, want the assets that are not visible: %v", slices.Sorted(maps.Keys(got)), want)
	}
}

func TestSearchReadsItsExclusionsWithoutTestingEveryAsset(t *testing.T) {
	plan := queryPlan(t, openTest(t), unavailableSQL)
	if !strings.Contains(plan, "COVERING INDEX assets_unavailable") || slices.Contains(strings.Split(plan, "\n"), "SCAN assets") {
		t.Fatalf("search's exclusions must come from the assets_unavailable index:\n%s", plan)
	}
}

func TestLocalBlockSettlementRemovesAssetsByPrimaryKey(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	plan, err := queryAll(ctx, st.r, func(r *sql.Rows) (detail string, err error) {
		var id, parent, unused int
		err = r.Scan(&id, &parent, &unused, &detail)
		return detail, err
	}, "EXPLAIN QUERY PLAN "+removeDirtyBlocksSQL)
	check(t, err)
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "SEARCH blocked_assets USING INTEGER PRIMARY KEY") || strings.Contains(joined, "SCAN blocked_assets") {
		t.Fatalf("local settlement must seek dirty asset keys: %s", joined)
	}
}

func TestBlockSettlementPreservesEveryDeferredRevisionIncrement(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%t", full), func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			seedBlockedOracle(ctx, t, st)
			_, err := st.SetBlock(ctx, core.BlockTag, 1, "", core.BlockPhotos)
			check(t, err)
			var beforeEvents, beforeVisibility int64
			check(t, st.r.QueryRowContext(ctx, "SELECT value, visible_value FROM event_revision WHERE id = 1").Scan(&beforeEvents, &beforeVisibility))
			assertRevisions := func(db querier) {
				var events, visibility int64
				check(t, db.QueryRowContext(ctx, "SELECT value, visible_value FROM event_revision WHERE id = 1").Scan(&events, &visibility))
				if events != beforeEvents+3 || visibility != beforeVisibility+3 {
					t.Fatalf("revision totals = (%d, %d), want (%d, %d)", events, visibility, beforeEvents+3, beforeVisibility+3)
				}
			}
			check(t, st.tx(ctx, func(tx *sql.Tx) error {
				for _, query := range []string{
					"INSERT INTO asset_tags(asset_id, tag_id, source, score) VALUES (2, 1, 'ai', 0.001)",
					"UPDATE asset_tags SET asset_id = 3, score = 0.5 WHERE asset_id = 2 AND tag_id = 1",
					"DELETE FROM asset_tags WHERE asset_id = 3 AND tag_id = 1",
				} {
					if _, err := tx.ExecContext(ctx, query); err != nil {
						return err
					}
				}
				if full {
					if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1)"); err != nil {
						return err
					}
				}
				var events, visibility int64
				check(t, tx.QueryRowContext(ctx, "SELECT sum(event_increments), sum(visible_increments) FROM blocked_dirty").Scan(&events, &visibility))
				if events != 3 || visibility != 3 {
					t.Fatalf("dirty revisions = (%d, %d), want (3, 3)", events, visibility)
				}
				if err := settleBlockedAssets(ctx, tx); err != nil {
					return err
				}
				assertRevisions(tx)
				assertBlockedOracle(ctx, t, tx)
				return nil
			}))
			assertRevisions(st.r)
		})
	}
}

func seedBlockedOracle(ctx context.Context, t *testing.T, st *Store) {
	t.Helper()
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0)`,
			`INSERT INTO tags(id, name, kind, created_at) VALUES (1, 'Secret', 'manual', 0), (2, 'Other', 'manual', 0)`,
			`INSERT INTO people(id, name, created_at) VALUES (1, 'Alice', 0), (2, 'Bob', 0)`,
			`INSERT INTO places(id, name, region, country, lat, lon) VALUES (1, 'Here', '', '', 0, 0), (2, 'There', '', '', 0, 0)`,
			`INSERT INTO albums(id, name, kind, rule_json, created_at) VALUES (1, 'Manual', 'manual', '{}', 0), (2, 'Smart', 'smart', '{}', 0)`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<16)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, place_id)
			SELECT x, printf('%016x', x), 'photo', '.jpg', 1700000000+x*172800, 0, 1 FROM n`,
			`INSERT INTO files(id, asset_id, source_id, path, size, mtime) SELECT id AS id, id AS asset_id, 1, 'C:\Photos\Private\'||id||'.jpg', 1, 0 FROM assets`,
			`INSERT INTO faces(id, asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id)
			SELECT id AS id, id AS asset_id, 0.1, 0.1, 0.2, 0.2, 1, 20, 0, 'test', zeroblob(512), 1 FROM assets`,
			`INSERT INTO face_blocks(id, asset_id, at_ms, x, y, w, h, face_id, lookalikes)
			VALUES (1, 1, 0, 0.1, 0.1, 0.2, 0.2, 1, 0), (2, 1, 0, 0.1, 0.1, 0.2, 0.2, 1, 0)`,
			`INSERT INTO event_tags(id, kind, created_at) VALUES (1, 'block', 0), (2, 'block', 0)`,
			`INSERT INTO event_members(tag_id, asset_id) VALUES (1, 1), (2, 2)`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

type blockedOracleChange struct {
	name string
	sql  string
	args func(*rand.Rand) []any
}

func blockedOracleChanges() []blockedOracleChange {
	asset := func(r *rand.Rand) []any { return []any{int64(2 + r.IntN(14))} }
	changes := []blockedOracleChange{
		{"tag add", `INSERT OR REPLACE INTO asset_tags(asset_id, tag_id, source, score) VALUES (?, 1, 'ai', 0.03)`, asset},
		{"tag remove", `DELETE FROM asset_tags WHERE asset_id = ?`, asset},
		{"tag change", `UPDATE asset_tags SET source = CASE source WHEN 'user' THEN 'rejected' ELSE 'user' END, score = 0.001 WHERE asset_id = ?`, asset},
		{"tag identity", `UPDATE OR IGNORE asset_tags SET tag_id = 3-tag_id WHERE asset_id = ?`, asset},
		{"tag asset", `UPDATE OR IGNORE asset_tags SET asset_id = 17-asset_id WHERE asset_id = ?`, asset},
		{"face add", `INSERT OR IGNORE INTO faces(id, asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id)
			SELECT id AS id, id AS asset_id, 0.1, 0.1, 0.2, 0.2, 1, 20, 0, 'test', zeroblob(512), 1 FROM assets WHERE id = ?`, asset},
		{"face remove", `DELETE FROM faces WHERE id = ?`, asset},
		{"face person", `UPDATE faces SET person_id = CASE person_id WHEN 1 THEN 2 ELSE 1 END WHERE id = ?`, asset},
		{"face asset", `UPDATE faces SET asset_id = 17-asset_id WHERE id = ?`, asset},
		{"file add", `INSERT OR IGNORE INTO files(id, asset_id, source_id, path, size, mtime)
			SELECT id AS id, id AS asset_id, 1, 'C:\Photos\Private\'||id||'.jpg', 1, 0 FROM assets WHERE id = ?`, asset},
		{"file remove", `DELETE FROM files WHERE id = ?`, asset},
		{"file move", `UPDATE files SET path = 'C:\Photos\' || CASE WHEN instr(path, 'Private')>0 THEN 'Open' ELSE 'Private' END || '\'||id||'.jpg' WHERE id = ?`, asset},
		{"file content", `UPDATE files SET asset_id = 17-asset_id WHERE id = ?`, asset},
		{"album item add", `INSERT OR IGNORE INTO album_items(album_id, asset_id, added_at) VALUES (1, ?, 0)`, asset},
		{"album item remove", `DELETE FROM album_items WHERE asset_id = ?`, asset},
		{"album item change", `UPDATE OR IGNORE album_items SET asset_id = 17-asset_id, album_id = 3-album_id WHERE asset_id = ?`, asset},
		{"smart rule", `UPDATE albums SET rule_json = ? WHERE id = 2`, func(r *rand.Rand) []any {
			return []any{[]string{`{}`, `{"kind":"video"}`, `{"favourites":true}`, `{"from":"2023-11-20","to":"2023-12-01"}`, `{"tag":1}`, `{"person":1}`, `{"kind":"photo","favourites":true,"tag":1,"person":2}`}[r.IntN(7)]}
		}},
		{"album kind", `UPDATE albums SET kind = CASE kind WHEN 'smart' THEN 'manual' ELSE 'smart' END WHERE id = 2`, nil},
		{"event add", `INSERT OR IGNORE INTO event_members(tag_id, asset_id) SELECT id, ? FROM event_tags WHERE id = 1`, asset},
		{"event remove", `DELETE FROM event_members WHERE asset_id = ?`, asset},
		{"event change", `UPDATE OR IGNORE event_members SET tag_id = 3-tag_id, asset_id = 17-asset_id WHERE asset_id = ? AND EXISTS (SELECT 1 FROM event_tags WHERE id = 3-tag_id)`, asset},
		{"event target add", `INSERT OR IGNORE INTO event_tags(id, kind, created_at) VALUES (1, 'block', 0), (2, 'block', 0)`, nil},
		{"face coverage add", `INSERT OR IGNORE INTO face_block_faces(face_block_id, face_id, asset_id) SELECT b.id, f.id, f.asset_id FROM faces f CROSS JOIN face_blocks b WHERE b.id = 1 AND f.id = ?`, asset},
		{"face coverage remove", `DELETE FROM face_block_faces WHERE face_id = ?`, asset},
		{"face coverage change", `UPDATE OR IGNORE face_block_faces SET face_block_id = 3-face_block_id WHERE face_id = ? AND EXISTS (SELECT 1 FROM face_blocks WHERE id = 3-face_block_id)`, asset},
		{"face coverage asset", `UPDATE face_block_faces SET asset_id = 17-asset_id WHERE face_id = ?`, asset},
		{"face coverage identity", `UPDATE OR IGNORE face_block_faces SET face_id = 17-face_id WHERE face_id = ? AND EXISTS (SELECT 1 FROM faces WHERE id = 17-face_id)`, asset},
		{"face target add", `INSERT OR IGNORE INTO face_blocks(id, asset_id, at_ms, x, y, w, h, lookalikes) VALUES (1, 1, 0, 0.1, 0.1, 0.2, 0.2, 0), (2, 1, 0, 0.1, 0.1, 0.2, 0.2, 0)`, nil},
		{"favourite", `UPDATE assets SET favourite = 1-favourite WHERE id = ?`, asset},
		{"place", `UPDATE assets SET place_id = 3-place_id WHERE id = ?`, asset},
		{"kind", `UPDATE assets SET kind = CASE kind WHEN 'photo' THEN 'video' ELSE 'photo' END WHERE id = ?`, asset},
		{"date", `UPDATE assets SET taken_at = taken_at + 86400 WHERE id = ?`, asset},
		{"hidden", `UPDATE assets SET hidden = 1-hidden WHERE id = ?`, asset},
		{"trashed", `UPDATE assets SET trashed_at = CASE WHEN trashed_at IS NULL THEN 1 ELSE NULL END WHERE id = ?`, asset},
		{"missing", `UPDATE assets SET missing_since = CASE WHEN missing_since IS NULL THEN 1 ELSE NULL END WHERE id = ?`, asset},
		{"confidence save", `INSERT INTO settings(key, value) VALUES ('tag_confidence', ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, func(r *rand.Rand) []any { return []any{[]string{"0.02", "0.5", "0", "broken"}[r.IntN(4)]} }},
		{"confidence delete", `DELETE FROM settings WHERE key = 'tag_confidence'`, nil},
		{"confidence key", `UPDATE OR IGNORE settings SET key = 'other_confidence' WHERE key = 'tag_confidence'`, nil},
		{"confidence key return", `UPDATE OR IGNORE settings SET key = 'tag_confidence' WHERE key = 'other_confidence'`, nil},
		{"asset delete", `DELETE FROM assets WHERE id = 16`, nil},
		{"asset insert", `INSERT OR IGNORE INTO assets(id, content_hash, kind, ext, taken_at, added_at) VALUES (16, '0000000000000010', 'photo', '.jpg', 1700000000, 0)`, nil},
	}
	for i, kind := range []core.BlockKind{core.BlockAsset, core.BlockPerson, core.BlockTag, core.BlockPlace, core.BlockFolder, core.BlockAlbum, core.BlockEvent, core.BlockFace} {
		args := func(r *rand.Rand) []any {
			var target any = int64(1)
			var path any
			mode := core.BlockPhotos
			if kind == core.BlockFolder {
				target, path = nil, `C:\Photos\Private`
			} else if kind != core.BlockAsset && r.IntN(2) == 0 {
				mode = core.BlockLabel
			}
			return []any{100 + i, kind, target, path, mode}
		}
		changes = append(changes,
			blockedOracleChange{string(kind) + " block add", `INSERT OR IGNORE INTO blocks(id, kind, target_id, path, mode, name, created_at) VALUES (?, ?, ?, ?, ?, 'Blocked', 0)`, args},
			blockedOracleChange{string(kind) + " block remove", `DELETE FROM blocks WHERE id = ?`, func(_ *rand.Rand) []any { return []any{100 + i} }},
			blockedOracleChange{string(kind) + " block change", `UPDATE blocks SET name = name || 'x', mode = CASE WHEN mode = 'photos' AND kind NOT IN ('asset', 'folder') THEN 'label' ELSE 'photos' END WHERE id = ?`, func(_ *rand.Rand) []any { return []any{100 + i} }},
		)
		if kind == core.BlockFolder {
			changes = append(changes, blockedOracleChange{"folder block path", `UPDATE blocks SET path = CASE WHEN instr(path, 'Private') > 0 THEN 'C:\Photos\Open' ELSE 'C:\Photos\Private' END WHERE id = ?`, func(_ *rand.Rand) []any { return []any{100 + i} }})
		} else {
			changes = append(changes, blockedOracleChange{string(kind) + " block target", `UPDATE OR IGNORE blocks SET target_id = 3-target_id WHERE id = ?`, func(_ *rand.Rand) []any { return []any{100 + i} }})
		}
	}
	changes = append(changes,
		blockedOracleChange{"smart block add", `INSERT OR IGNORE INTO blocks(id, kind, target_id, mode, name, created_at) VALUES (200, 'album', 2, 'photos', 'Smart', 0)`, nil},
		blockedOracleChange{"smart block mode", `UPDATE blocks SET mode = CASE mode WHEN 'photos' THEN 'label' ELSE 'photos' END WHERE id = 200`, nil},
		blockedOracleChange{"block target", `UPDATE OR IGNORE blocks SET target_id = 3-target_id WHERE id = 200`, nil},
		blockedOracleChange{"block kind", `UPDATE OR IGNORE blocks SET kind = CASE kind WHEN 'album' THEN 'event' ELSE 'album' END WHERE id = 200`, nil},
	)
	return changes
}

func TestMaterialisedBlocksEqualTheOriginalPredicateThroughRandomisedChanges(t *testing.T) {
	for _, seed := range []uint64{1, 42, 20261013} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			seedBlockedOracle(ctx, t, st)
			rng := rand.New(rand.NewPCG(seed, seed+1))
			changes := blockedOracleChanges()
			t.Logf("checking %d mutations across %d input operations", 12*len(changes), len(changes))
			for round := range 12 {
				for _, index := range rng.Perm(len(changes)) {
					change := changes[index]
					var args []any
					if change.args != nil {
						args = change.args(rng)
					}
					rollback := round%4 == 3 && index%7 == 0
					errRollback := errors.New("oracle rollback")
					err := st.tx(ctx, func(tx *sql.Tx) error {
						if _, err := tx.ExecContext(ctx, change.sql, args...); err != nil {
							return err
						}
						// Check this transaction's current predicate before event settling,
						// then let Store.tx settle membership and coverage together again.
						if err := settleBlockedAssets(ctx, tx); err != nil {
							return err
						}
						assertBlockedOracle(ctx, t, tx)
						if err := settleDirty(ctx, tx); err != nil {
							return err
						}
						if err := settleBlockedAssets(ctx, tx); err != nil {
							return err
						}
						assertBlockedOracle(ctx, t, tx)
						if rollback {
							return errRollback
						}
						return nil
					})
					if err != nil && (!rollback || !errors.Is(err, errRollback)) {
						t.Fatalf("seed %d round %d %s: %v", seed, round, change.name, err)
					}
					check(t, st.read(ctx, func(tx *sql.Tx) error {
						assertBlockedOracle(ctx, t, tx)
						return nil
					}))
					assertUnavailableOracle(ctx, t, st)
				}
			}
		})
	}
}

func TestFailedBlockSettlementRollsBackTheInputsAndCoverage(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	_, err := st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)
	check(t, err)
	writeInTx(t, st, `CREATE TRIGGER refuse_block_settle BEFORE INSERT ON blocked_assets BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	err = st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE files SET path = 'C:\Photos\Private\changed.jpg' WHERE asset_id = 2`)
		return err
	})
	if err == nil {
		t.Fatal("a failed block settle committed its inputs")
	}
	var path string
	check(t, st.r.QueryRowContext(ctx, "SELECT path FROM files WHERE asset_id = 2").Scan(&path))
	if path != `C:\Photos\Private\2.jpg` {
		t.Fatal("the failed settle left the input changed")
	}
	assertBlockedOracle(ctx, t, st.r)
}

func TestOpeningAnOlderLibraryBackfillsBlocksBeforeBrowsing(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := sql.Open(driverName, path+"?_foreign_keys=1")
	check(t, err)
	st := &Store{w: &storeWriter{DB: db}}
	st.w.execContext = st.execContext
	names, err := migrationFS.ReadDir("migrations")
	check(t, err)
	for _, name := range names {
		if name.Name() >= "0032" {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name.Name())
		check(t, err)
		check(t, st.runTx(ctx, false, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, string(body))
			return err
		}))
	}
	_, err = db.ExecContext(ctx, `INSERT INTO assets(content_hash, kind, ext, taken_at, added_at) VALUES ('old', 'photo', '.jpg', 0, 0);
		INSERT INTO blocks(kind, target_id, mode, name, created_at) VALUES ('asset', 1, 'photos', 'Old block', 0);
		PRAGMA user_version = 31`)
	check(t, err)
	check(t, db.Close())
	opened, err := Open(path)
	check(t, err)
	t.Cleanup(func() { check(t, opened.Close()) })
	assertBlockedOracle(ctx, t, opened.r)
	n, err := opened.CountVisible(ctx)
	check(t, err)
	if n != 0 {
		t.Fatalf("opening exposed %d blocked assets", n)
	}
}

func TestLocalBlockInputsSettleOnlyTheChangedAssets(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	_, err := st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)
	check(t, err)
	writeInTx(t, st, `CREATE TRIGGER refuse_unrelated_block_settle BEFORE INSERT ON blocked_assets
		WHEN NEW.asset_id != 2 BEGIN SELECT RAISE(ABORT, 'unrelated asset'); END`)
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE files SET path = 'C:\Photos\Private\changed.jpg' WHERE asset_id = 2`)
		if err != nil {
			return err
		}
		dirty, err := queryAll(ctx, tx, scanID, "SELECT asset_id FROM blocked_dirty ORDER BY asset_id")
		if err != nil {
			return err
		}
		if !slices.Equal(dirty, []int64{2}) {
			t.Fatalf("a local change dirtied %v, want only asset 2", dirty)
		}
		var full bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM blocked_rebuild)").Scan(&full); err != nil {
			return err
		}
		if full {
			t.Fatal("an asset change requested a full rebuild")
		}
		return nil
	}))
	assertBlockedOracle(ctx, t, st.r)
}

func TestBlockSettlementAndItsInputsBecomeVisibleInOneCommit(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	id, err := st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)
	check(t, err)
	snapshot, err := st.r.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	check(t, err)
	defer func() { _ = snapshot.Rollback() }()
	count := func(db querier, want int) {
		t.Helper()
		var got int
		check(t, db.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+visible).Scan(&got))
		if got != want {
			t.Fatalf("snapshot has %d visible photos, want %d", got, want)
		}
	}
	count(snapshot, 0)
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM blocks WHERE id = ?", id); err != nil {
			return err
		}
		if err := settleBlockedAssets(ctx, tx); err != nil {
			return err
		}
		assertBlockedOracle(ctx, t, tx)
		count(tx, 16)
		// Even after the answer changes on the writer, all readers still see
		// the committed block and its committed coverage together.
		count(st.r, 0)
		count(snapshot, 0)
		return nil
	}))
	count(snapshot, 0)
	count(st.r, 16)
}

func TestBatchAdmissionSeesABlockCreatedByAnEarlierItem(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	check(t, st.UpdateAlbum(ctx, 2, "Smart", core.AlbumRule{Favourites: true}))
	_, err := st.SetBlock(ctx, core.BlockAlbum, 2, "", core.BlockPhotos)
	check(t, err)
	outcomes, err := st.ApplyBatch(ctx, []int64{2, 2}, core.BatchFavourite, 0)
	check(t, err)
	if len(outcomes) != 2 || outcomes[0].Err != nil || !errors.Is(outcomes[1].Err, ErrNotFound) {
		t.Fatalf("the duplicate's admission ignored its new block: %+v", outcomes)
	}
	assertBlockedOracle(ctx, t, st.r)
}

func TestChangedMaterialisedMembershipInvalidatesCachedVisibility(t *testing.T) {
	for _, input := range []string{"kind", "label face coverage"} {
		t.Run(input, func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			seedBlockedOracle(ctx, t, st)
			rule := core.AlbumRule{Kind: core.KindVideo}
			if input == "label face coverage" {
				rule = core.AlbumRule{Person: 1}
				_, err := st.SetBlock(ctx, core.BlockFace, 1, "", core.BlockLabel)
				check(t, err)
			}
			check(t, st.UpdateAlbum(ctx, 2, "Smart", rule))
			_, err := st.SetBlock(ctx, core.BlockAlbum, 2, "", core.BlockPhotos)
			check(t, err)
			before, err := st.VisibilityRevision(ctx)
			check(t, err)
			if input == "kind" {
				writeInTx(t, st, "UPDATE assets SET kind = 'video' WHERE id = 2")
			} else {
				writeInTx(t, st, "INSERT INTO face_block_faces(face_block_id, face_id, asset_id) VALUES (1, 2, 2)")
			}
			after, err := st.VisibilityRevision(ctx)
			check(t, err)
			if after <= before {
				t.Fatal("changed block membership did not invalidate visibility")
			}
			assertBlockedOracle(ctx, t, st.r)
		})
	}
}

func TestSettingsWrittenTogetherSettleBlockedAssets(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	writeInTx(t, st, "INSERT INTO asset_tags(asset_id, tag_id, source, score) VALUES (2, 1, 'ai', 0.001)")
	_, err := st.SetBlock(ctx, core.BlockTag, 1, "", core.BlockPhotos)
	check(t, err)
	st.settingsMu.Lock()
	err = st.writeSettings(ctx, []setting{{core.SettingTagConfidence, "0"}})
	st.settingsMu.Unlock()
	check(t, err)
	shown, err := st.AssetVisible(ctx, 2, false)
	check(t, err)
	if shown {
		t.Fatal("lowering the confidence left a newly blocked photo visible")
	}
	assertBlockedOracle(ctx, t, st.r)
}

func TestMaterialisedBlocksPreserveTheOriginalUnreadableConfidenceRule(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	writeInTx(t, st, "INSERT INTO asset_tags(asset_id, tag_id, source, score) VALUES (2, 1, 'ai', 0.001)")
	_, err := st.SetBlock(ctx, core.BlockTag, 1, "", core.BlockPhotos)
	check(t, err)
	shown, err := st.AssetVisible(ctx, 2, false)
	check(t, err)
	if !shown {
		t.Fatal("the low-confidence tag matched the default threshold")
	}
	writeInTx(t, st, "INSERT INTO settings(key, value) VALUES ('tag_confidence', 'broken')")
	confidence, err := st.Float(ctx, core.SettingTagConfidence, core.TagConfidenceDefault)
	check(t, err)
	if confidence != core.TagConfidenceDefault {
		t.Fatal("the setting reader did not fall back to the default")
	}
	// The existing SQL casts unreadable text to zero, while Float uses the
	// default. Keep that discrepancy explicit without redefining privacy here.
	shown, err = st.AssetVisible(ctx, 2, false)
	check(t, err)
	if shown {
		t.Fatal("materialisation changed the original unreadable-confidence predicate")
	}
	assertBlockedOracle(ctx, t, st.r)
}

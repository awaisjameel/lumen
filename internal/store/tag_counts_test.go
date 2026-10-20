package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"lumen/internal/core"
)

func assertTagCountValues(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()
	type count struct {
		id int64
		n  int
	}
	scan := func(r *sql.Rows) (c count, err error) { err = r.Scan(&c.id, &c.n); return c, err }
	got, err := queryAll(ctx, tx, scan, "SELECT tag_id, n FROM tag_counts ORDER BY tag_id")
	check(t, err)
	want, err := queryAll(ctx, tx, scan, `SELECT t.id, count(a.id) FROM tags t
 LEFT JOIN asset_tags x ON x.tag_id = t.id AND `+tagAppliesTo("x", tagConfidenceSQL)+`
 LEFT JOIN assets a ON a.id = x.asset_id AND `+visibleA+` GROUP BY t.id ORDER BY t.id`)
	check(t, err)
	if !slices.Equal(got, want) {
		t.Fatalf("kept counts %v, reference %v", got, want)
	}
}

func assertTagCounts(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()
	assertTagCountValues(ctx, t, tx)
	var threshold float64
	check(t, tx.QueryRowContext(ctx, "SELECT "+tagConfidenceSQL).Scan(&threshold))
	for _, score := range []float64{threshold, math.Nextafter(threshold, math.Inf(1)), 0.7} {
		kept, err := thingsIn(ctx, tx, score)
		check(t, err)
		check(t, addTagCovers(ctx, tx, kept, score))
		reference, err := thingsCounted(ctx, tx, score)
		check(t, err)
		check(t, addTagCovers(ctx, tx, reference, score))
		if !slices.Equal(kept, reference) {
			t.Fatalf("threshold %.18g: kept %+v, reference %+v", score, kept, reference)
		}
	}
}

func tagCountChanges() []blockedOracleChange {
	changes := keptCountChanges()
	changes = append(changes,
		blockedOracleChange{"thumbnail", "UPDATE assets SET thumb_version = (thumb_version+1)%4 WHERE id = ?", changes[0].args},
		blockedOracleChange{"score", "UPDATE asset_tags SET score = ? WHERE asset_id = ?", func(r *rand.Rand) []any {
			return []any{[]any{nil, 0.001, 0.02, 0.03, 0.5, 1.0}[r.IntN(6)], 2 + r.IntN(14)}
		}},
		blockedOracleChange{"label hidden", "UPDATE tags SET hidden = 1-hidden WHERE id = 1", nil},
		blockedOracleChange{"label pin", "UPDATE tags SET pinned_at = CASE WHEN pinned_at IS NULL THEN 1 END WHERE id = 1", nil},
		blockedOracleChange{"label rename", "UPDATE tags SET name = CASE name WHEN 'Secret' THEN 'Renamed' ELSE 'Secret' END WHERE id = 1", nil},
		blockedOracleChange{"label add", "INSERT OR IGNORE INTO tags(id, name, kind, created_at) VALUES (3, 'Third', 'manual', 0)", nil},
		blockedOracleChange{"label match", "INSERT OR IGNORE INTO asset_tags(asset_id, tag_id, source) SELECT 1, id, 'user' FROM tags WHERE id = 3", nil},
		blockedOracleChange{"label delete", "DELETE FROM tags WHERE id = 3", nil},
	)
	return changes
}

func TestTagCountsAndCoversEqualTheReferenceAfterEveryRandomisedChange(t *testing.T) {
	t.Parallel()
	for _, limit := range []int64{keptCountsThreshold, 1} {
		for _, seed := range []uint64{1, 42, 20261014} {
			t.Run(fmt.Sprintf("limit=%d/seed=%d", limit, seed), func(t *testing.T) {
				t.Parallel()
				st := openTest(t)
				ctx := context.WithValue(t.Context(), keptCountsThresholdKey{}, limit)
				seedBlockedOracle(ctx, t, st)
				writeInTx(t, st, "UPDATE assets SET thumb_version = id%3")
				rng := rand.New(rand.NewPCG(seed, seed+1))
				changes := tagCountChanges()
				t.Logf("checking %d changes across %d operations", 12*len(changes), len(changes))
				for round := range 12 {
					for _, index := range rng.Perm(len(changes)) {
						change := changes[index]
						var args []any
						if change.args != nil {
							args = change.args(rng)
						}
						rollback := round%4 == 3 && index%7 == 0
						errRollback := errKeptCountRollback
						err := applyKeptCountChange(ctx, t, st, change.sql, args, rollback)
						if err != nil && (!rollback || !errors.Is(err, errRollback)) {
							t.Fatalf("round %d %s: %v", round, change.name, err)
						}
						check(t, st.read(ctx, func(tx *sql.Tx) error { assertTagCounts(ctx, t, tx); return nil }))
					}
				}
			})
		}
	}
}

func TestThingsUseKeptCountsOnlyAtTheExactStoredThreshold(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	check(t, st.SetFloat(ctx, core.SettingTagConfidence, 0.03))
	writeInTx(t, st, "INSERT INTO asset_tags(asset_id, tag_id, source, score) VALUES (1, 1, 'ai', 0.03)")
	// A sentinel inside a rolled-back transaction proves which read path ran.
	sentinel := errors.New("rollback sentinel")
	err := st.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE tag_counts SET n = 37 WHERE tag_id = 1"); err != nil {
			return err
		}
		for _, score := range []float64{0.03, math.Nextafter(0.03, 0), math.Nextafter(0.03, 1), 0.5} {
			got, err := thingsIn(ctx, tx, score)
			if err != nil {
				return err
			}
			want := 0
			switch {
			case score == 0.03:
				want = 37
			case score < 0.03:
				want = 1
			}
			for _, tag := range got {
				if tag.ID == 1 && tag.Count != want {
					t.Fatalf("score %.18g: count %d, want %d", score, tag.Count, want)
				}
			}
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	// Signed zero compares equal numerically but must still take the fallback
	// when its bits differ from SQLite's stored spelling.
	err = st.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE settings SET value = '-0.0' WHERE key = 'tag_confidence'"); err != nil {
			return err
		}
		var zero float64
		if err := tx.QueryRowContext(ctx, "SELECT "+tagConfidenceSQL).Scan(&zero); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE tag_counts SET n = 37 WHERE tag_id = 1"); err != nil {
			return err
		}
		for _, score := range []float64{zero, math.Copysign(0, -math.Copysign(1, zero))} {
			got, err := thingsIn(ctx, tx, score)
			if err != nil {
				return err
			}
			want := 1
			if math.Float64bits(score) == math.Float64bits(zero) {
				want = 37
			}
			for _, tag := range got {
				if tag.ID == 1 && tag.Count != want {
					t.Fatalf("signed zero %x: count %d, want %d", math.Float64bits(score), tag.Count, want)
				}
			}
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	// SQL's original malformed-value cast is zero; a normal Float caller uses
	// the default and must fall back rather than use those zero-threshold counts.
	check(t, st.setSetting(ctx, core.SettingTagConfidence, "broken"))
	writeInTx(t, st, "UPDATE asset_tags SET score = 0.001")
	got, err := st.Things(ctx, core.TagConfidenceDefault)
	check(t, err)
	for _, tag := range got {
		if tag.Count != 0 {
			t.Fatalf("malformed setting: %+v", got)
		}
	}
}

func TestTagCountsSubtractDeletedAssetsOnceAcrossCascades(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	writeInTx(t, st, "INSERT INTO asset_tags(asset_id, tag_id, source) SELECT id, 1, 'user' FROM assets")
	check(t, errOf(st.SetBlock(ctx, core.BlockAsset, 2, "", core.BlockPhotos)))
	writeInTx(t, st, "UPDATE assets SET hidden = 1 WHERE id = 3")
	for _, id := range []int{1, 2, 3} {
		writeInTx(t, st, "DELETE FROM assets WHERE id = ?", id)
		check(t, st.read(ctx, func(tx *sql.Tx) error { assertTagCounts(ctx, t, tx); return nil }))
	}
}

func TestTagCountTriggersIgnoreUnchangedContributionsAndThumbnailChanges(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	writeInTx(t, st, "INSERT INTO asset_tags(asset_id, tag_id, source, score) VALUES (1, 1, 'user', 1), (2, 2, 'ai', 0.5), (3, 2, 'ai', 0.001)")
	refuseKeptCountWrites(t, st, "tag_counts")
	writeInTx(t, st, "UPDATE assets SET hidden = hidden, trashed_at = trashed_at, missing_since = missing_since, thumb_version = thumb_version+1, width = width+1")
	writeInTx(t, st, "UPDATE asset_tags SET source = source, score = score, asset_id = asset_id, tag_id = tag_id")
	writeInTx(t, st, "UPDATE asset_tags SET score = score+0.0001")
	writeInTx(t, st, "UPDATE asset_tags SET source = 'ai' WHERE asset_id = 1")
	check(t, st.SetFloat(ctx, core.SettingTagConfidence, core.TagConfidenceDefault))
	check(t, st.SetFloat(ctx, core.SettingTagConfidence, core.TagConfidenceDefault))
	check(t, st.read(ctx, func(tx *sql.Tx) error { assertTagCounts(ctx, t, tx); return nil }))
}

func TestTagCountReadersKeepTheirSnapshotBesideAWriter(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	writeInTx(t, st, "INSERT INTO asset_tags(asset_id, tag_id, source, score) SELECT id, 1, 'ai', 0.5 FROM assets")
	started, finished := make(chan struct{}), make(chan error, 1)
	var readers sync.WaitGroup
	readers.Add(3)
	for range 3 {
		go func() {
			defer readers.Done()
			<-started
			for range 60 {
				check(t, st.read(ctx, func(tx *sql.Tx) error { assertTagCounts(ctx, t, tx); return nil }))
			}
		}()
	}
	check(t, st.read(ctx, func(tx *sql.Tx) error {
		var old int
		if err := tx.QueryRowContext(ctx, "SELECT n FROM tag_counts WHERE tag_id = 1").Scan(&old); err != nil {
			return err
		}
		go func() {
			close(started)
			for i := range 120 {
				err := st.tx(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "UPDATE assets SET hidden = 1-hidden, thumb_version = thumb_version+1 WHERE id = ?", 1+i%16)
					return err
				})
				if err != nil {
					finished <- err
					return
				}
			}
			finished <- nil
		}()
		if err := <-finished; err != nil {
			return err
		}
		assertTagCounts(ctx, t, tx)
		var still int
		if err := tx.QueryRowContext(ctx, "SELECT n FROM tag_counts WHERE tag_id = 1").Scan(&still); err != nil {
			return err
		}
		if old != still {
			t.Fatalf("snapshot count moved from %d to %d", old, still)
		}
		return nil
	}))
	readers.Wait()
}

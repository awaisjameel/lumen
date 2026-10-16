package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mattn/go-sqlite3"

	"lumen/internal/core"
)

func dayBucketsReference(ctx context.Context, db querier, filter core.Filter) ([]core.DayBucket, error) {
	f := assetFilter{favourites: filter == core.FilterFavourites}
	switch filter {
	case core.FilterVideos:
		f.kind = core.KindVideo
	case core.FilterPhotos:
		f.kind = core.KindPhoto
	default:
	}
	where, args := f.where(core.TagConfidenceDefault)
	return queryAll(ctx, db, func(r *sql.Rows) (b core.DayBucket, err error) {
		err = r.Scan(&b.Day, &b.Count)
		return b, err
	}, `SELECT strftime('%Y-%m-%d', taken_at, 'unixepoch') AS day, count(*)
		FROM assets WHERE `+where+` GROUP BY day ORDER BY day DESC`, args...)
}

func compareDayCounts(ctx context.Context, t *testing.T, db querier) {
	t.Helper()
	for _, filter := range []core.Filter{core.FilterAll, core.FilterPhotos, core.FilterFavourites, core.FilterVideos} {
		got, err := keptDayBuckets(ctx, db, filter)
		check(t, err)
		want, err := dayBucketsReference(ctx, db, filter)
		check(t, err)
		if !slices.Equal(got, want) {
			t.Fatalf("filter %q: kept %v, counted %v", filter, got, want)
		}
	}
	got, err := keptVisibleCount(ctx, db)
	check(t, err)
	var want, empty int
	check(t, db.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+visible).Scan(&want))
	check(t, db.QueryRowContext(ctx, "SELECT count(*) FROM day_counts WHERE total = 0").Scan(&empty))
	if got != want || empty != 0 {
		t.Fatalf("kept total %d, counted %d, empty days %d", got, want, empty)
	}
}

func TestDayCountsEqualTheReferenceAfterEveryRandomisedChange(t *testing.T) {
	for _, limit := range []int64{keptCountsThreshold, 1} {
		for _, seed := range []uint64{1, 42, 20261014} {
			t.Run(fmt.Sprintf("limit=%d/seed=%d", limit, seed), func(t *testing.T) {
				st := openTest(t)
				ctx := context.WithValue(t.Context(), keptCountsThresholdKey{}, limit)
				seedBlockedOracle(ctx, t, st)
				rng := rand.New(rand.NewPCG(seed, seed+1))
				changes := keptCountChanges()
				changes = append(changes,
					blockedOracleChange{"restore", "UPDATE assets SET hidden = 0, missing_since = NULL, trashed_at = NULL WHERE id = ?", func(r *rand.Rand) []any { return []any{2 + r.IntN(14)} }},
					blockedOracleChange{"cross midnight", "UPDATE assets SET taken_at = ? WHERE id = ?", func(r *rand.Rand) []any {
						return []any{[]int64{-86401, -86400, -1, 0, 86399, 86400, 951782400, 1700000000}[r.IntN(8)], 2 + r.IntN(14)}
					}},
				)
				t.Logf("checking %d mutations across %d input operations", 24*len(changes), len(changes))
				for round := range 24 {
					for _, index := range rng.Perm(len(changes)) {
						change := changes[index]
						var args []any
						if change.args != nil {
							args = change.args(rng)
						}
						errRollback := errKeptCountRollback
						rollback := round%4 == 3 && index%7 == 0
						err := applyKeptCountChange(ctx, t, st, change.sql, args, rollback)
						if err != nil && (!rollback || !errors.Is(err, errRollback)) {
							t.Fatalf("seed %d round %d %s: %v", seed, round, change.name, err)
						}
						check(t, st.read(ctx, func(tx *sql.Tx) error {
							compareDayCounts(ctx, t, tx)
							return nil
						}))
					}
				}
			})
		}
	}
}

func TestDayCountsSubtractDeletedAssetsExactlyOnceAcrossCascades(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			seedBlockedOracle(ctx, t, st)
			writeInTx(t, st, `UPDATE assets SET favourite = 1, kind = 'video', taken_at = 0;
				UPDATE files SET path = 'C:\Photos\Public'||id||'.jpg' WHERE id % 2 = 1;
				INSERT INTO asset_tags(asset_id, tag_id, source) SELECT id, 1, 'user' FROM assets`)
			if blocked {
				check(t, errOf(st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)))
			}
			writeInTx(t, st, "DELETE FROM assets WHERE id % 2 = 0")
			compareDayCounts(ctx, t, st.r)
			writeInTx(t, st, "DELETE FROM assets")
			compareDayCounts(ctx, t, st.r)
		})
	}
}

func TestDayBucketsUseKeptCountsOnlyForTheThreeWholeTimelineFilters(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	writeInTx(t, st, `UPDATE assets SET favourite = id % 2, kind = CASE WHEN id % 3 = 0 THEN 'video' ELSE 'photo' END;
		INSERT INTO asset_tags(asset_id, tag_id, source) SELECT id, 1, 'user' FROM assets WHERE id % 2 = 0;
		INSERT INTO album_items(album_id, asset_id, added_at) SELECT 1, id, 0 FROM assets WHERE id % 3 = 0;
		UPDATE albums SET rule_json = '{"favourites":true}' WHERE id = 2;
		UPDATE assets SET trashed_at = 1 WHERE id = 5`)
	for _, filter := range []core.Filter{core.FilterAll, core.FilterFavourites, core.FilterVideos, core.FilterTrash, "unknown"} {
		for _, target := range []core.View{{}, {Tag: 1}, {Person: 1}, {Album: 1}, {Album: 2}, {Place: 1}, {Event: 1}} {
			view := target
			view.Filter = filter
			got, err := st.DayBuckets(ctx, view, core.TagConfidenceDefault)
			want, wantErr := st.dayBucketsCounted(ctx, view, core.TagConfidenceDefault)
			if !errors.Is(err, wantErr) || !slices.Equal(got, want) {
				t.Fatalf("view %+v: kept %v (%v), counted %v (%v)", view, got, err, want, wantErr)
			}
		}
	}
	for _, kind := range []core.BlockKind{core.BlockTag, core.BlockPerson, core.BlockAlbum, core.BlockPlace, core.BlockEvent} {
		check(t, errUnlessNotFound(st.SetBlock(ctx, kind, 1, "", core.BlockLabel)))
	}
	for _, view := range []core.View{{Tag: 1}, {Person: 1}, {Album: 1}, {Place: 1}, {Event: 1}} {
		_, err := st.DayBuckets(ctx, view, core.TagConfidenceDefault)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("blocked view %+v: %v", view, err)
		}
	}
	got, err := st.CountVisible(ctx)
	check(t, err)
	want, err := keptVisibleCount(ctx, st.r)
	check(t, err)
	if got != want {
		t.Fatalf("public total %d, kept total %d", got, want)
	}
}

func TestDayCountsDoNoWorkWhenTheContributionDoesNotChange(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	writeInTx(t, st, "UPDATE assets SET hidden = 1 WHERE id = 2")
	check(t, errOf(st.SetBlock(ctx, core.BlockAsset, 3, "", core.BlockPhotos)))
	writeInTx(t, st, `CREATE TRIGGER refuse_day_counts_update BEFORE UPDATE ON day_counts BEGIN SELECT RAISE(ABORT, 'unexpected count update'); END;
		CREATE TRIGGER refuse_day_counts_insert BEFORE INSERT ON day_counts BEGIN SELECT RAISE(ABORT, 'unexpected count insert'); END;
		CREATE TRIGGER refuse_day_counts_delete BEFORE DELETE ON day_counts BEGIN SELECT RAISE(ABORT, 'unexpected count delete'); END`)
	writeInTx(t, st, `UPDATE assets SET hidden = hidden, trashed_at = trashed_at, missing_since = missing_since,
		taken_at = taken_at + 1, kind = kind, favourite = favourite, thumb_version = thumb_version + 1;
		UPDATE assets SET width = 123, height = 456;
		UPDATE assets SET taken_at = taken_at + 86400, favourite = 1, kind = 'video' WHERE id IN (2, 3)`)
	compareDayCounts(ctx, t, st.r)
}

func TestDayCountsPreserveOutOfRangeDatesInTheTotalAndTheExistingBucketError(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	writeInTx(t, st, `INSERT INTO assets(content_hash, kind, ext, taken_at, added_at, favourite)
		VALUES ('invalid', 'video', '.mp4', 9223372036854775807, 0, 1)`)
	for _, filter := range []core.Filter{core.FilterAll, core.FilterFavourites, core.FilterVideos} {
		_, err := st.DayBuckets(ctx, core.View{Filter: filter}, core.TagConfidenceDefault)
		_, wantErr := dayBucketsReference(ctx, st.r, filter)
		if err == nil || wantErr == nil || err.Error() != wantErr.Error() {
			t.Fatalf("invalid day: kept error %v, reference error %v", err, wantErr)
		}
	}
	n, err := st.CountVisible(ctx)
	check(t, err)
	if n != 1 {
		t.Fatalf("out-of-range date vanished from the total: %d", n)
	}
}

func TestDayCountsMatchEachReadersSnapshotAlongsideAWriter(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	var readers sync.WaitGroup
	var snapshots atomic.Int64
	done := make(chan struct{})
	for range 3 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				check(t, st.read(ctx, func(tx *sql.Tx) error {
					compareDayCounts(ctx, t, tx)
					snapshots.Add(1)
					return nil
				}))
			}
		})
	}
	func() {
		defer func() { close(done); readers.Wait() }()
		for step := range 300 {

			writeInTx(t, st, `UPDATE assets SET taken_at = taken_at + 86400, favourite = 1-favourite,
				kind = CASE kind WHEN 'photo' THEN 'video' ELSE 'photo' END, hidden = 1-hidden WHERE id = ?`, 1+step%16)
			if step%2 == 0 {
				check(t, errOf(st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)))
			} else {
				writeInTx(t, st, "DELETE FROM blocks")
			}
		}
	}()
	if snapshots.Load() == 0 {
		t.Fatal("readers did not compare a snapshot")
	}
}

func TestTimelineCountReadersNeverReadAssets(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedBlockedOracle(ctx, t, st)
	writeInTx(t, st, "UPDATE assets SET favourite = 1, kind = 'video'")
	st.r.SetMaxOpenConns(1)
	conn, err := st.r.Conn(ctx)
	check(t, err)
	check(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(operation int, table, _, _ string) int {
			if operation == sqlite3.SQLITE_READ && table != "day_counts" {
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	check(t, conn.Close())
	for _, filter := range []core.Filter{core.FilterAll, core.FilterPhotos, core.FilterFavourites, core.FilterVideos} {
		buckets, err := st.DayBuckets(ctx, core.View{Filter: filter}, core.TagConfidenceDefault)
		check(t, err)
		want := 16
		if filter == core.FilterPhotos {
			want = 0
		}
		if len(buckets) != want {
			t.Fatalf("filter %q: got %d days, want %d", filter, len(buckets), want)
		}
	}
	n, err := st.CountVisible(ctx)
	check(t, err)
	if n != 16 {
		t.Fatalf("got %d visible assets, want 16", n)
	}
}

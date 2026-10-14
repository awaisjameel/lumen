package store

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"testing"
)

func TestClusterChangesReconstructTheFullSnapshot(t *testing.T) {
	st, ctx, a, b, c := peopleFixture(t)
	var cursor ClusterCursor
	kept := map[int64]ClusterFace{}
	checkSnapshot := func(wantFull bool) ClusterSnapshot {
		t.Helper()
		snapshot, err := st.ClusterSince(ctx, "f1", cursor)
		check(t, err)
		if snapshot.Full != wantFull {
			t.Fatalf("full = %v, want %v", snapshot.Full, wantFull)
		}
		if snapshot.Full {
			kept = map[int64]ClusterFace{}
		}
		for _, id := range snapshot.Removed {
			delete(kept, id)
		}
		for _, f := range snapshot.Faces {
			kept[f.ID] = f
		}
		var got []ClusterFace
		for _, f := range kept {
			got = append(got, f)
		}
		slices.SortFunc(got, func(a, b ClusterFace) int {
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			return 0
		})
		faces, people, rejected, err := st.ClusterData(ctx, "f1")
		check(t, err)
		if !reflect.DeepEqual(got, faces) || !reflect.DeepEqual(snapshot.People, people) || !reflect.DeepEqual(snapshot.Rejections, rejected) {
			t.Fatalf("delta does not reconstruct the reference: %+v", snapshot)
		}
		cursor = snapshot.Cursor
		return snapshot
	}
	checkSnapshot(true)
	if got := checkSnapshot(false); len(got.Faces) != 0 {
		t.Fatal("unchanged vectors were loaded")
	}
	check(t, st.SaveFaces(ctx, "f1", "stage2", []FaceResult{{AssetID: a, Unchanged: true}}))
	if got := checkSnapshot(false); len(got.Faces) != 0 {
		t.Fatal("a version-only face save reloaded vectors")
	}
	fs := facesOf(t, st, a)
	person, err := st.NameFaces(ctx, []int64{fs[0].ID}, "Ali")
	check(t, err)
	if got := checkSnapshot(false); len(got.Faces) != 1 || got.Faces[0].ID != fs[0].ID {
		t.Fatalf("placement delta = %+v", got)
	}
	check(t, st.RejectFaces(ctx, person, []int64{fs[0].ID}))
	checkSnapshot(false)
	check(t, st.SaveFaces(ctx, "f1", "stage2", []FaceResult{{AssetID: a, Faces: []DetectedFace{face(0.1, 3), face(0.6, 1)}}}))
	checkSnapshot(false)
	check(t, st.TrashAssets(ctx, []int64{b}))
	if got := checkSnapshot(false); len(got.Removed) != 1 {
		t.Fatalf("trashed delta = %+v", got)
	}
	check(t, st.RestoreAssets(ctx, []int64{b}))
	checkSnapshot(false)
	check(t, st.SaveFaces(ctx, "f2", "stage3", []FaceResult{{AssetID: c, Faces: []DetectedFace{face(0.3, 2)}}}))
	if got := checkSnapshot(false); len(got.Removed) != 1 {
		t.Fatalf("model replacement delta = %+v", got)
	}
	check(t, st.SaveFaces(ctx, "f1", "stage4", []FaceResult{{AssetID: c, Faces: []DetectedFace{face(0.3, 2)}}}))
	checkSnapshot(false)
	for _, q := range []string{
		"UPDATE assets SET hidden = 1 WHERE id = ?",
		"UPDATE assets SET hidden = 0 WHERE id = ?",
		"UPDATE assets SET missing_since = 1 WHERE id = ?",
		"UPDATE assets SET missing_since = NULL WHERE id = ?",
		"DELETE FROM assets WHERE id = ?",
	} {
		check(t, st.tx(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, q, b); return err }))
		checkSnapshot(false)
	}
	// Changes to one face coalesce rather than growing with the number of runs.
	var n int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM cluster_faces").Scan(&n))
	if n > 4 {
		t.Fatalf("journal grew beyond the fixture's faces: %d", n)
	}
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE cluster_revision SET value = value + 100001")
		return err
	}))
	checkSnapshot(true)
	snapshot, err := st.ClusterSince(ctx, "f2", cursor)
	check(t, err)
	if !snapshot.Full {
		t.Fatal("a different model reused a prior model's state")
	}
	cursor.Revision = snapshot.Cursor.Revision + 1
	snapshot, err = st.ClusterSince(ctx, "f1", cursor)
	check(t, err)
	if !snapshot.Full {
		t.Fatal("a future cursor was accepted")
	}
}

func TestClusterJournalRollsBackWithTheFaceWrite(t *testing.T) {
	st, ctx, _, _, _ := peopleFixture(t)
	before, err := st.ClusterSince(ctx, "f1", ClusterCursor{})
	check(t, err)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := st.SaveFaces(cancelled, "f1", "v2", []FaceResult{{AssetID: before.Faces[0].AssetID}}); err == nil {
		t.Fatal("cancelled face write succeeded")
	}
	after, err := st.ClusterSince(ctx, "f1", before.Cursor)
	check(t, err)
	if after.Cursor != before.Cursor || len(after.Faces) != 0 || len(after.Removed) != 0 {
		t.Fatalf("cancelled write leaked into journal: %+v", after)
	}
}

func TestClusterJournalFollowsForeignKeyPlacements(t *testing.T) {
	st, ctx, a, _, _ := peopleFixture(t)
	f := facesOf(t, st, a)[0]
	person, err := st.NameFaces(ctx, []int64{f.ID}, "Ali")
	check(t, err)
	before, err := st.ClusterSince(ctx, "f1", ClusterCursor{})
	check(t, err)
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM people WHERE id = ?", person)
		return err
	}))
	after, err := st.ClusterSince(ctx, "f1", before.Cursor)
	check(t, err)
	if after.Full || len(after.Faces) != 1 || after.Faces[0].ID != f.ID || after.Faces[0].Person != 0 {
		t.Fatalf("foreign-key placement delta = %+v", after)
	}
}

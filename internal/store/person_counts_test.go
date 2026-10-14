package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"lumen/internal/core"
)

func peopleCountedReference(ctx context.Context, tx *sql.Tx) ([]core.Person, error) {
	people, err := queryAll(ctx, tx, scanPerson, `SELECT p.id, COALESCE(p.name, ''), count(DISTINCT f.asset_id)
		FROM people p JOIN faces f ON f.person_id = p.id JOIN assets a ON a.id = f.asset_id AND `+visibleA+`
		WHERE NOT `+labelBlocked(core.BlockPerson, "p.id")+` AND `+faceShown+`
		GROUP BY p.id ORDER BY p.name IS NULL, count(DISTINCT f.asset_id) DESC, `+byName("p.name")+`, p.id`)
	if err != nil {
		return nil, err
	}
	return people, addCovers(ctx, tx, people)
}

func comparePersonCounts(ctx context.Context, tx *sql.Tx) error {
	want, err := peopleCountedReference(ctx, tx)
	if err != nil {
		return err
	}
	got, err := peopleKept(ctx, tx)
	if err != nil {
		return err
	}
	if err := addKeptCovers(ctx, tx, got); err != nil {
		return err
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("kept people %+v, reference %+v", got, want)
	}
	// Include zero-count and label-blocked people in the raw count comparison.
	var wrong int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM people p LEFT JOIN person_counts c ON c.person_id = p.id
		WHERE c.count IS NULL OR c.count <> (SELECT count(DISTINCT f.asset_id)
		FROM faces f JOIN assets a ON a.id = f.asset_id WHERE f.person_id = p.id AND `+visibleA+` AND `+faceShown+`)`).Scan(&wrong)
	if err != nil {
		return err
	}
	if wrong != 0 {
		return fmt.Errorf("%d maintained person counts disagree with the reference", wrong)
	}
	return nil
}

func seedPersonCounts(t *testing.T, st *Store) {
	t.Helper()
	seedBlockedOracle(t.Context(), t, st)
	writeInTx(t, st, `UPDATE assets SET width = 100 + id, height = 200, thumb_version = 1;
		UPDATE faces SET w = 0.1 + id * 0.01, score = 0.8;
		INSERT INTO faces(id, asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id)
		SELECT 100 + id, asset_id, 0.4, 0.2, 0.3, 0.2, 0.9, 20, 0, 'test', zeroblob(512), person_id FROM faces;
		INSERT INTO people(id, name, created_at) VALUES (3, NULL, 0), (4, 'Empty', 0);
		UPDATE faces SET person_id = 3 WHERE id IN (103, 104)`)
}

func personCountChanges() []blockedOracleChange {
	asset := func(r *rand.Rand) []any { return []any{2 + r.IntN(14)} }
	changes := keptCountChanges()
	return append(changes,
		blockedOracleChange{"thumbnail", `UPDATE assets SET thumb_version = 1-thumb_version WHERE id = ?`, asset},
		blockedOracleChange{"dimensions", `UPDATE assets SET width = CASE WHEN width IS NULL THEN 100 ELSE NULL END, height = height + 10 WHERE id = ?`, asset},
		blockedOracleChange{"box and score", `UPDATE faces SET x = 1-x, y = 1-y, w = 0.5-w, h = 0.4-h, score = 1.7-score WHERE id = ?`, asset},
		blockedOracleChange{"frame and assignment", `UPDATE faces SET at_ms = 1000-at_ms, assigned_by = CASE assigned_by WHEN 'user' THEN 'auto' ELSE 'user' END WHERE id = ?`, asset},
		blockedOracleChange{"cover pick", `UPDATE people SET cover_face = (SELECT id FROM faces WHERE person_id = people.id ORDER BY id DESC LIMIT 1) WHERE id = 1`, nil},
		blockedOracleChange{"cover clear", `UPDATE people SET cover_face = NULL WHERE id = 1`, nil},
		blockedOracleChange{"person delete", `DELETE FROM people WHERE id = 3`, nil},
		blockedOracleChange{"person restore", `INSERT OR IGNORE INTO people(id, created_at) VALUES (3, 0)`, nil},
		blockedOracleChange{"unassign", `UPDATE faces SET person_id = NULL WHERE id = ?`, asset},
		blockedOracleChange{"restore visibility", `UPDATE assets SET hidden = 0, trashed_at = NULL, missing_since = NULL WHERE id = ?`, asset},
		blockedOracleChange{"last duplicate removed", `DELETE FROM faces WHERE id = 100 + ?`, asset},
		blockedOracleChange{"duplicate restored", `INSERT OR IGNORE INTO faces(id, asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id)
			SELECT 100+id, id, 0.4, 0.2, 0.3, 0.2, 0.9, 20, 0, 'test', zeroblob(512), 1 FROM assets WHERE id = ?`, asset},
		blockedOracleChange{"face identity", `UPDATE OR IGNORE faces SET id = CASE WHEN id > 1000 THEN id-1000 ELSE id+1000 END
			WHERE id > 100 AND asset_id = ? AND NOT EXISTS (SELECT 1 FROM people WHERE cover_face = faces.id)
			AND NOT EXISTS (SELECT 1 FROM face_block_faces WHERE face_id = faces.id)
			AND NOT EXISTS (SELECT 1 FROM face_blocks WHERE face_id = faces.id)`, asset},
		blockedOracleChange{"overlapping face blocks", `INSERT OR IGNORE INTO face_block_faces(face_block_id, face_id, asset_id)
			SELECT b.id, f.id, f.asset_id FROM faces f CROSS JOIN face_blocks b WHERE f.id = ?`, asset},
		blockedOracleChange{"rename", `UPDATE people SET name = CASE WHEN name IS NULL THEN 'Named' ELSE NULL END WHERE id = 3`, nil},
	)
}

func TestPersonCountsAndCoversEqualTheReferenceAfterEveryRandomisedChange(t *testing.T) {
	for _, limit := range []int64{keptCountsThreshold, 1} {
		for _, seed := range []uint64{1, 42, 20261014} {
			t.Run(fmt.Sprintf("limit=%d/seed=%d", limit, seed), func(t *testing.T) {
				st := openTest(t)
				ctx := context.WithValue(t.Context(), keptCountsThresholdKey{}, limit)
				seedPersonCounts(t, st)
				rng := rand.New(rand.NewPCG(seed, seed+1))
				changes := personCountChanges()
				t.Logf("checking %d steps across %d inputs", 16*len(changes), len(changes))
				for round := range 16 {
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
						check(t, st.read(ctx, func(tx *sql.Tx) error { return comparePersonCounts(ctx, tx) }))
					}
				}
			})
		}
	}
}

func TestPersonCountsAndCoversShareTheReadersSnapshotDuringWrites(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedPersonCounts(t, st)
	var readers sync.WaitGroup
	started := make(chan struct{}, 3)
	for range 3 {
		readers.Go(func() {
			started <- struct{}{}
			for range 200 {
				err := st.read(ctx, func(tx *sql.Tx) error { return comparePersonCounts(ctx, tx) })
				if err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	for range 3 {
		<-started
	}
	for i := range 200 {
		err := st.tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE assets SET hidden = 1-hidden, width = width + 1 WHERE id = ?;
				UPDATE faces SET person_id = 3-person_id WHERE id = ?`, 2+i%14, 2+i%14)
			return err
		})
		if err != nil {
			t.Error(err)
			break
		}
	}
	readers.Wait()
}

func TestPersonCountsFollowManualPlacementClusteringMergesRejectionsAndReanalysis(t *testing.T) {
	st, ctx, a, b, c := peopleFixture(t)
	verify := func() {
		t.Helper()
		check(t, st.read(ctx, func(tx *sql.Tx) error { return comparePersonCounts(ctx, tx) }))
	}
	fa, fb, fc := facesOf(t, st, a), facesOf(t, st, b), facesOf(t, st, c)
	person, err := st.NameFaces(ctx, []int64{fa[0].ID, fa[1].ID}, "Ali")
	check(t, err)
	verify()
	check(t, st.ApplyClusters(ctx, ClusterChanges{Assign: map[int64]int64{fb[0].ID: person}}))
	verify()
	check(t, st.ApplyClusters(ctx, ClusterChanges{NewPeople: [][]int64{{fc[0].ID}}}))
	verify()
	other := facesOf(t, st, c)[0].Person
	check(t, st.ApplyClusters(ctx, ClusterChanges{Merges: []Merge{{From: other, Into: person}}}))
	verify()
	other, err = st.NameFaces(ctx, []int64{fb[0].ID}, "Bob")
	check(t, err)
	verify()
	check(t, st.SetPersonCover(ctx, other, fb[0].ID))
	verify()
	check(t, st.MergePeople(ctx, other, person))
	verify()
	check(t, st.RejectFaces(ctx, person, []int64{fa[0].ID}))
	verify()
	check(t, st.ApplyClusters(ctx, ClusterChanges{Assign: map[int64]int64{fa[0].ID: person}}))
	verify()
	check(t, st.SaveFaceVersions(ctx, "f2", StageVersion{Photo: "v2", Video: "v2"}, []FaceResult{
		{AssetID: a, Faces: []DetectedFace{face(0.61, 1)}}, {AssetID: b},
	}))
	verify()
	check(t, st.SaveFaceVersions(ctx, "f2", StageVersion{Photo: "v3", Video: "v3"}, []FaceResult{{AssetID: a, Unchanged: true}}))
	verify()
	writeInTx(t, st, `INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at)
		VALUES (100, 'copy', 'photo', '.jpg', 0, 0)`)
	check(t, st.tx(ctx, func(tx *sql.Tx) error { return transferCompressionAnnotations(ctx, tx, a, 100) }))
	verify()
	writeInTx(t, st, "UPDATE assets SET thumb_version = 1, width = 1000, height = 1000 WHERE id = 100")
	verify()
}

func TestPersonCountsSubtractEachPhotoOnceThroughCascadesAndOverlappingFaceBlocks(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedPersonCounts(t, st)
	for _, q := range []string{
		"INSERT INTO face_block_faces SELECT id, 2, 2 FROM face_blocks",
		"DELETE FROM face_block_faces WHERE face_block_id = 1",
		"DELETE FROM faces WHERE id = 102",
		"DELETE FROM face_blocks WHERE id = 2",
		"DELETE FROM faces WHERE id = 2",
		"INSERT INTO blocks(kind, target_id, mode, name, created_at) VALUES ('asset', 4, 'photos', 'Hidden', 0)",
		"DELETE FROM assets WHERE id IN (1, 3, 4)",
		"DELETE FROM people WHERE id = 1",
	} {
		writeInTx(t, st, q)
		check(t, st.read(ctx, func(tx *sql.Tx) error { return comparePersonCounts(ctx, tx) }))
	}
}

func TestUnchangedPersonCountInputsDoNotRewriteMaintainedRows(t *testing.T) {
	st := openTest(t)
	seedPersonCounts(t, st)
	writeInTx(t, st, `CREATE TRIGGER refuse_person_count_insert BEFORE INSERT ON person_counts_faces BEGIN SELECT RAISE(ABORT, 'unexpected insert'); END;
		CREATE TRIGGER refuse_person_count_update BEFORE UPDATE ON person_counts_faces BEGIN SELECT RAISE(ABORT, 'unexpected update'); END;
		CREATE TRIGGER refuse_person_count_delete BEFORE DELETE ON person_counts_faces BEGIN SELECT RAISE(ABORT, 'unexpected delete'); END;
		UPDATE assets SET hidden = hidden, trashed_at = trashed_at, missing_since = missing_since,
			thumb_version = thumb_version + 1, width = width, height = height, taken_at = taken_at + 1;
		UPDATE faces SET id = id, person_id = person_id, asset_id = asset_id, w = w, h = h, score = score, x = 0.3, y = 0.2;
		UPDATE face_block_faces SET face_id = face_id`)
	check(t, st.read(t.Context(), func(tx *sql.Tx) error { return comparePersonCounts(t.Context(), tx) }))
}

func TestPersonReadersUseMaintainedCountsAndRankedCovers(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedPersonCounts(t, st)
	// Distinguish a maintained read from a recount without timing the machine.
	writeInTx(t, st, `UPDATE person_counts SET count = 123 WHERE person_id = 1;
		UPDATE person_counts_faces SET cover_weight = 1000000000 WHERE face_id = 1`)
	people, err := st.People(ctx)
	check(t, err)
	person, err := st.Person(ctx, 1)
	check(t, err)
	if len(people) == 0 || people[0].ID != 1 || people[0].Count != 123 || people[0].Cover.ID != 1 || person.Count != 123 || person.Cover.ID != 1 {
		t.Fatalf("readers did not use maintained answers: list %+v, person %+v", people, person)
	}
}

// addCovers picks each person's clearest face with a thumbnail to show.
func addCovers(ctx context.Context, tx *sql.Tx, people []core.Person) error {
	st, err := tx.PrepareContext(ctx, faceSelect+` WHERE f.person_id = ? AND `+visibleA+` AND `+faceShown+` AND a.thumb_version > 0
		ORDER BY `+clearest+` LIMIT 1`)
	if err != nil {
		return err
	}
	defer st.Close()
	for i := range people {
		rows, err := st.QueryContext(ctx, people[i].ID)
		covers, err := collect(rows, err, scanFace)
		if err != nil {
			return err
		}
		if len(covers) > 0 {
			people[i].Cover = covers[0]
		}
	}
	return nil
}

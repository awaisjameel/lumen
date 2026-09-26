package store

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"time"

	"lumen/internal/core"
)

// DetectedFace is one face the faces stage found.
type DetectedFace struct {
	X, Y, W, H float32 // box, as fractions of the upright photo
	Score      float32 // detection confidence
	Sharpness  float32 // detail in the aligned face (ai.Identity)
	Yaw        float32 // head turn (ai.Face.Yaw)
	Vec        []float32
}

// FaceResult is the outcome of the faces stage for one asset.
type FaceResult struct {
	AssetID int64
	Faces   []DetectedFace
	Err     string
	Skipped bool // nothing to look at (a video, or an undecodable format)
}

// SaveFaces stores a batch of face results and closes their jobs. A photo
// seen again (by a newer model) keeps who is in it: each new face takes over
// the old face at the same spot, with its person and the user's corrections.
func (s *Store) SaveFaces(ctx context.Context, model string, rs []FaceResult) error {
	if len(rs) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		old, err := tx.PrepareContext(ctx, "SELECT id, x, y, w, h FROM faces WHERE asset_id = ?")
		if err != nil {
			return err
		}
		defer old.Close()
		upd, err := tx.PrepareContext(ctx, `UPDATE faces SET x = ?, y = ?, w = ?, h = ?, score = ?, sharpness = ?, yaw = ?,
			model = ?, vec = ? WHERE id = ?`)
		if err != nil {
			return err
		}
		defer upd.Close()
		ins, err := tx.PrepareContext(ctx, `INSERT INTO faces (asset_id, x, y, w, h, score, sharpness, yaw, model, vec)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer ins.Close()
		del, err := tx.PrepareContext(ctx, "DELETE FROM faces WHERE id = ?")
		if err != nil {
			return err
		}
		defer del.Close()
		job, err := prepareJobUpdate(ctx, tx)
		if err != nil {
			return err
		}
		defer job.Close()

		for _, r := range rs {
			if r.Err == "" && !r.Skipped {
				rows, err := old.QueryContext(ctx, r.AssetID)
				before, err := collect(rows, err, func(rows *sql.Rows) (f storedBox, err error) {
					return f, rows.Scan(&f.id, &f.X, &f.Y, &f.W, &f.H)
				})
				if err != nil {
					return err
				}
				took := matchFaces(before, r.Faces)
				for i, f := range r.Faces {
					var err error
					if id := took[i]; id != 0 {
						_, err = upd.ExecContext(ctx, f.X, f.Y, f.W, f.H, f.Score, f.Sharpness, f.Yaw, model, float32Bytes(f.Vec), id)
					} else {
						_, err = ins.ExecContext(ctx, r.AssetID, f.X, f.Y, f.W, f.H, f.Score, f.Sharpness, f.Yaw, model, float32Bytes(f.Vec))
					}
					if err != nil {
						return err
					}
				}
				for _, b := range before {
					if !slices.Contains(took, b.id) {
						if _, err := del.ExecContext(ctx, b.id); err != nil {
							return err
						}
					}
				}
			}
			if err := execJob(ctx, job, r.AssetID, core.StageFaces, model, jobStatus(r.Err, r.Skipped), r.Err, now); err != nil {
				return err
			}
		}
		return dropEmptyPeople(ctx, tx)
	})
}

// box is a face box as fractions of the photo.
type box struct{ X, Y, W, H float32 }

type storedBox struct {
	id int64
	box
}

// matchFaces pairs new faces with stored ones at the same spot (boxes
// overlapping by at least half), best overlaps first. took[i] is the stored
// face new face i replaces, or 0.
func matchFaces(before []storedBox, now []DetectedFace) []int64 {
	type pair struct {
		i   int
		id  int64
		iou float32
	}
	var pairs []pair
	for i, f := range now {
		for _, b := range before {
			if v := overlap(b.box, box{f.X, f.Y, f.W, f.H}); v >= 0.5 {
				pairs = append(pairs, pair{i, b.id, v})
			}
		}
	}
	slices.SortFunc(pairs, func(a, b pair) int { return cmp.Compare(b.iou, a.iou) })
	took := make([]int64, len(now))
	for _, p := range pairs {
		if took[p.i] == 0 && !slices.Contains(took, p.id) {
			took[p.i] = p.id
		}
	}
	return took
}

// overlap is the intersection over union of two boxes.
func overlap(a, b box) float32 {
	iw := min(a.X+a.W, b.X+b.W) - max(a.X, b.X)
	ih := min(a.Y+a.H, b.Y+b.H) - max(a.Y, b.Y)
	if iw <= 0 || ih <= 0 {
		return 0
	}
	inter := iw * ih
	return inter / (a.W*a.H + b.W*b.H - inter)
}

// ClusterFace is a face as clustering sees it.
type ClusterFace struct {
	ID, AssetID int64
	Person      int64   // 0 = nobody yet
	ByUser      bool    // the user put it with Person: it never moves
	Sharpness   float32 // see DetectedFace
	Yaw         float32
	Vec         []float32
}

// ClusterPerson is a person as clustering sees them.
type ClusterPerson struct {
	ID    int64
	Named bool // the user named them: never merged into anyone automatically
}

// Rejection is the user saying a face is not a person.
type Rejection struct{ Face, Person int64 }

// ClusterData returns what clustering works from: the faces made by model in
// visible photos, every person, and the user's rejections.
func (s *Store) ClusterData(ctx context.Context, model string) ([]ClusterFace, []ClusterPerson, []Rejection, error) {
	faces, err := queryAll(ctx, s.r, func(r *sql.Rows) (f ClusterFace, err error) {
		var vec []byte
		err = r.Scan(&f.ID, &f.AssetID, &f.Person, &f.ByUser, &f.Sharpness, &f.Yaw, &vec)
		f.Vec = bytesFloat32(vec)
		return f, err
	}, `SELECT f.id, f.asset_id, COALESCE(f.person_id, 0), COALESCE(f.assigned_by = 'user', 0), f.sharpness, f.yaw, f.vec
		FROM faces f JOIN assets a ON a.id = f.asset_id
		WHERE f.model = ? AND `+visibleAs("a")+` ORDER BY f.id`, model)
	if err != nil {
		return nil, nil, nil, err
	}
	people, err := queryAll(ctx, s.r, func(r *sql.Rows) (p ClusterPerson, err error) {
		return p, r.Scan(&p.ID, &p.Named)
	}, "SELECT id, name IS NOT NULL FROM people ORDER BY id")
	if err != nil {
		return nil, nil, nil, err
	}
	rejections, err := queryAll(ctx, s.r, func(r *sql.Rows) (x Rejection, err error) {
		return x, r.Scan(&x.Face, &x.Person)
	}, "SELECT face_id, person_id FROM face_rejections")
	return faces, people, rejections, err
}

// ClusterChanges is what one clustering run decided.
type ClusterChanges struct {
	Assign    map[int64]int64 // face -> the existing person it joins
	NewPeople [][]int64       // the faces of each new, unnamed person
	Merges    []Merge         // unnamed people found to be someone else
}

// Merge folds person From into person Into.
type Merge struct{ From, Into int64 }

// ApplyClusters saves a clustering run in one transaction. The run worked
// from a snapshot, so anything the user changed since wins: only faces still
// unassigned are placed, and only unnamed people without user-placed faces
// are merged away.
func (s *Store) ApplyClusters(ctx context.Context, c ClusterChanges) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		assign, err := tx.PrepareContext(ctx, `UPDATE faces SET person_id = ?1, assigned_by = 'auto'
			WHERE id = ?2 AND person_id IS NULL AND EXISTS (SELECT 1 FROM people WHERE id = ?1)`)
		if err != nil {
			return err
		}
		defer assign.Close()
		for face, person := range c.Assign {
			if _, err := assign.ExecContext(ctx, person, face); err != nil {
				return err
			}
		}
		now := time.Now().Unix()
		for _, faces := range c.NewPeople {
			res, err := tx.ExecContext(ctx, "INSERT INTO people (created_at) VALUES (?)", now)
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			for _, face := range faces {
				if _, err := assign.ExecContext(ctx, id, face); err != nil {
					return err
				}
			}
		}
		for _, m := range c.Merges {
			var ok bool
			err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM people WHERE id = ?1 AND name IS NULL
					AND NOT EXISTS (SELECT 1 FROM faces WHERE person_id = ?1 AND assigned_by = 'user'))
				AND EXISTS (SELECT 1 FROM people WHERE id = ?2)`, m.From, m.Into).Scan(&ok)
			if err != nil {
				return err
			}
			if ok {
				if err := mergePeople(ctx, tx, m.From, m.Into); err != nil {
					return err
				}
			}
		}
		return dropEmptyPeople(ctx, tx)
	})
}

// mergePeople moves everything of person from to person into and deletes
// from. into keeps its name, or takes from's if it has none.
func mergePeople(ctx context.Context, tx *sql.Tx, from, into int64) error {
	var name sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT name FROM people WHERE id = ?", from).Scan(&name); err != nil {
		return err
	}
	for _, q := range []string{
		"UPDATE faces SET person_id = ?2 WHERE person_id = ?1",
		`INSERT OR IGNORE INTO face_rejections (face_id, person_id)
			SELECT face_id, ?2 FROM face_rejections WHERE person_id = ?1`,
		// A face now in into can't also be rejected by it.
		"DELETE FROM face_rejections WHERE person_id = ?2 AND face_id IN (SELECT id FROM faces WHERE person_id = ?2)",
		"DELETE FROM people WHERE id = ?1",
	} {
		if _, err := tx.ExecContext(ctx, q, from, into); err != nil {
			return err
		}
	}
	if name.Valid {
		_, err := tx.ExecContext(ctx, "UPDATE people SET name = ? WHERE id = ? AND name IS NULL", name.String, into)
		return err
	}
	return nil
}

// dropEmptyPeople deletes unnamed people no face belongs to any more. Named
// people stay: the user may still add photos of them.
func dropEmptyPeople(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM people WHERE name IS NULL
		AND NOT EXISTS (SELECT 1 FROM faces WHERE person_id = people.id)`)
	return err
}

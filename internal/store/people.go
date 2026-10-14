package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"lumen/internal/core"
)

// ErrPersonExists means someone else already has that name.
var ErrPersonExists = errors.New("someone already has that name")

// ErrBlockedName means a name belongs to a blocked person or Thing: naming
// someone with it, or tagging with it, would reveal or undo the block.
var ErrBlockedName = errors.New("that name belongs to a blocked person or Thing")

// A core.Face is read (in scanFace order) from faces f joined with its
// asset a and person p.
const (
	faceColumns = `f.id, f.asset_id, a.thumb_version, COALESCE(a.width, 0), COALESCE(a.height, 0),
		a.kind = 'video', f.at_ms,
		f.x, f.y, f.w, f.h, COALESCE(f.person_id, 0), COALESCE(p.name, ''), COALESCE(f.assigned_by = 'user', 0)`
	faceFrom   = ` FROM faces f JOIN assets a ON a.id = f.asset_id LEFT JOIN people p ON p.id = f.person_id`
	faceSelect = "SELECT " + faceColumns + faceFrom
)

func scanFace(r *sql.Rows) (f core.Face, err error) {
	err = r.Scan(&f.ID, &f.AssetID, &f.Thumb, &f.AssetW, &f.AssetH, &f.Video, &f.Frame,
		&f.X, &f.Y, &f.W, &f.H, &f.Person, &f.Name, &f.ByUser)
	return f, err
}

// clearest orders a person's faces best first for a cover: the user's pick,
// then big, sure faces.
const clearest = `f.id = (SELECT cover_face FROM people WHERE id = f.person_id) DESC,
	f.w * a.width * f.h * a.height * f.score DESC`

var countedFacesFrom = ` FROM faces f JOIN assets a ON a.id = f.asset_id WHERE f.person_id IS NOT NULL AND ` + visibleA + ` AND ` + faceShown

var recountPersonCountsSQL = `DELETE FROM person_counts_faces;
	DELETE FROM person_counts;
	INSERT INTO person_counts(person_id, count) SELECT id, 0 FROM people;
	INSERT INTO person_counts_faces(face_id, person_id, asset_id, cover_weight, cover_ready)
	SELECT f.id, f.person_id, f.asset_id, f.w * a.width * f.h * a.height * f.score, a.thumb_version > 0` + countedFacesFrom + `;
	UPDATE person_counts SET count = (SELECT count(DISTINCT asset_id) FROM person_counts_faces WHERE person_id = person_counts.person_id);`

// People lists everyone who appears in a visible photo: named people first,
// then by how many photos they are in.
func (s *Store) People(ctx context.Context) ([]core.Person, error) {
	var people []core.Person
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		people, err = peopleKept(ctx, tx)
		if err != nil {
			return err
		}
		return addKeptCovers(ctx, tx, people)
	})
	return people, err
}

// Person returns one person, with how many visible photos they are in.
func (s *Store) Person(ctx context.Context, id int64) (core.Person, error) {
	var people []core.Person
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		people, err = queryAll(ctx, tx, scanPerson, `SELECT p.id, COALESCE(p.name, ''),
				COALESCE(c.count, 0)
			FROM people p LEFT JOIN person_counts c ON c.person_id = p.id WHERE p.id = ? AND NOT `+labelBlocked(core.BlockPerson, "p.id")+` AND NOT `+personCovered("p.id"), id)
		if err != nil {
			return err
		}
		if len(people) == 0 {
			return ErrNotFound
		}
		return addKeptCovers(ctx, tx, people)
	})
	if err != nil {
		return core.Person{}, err
	}
	return people[0], nil
}

// peopleKept reads the counts in the caller's snapshot; covers use it too.
func peopleKept(ctx context.Context, tx *sql.Tx) ([]core.Person, error) {
	return queryAll(ctx, tx, scanPerson, `SELECT p.id, COALESCE(p.name, ''), c.count
		FROM people p JOIN person_counts c ON c.person_id = p.id
		WHERE c.count > 0 AND NOT `+labelBlocked(core.BlockPerson, "p.id")+`
		ORDER BY p.name IS NULL, c.count DESC, `+byName("p.name")+`, p.id`)
}

// addKeptCovers seeks the user's eligible pick or the first ranked candidate.
// Face details are read live, so names, boxes and thumbnail versions stay current.
func addKeptCovers(ctx context.Context, tx *sql.Tx, people []core.Person) error {
	ids := make([]int64, len(people))
	for i, p := range people {
		ids[i] = p.ID
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	covers, err := queryAll(ctx, tx, scanFace, faceSelect+` WHERE f.id IN (
		SELECT COALESCE(
			(SELECT face_id FROM person_counts_faces WHERE face_id = p.cover_face AND person_id = p.id AND cover_ready = 1),
			(SELECT face_id FROM person_counts_faces WHERE person_id = p.id AND cover_ready = 1
				ORDER BY cover_weight DESC, face_id LIMIT 1))
		FROM people p WHERE p.id IN (SELECT value FROM json_each(?)))`, string(list))
	if err != nil {
		return err
	}
	byPerson := make(map[int64]core.Face, len(covers))
	for _, f := range covers {
		byPerson[f.Person] = f
	}
	for i := range people {
		people[i].Cover = byPerson[people[i].ID]
	}
	return nil
}

func scanPerson(r *sql.Rows) (p core.Person, err error) {
	err = r.Scan(&p.ID, &p.Name, &p.Count)
	return p, err
}

// PersonFaces returns a person's faces in visible photos with their identity
// vectors (vecs[i] belongs to faces[i]), newest photos first. A face a face
// block covers is left out.
func (s *Store) PersonFaces(ctx context.Context, id int64) (faces []core.Face, vecs [][]float32, err error) {
	type row struct {
		f   core.Face
		vec []float32
	}
	rows, err := queryAll(ctx, s.r, func(r *sql.Rows) (x row, err error) {
		var vec []byte
		err = r.Scan(&x.f.ID, &x.f.AssetID, &x.f.Thumb, &x.f.AssetW, &x.f.AssetH, &x.f.Video, &x.f.Frame, &x.f.X, &x.f.Y, &x.f.W, &x.f.H,
			&x.f.Person, &x.f.Name, &x.f.ByUser, &vec)
		x.vec = bytesFloat32(vec)
		return x, err
	}, "SELECT "+faceColumns+", f.vec"+faceFrom+
		` WHERE f.person_id = ? AND `+visibleA+` AND `+faceShown+` AND NOT `+labelBlocked(core.BlockPerson, "f.person_id")+`
		ORDER BY a.taken_at DESC, f.id`, id)
	if err != nil {
		return nil, nil, err
	}
	faces, vecs = make([]core.Face, len(rows)), make([][]float32, len(rows))
	for i, x := range rows {
		faces[i], vecs[i] = x.f, x.vec
	}
	return faces, vecs, nil
}

// AssetFaces returns the faces in one photo, left to right, except those a
// face block covers: in a photo shown in label mode they have no box or name.
func (s *Store) AssetFaces(ctx context.Context, assetID int64) ([]core.Face, error) {
	return queryAll(ctx, s.r, scanFace, faceSelect+` WHERE f.asset_id = ? AND `+visibleA+` AND `+faceShown+` AND NOT `+labelBlocked(core.BlockPerson, "f.person_id")+` ORDER BY f.x`, assetID)
}

// NamedPeople returns everyone the user has named (for finding names in a
// search), except those every face of whom a face block covers.
func (s *Store) NamedPeople(ctx context.Context) ([]core.Person, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (p core.Person, err error) {
		err = r.Scan(&p.ID, &p.Name)
		return p, err
	}, `SELECT id, name FROM people WHERE name IS NOT NULL AND NOT `+labelBlocked(core.BlockPerson, "people.id")+`
		AND NOT `+personCovered("people.id")+`
		ORDER BY length(name) DESC, name`)
}

// PeopleAssets returns the visible assets in which every one of people
// appears, newest first.
func (s *Store) PeopleAssets(ctx context.Context, people []int64) ([]int64, error) {
	list, err := json.Marshal(people)
	if err != nil {
		return nil, err
	}
	return queryAll(ctx, s.r, func(r *sql.Rows) (id int64, err error) {
		err = r.Scan(&id)
		return id, err
	},
		`SELECT f.asset_id FROM faces f JOIN assets a ON a.id = f.asset_id
		WHERE f.person_id IN (SELECT value FROM json_each(?1)) AND `+visibleA+` AND `+faceShown+`
		AND NOT `+labelBlocked(core.BlockPerson, "f.person_id")+`
		GROUP BY f.asset_id HAVING count(DISTINCT f.person_id) = (SELECT count(DISTINCT value) FROM json_each(?1))
		ORDER BY max(a.taken_at) DESC, f.asset_id DESC`, string(list))
}

// SearchPeople returns up to limit named, shown people whose names contain
// text (in any case), names starting with it first. Their counts are not set.
func (s *Store) SearchPeople(ctx context.Context, text string, limit int) ([]core.Person, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []core.Person{}, nil
	}
	var people []core.Person
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		people, err = queryAll(ctx, tx, func(r *sql.Rows) (p core.Person, err error) {
			err = r.Scan(&p.ID, &p.Name)
			return p, err
		}, `SELECT id, name FROM people WHERE name IS NOT NULL AND `+nameMatch+`
			AND NOT `+labelBlocked(core.BlockPerson, "people.id")+` AND NOT `+personCovered("people.id")+`
			ORDER BY `+nameFirst+` LIMIT ?2`, core.Fold(text), limit)
		if err != nil {
			return err
		}
		return addKeptCovers(ctx, tx, people)
	})
	return people, err
}

// PersonByName finds someone by name, whatever its case.
func (s *Store) PersonByName(ctx context.Context, name string) (int64, error) {
	id, err := named(ctx, s.r, peopleNames, strings.TrimSpace(name), 0)
	if err == nil && id != 0 {
		blocked, berr := s.IsBlocked(ctx, core.BlockPerson, id)
		if berr != nil {
			return 0, berr
		}
		if blocked {
			return 0, ErrNotFound
		}
	}
	if err == nil && id == 0 {
		err = ErrNotFound
	}
	return id, err
}

// NamePerson names (or with "", unnames) a person. A name someone else has
// (in any case) is ErrPersonExists: the user decides whether they are one.
func (s *Store) NamePerson(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name != "" {
		var err error
		if name, err = cleanName(name); err != nil {
			return err
		}
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if name != "" {
			switch other, err := named(ctx, tx, peopleNames, name, id); {
			case err != nil:
				return err
			case other != 0:
				var blocked bool
				if err := tx.QueryRowContext(ctx, `SELECT `+labelBlocked(core.BlockPerson, "?"), other).Scan(&blocked); err != nil {
					return err
				}
				if blocked {
					return ErrBlockedName
				}
				return ErrPersonExists
			}
		}
		return affected(tx.ExecContext(ctx, "UPDATE people SET name = NULLIF(?, '') WHERE id = ?", name, id))
	})
}

// SetPersonCover makes one of a person's faces the one shown for them.
func (s *Store) SetPersonCover(ctx context.Context, id, face int64) error {
	return affected(s.w.ExecContext(ctx, `UPDATE people SET cover_face = ?2 WHERE id = ?1
		AND EXISTS (SELECT 1 FROM faces WHERE id = ?2 AND person_id = ?1)`, id, face))
}

// MergePeople says two people are one: everything of from moves to into.
func (s *Store) MergePeople(ctx context.Context, from, into int64) error {
	if from == into {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM people WHERE id IN (?, ?)", from, into).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			return ErrNotFound
		}
		return mergePeople(ctx, tx, from, into)
	})
}

// RejectFaces says faces are not person: they leave that person, and
// clustering never puts them back there.
func (s *Store) RejectFaces(ctx context.Context, person int64, faces []int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT OR IGNORE INTO face_rejections (face_id, person_id)
				SELECT value, ?1 FROM json_each(?2) WHERE EXISTS (SELECT 1 FROM people WHERE id = ?1)`,
			`UPDATE faces SET person_id = NULL, assigned_by = NULL
				WHERE person_id = ?1 AND id IN (SELECT value FROM json_each(?2))`,
		} {
			if err := execList(ctx, tx, q, person, faces); err != nil {
				return err
			}
		}
		return dropEmptyPeople(ctx, tx)
	})
}

// NameFaces puts faces with the person called name, as the user's own
// choice (clustering never moves them again), and returns that person. A
// new name makes a new person; "" always does, unnamed (splitting someone
// in two).
func (s *Store) NameFaces(ctx context.Context, faces []int64, name string) (int64, error) {
	if len(faces) == 0 {
		return 0, ErrNotFound
	}
	name = strings.TrimSpace(name)
	if name != "" {
		var err error
		if name, err = cleanName(name); err != nil {
			return 0, err
		}
	}
	var person int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		list, err := json.Marshal(faces)
		if err != nil {
			return err
		}
		var all bool
		if err := tx.QueryRowContext(ctx, `SELECT count(*) = (SELECT count(DISTINCT value) FROM json_each(?1))
			FROM faces WHERE id IN (SELECT value FROM json_each(?1))`, string(list)).Scan(&all); err != nil {
			return err
		}
		if !all {
			return ErrNotFound // a face went away (its photo was re-read) since the user saw it
		}
		if name != "" {
			person, err = named(ctx, tx, peopleNames, name, 0)
			if err == nil && person != 0 {
				var blocked bool
				err = tx.QueryRowContext(ctx, `SELECT `+labelBlocked(core.BlockPerson, "?"), person).Scan(&blocked)
				if err == nil && blocked {
					return ErrBlockedName
				}
			}
		}
		if err == nil && person == 0 {
			var res sql.Result
			res, err = tx.ExecContext(ctx, "INSERT INTO people (name, created_at) VALUES (NULLIF(?, ''), ?)",
				name, time.Now().Unix())
			if err == nil {
				person, err = res.LastInsertId()
			}
		}
		if err != nil {
			return err
		}
		for _, q := range []string{
			`UPDATE faces SET person_id = ?1, assigned_by = 'user' WHERE id IN (SELECT value FROM json_each(?2))`,
			`DELETE FROM face_rejections WHERE person_id = ?1 AND face_id IN (SELECT value FROM json_each(?2))`,
		} {
			if err := execList(ctx, tx, q, person, faces); err != nil {
				return err
			}
		}
		return dropEmptyPeople(ctx, tx)
	})
	return person, err
}

// execList runs q with ?1 = id and ?2 = ids as a JSON array.
func execList(ctx context.Context, tx *sql.Tx, q string, id int64, ids []int64) error {
	list, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, q, id, string(list))
	return err
}

// affected turns an update that matched no row into ErrNotFound.
func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

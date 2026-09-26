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

// A core.Face is read (in scanFace order) from faces f joined with its
// asset a and person p.
const (
	faceColumns = `f.id, f.asset_id, a.thumb_version, COALESCE(a.width, 0), COALESCE(a.height, 0),
		f.x, f.y, f.w, f.h, COALESCE(f.person_id, 0), COALESCE(p.name, ''), COALESCE(f.assigned_by = 'user', 0)`
	faceFrom   = ` FROM faces f JOIN assets a ON a.id = f.asset_id LEFT JOIN people p ON p.id = f.person_id`
	faceSelect = "SELECT " + faceColumns + faceFrom
)

func scanFace(r *sql.Rows) (f core.Face, err error) {
	return f, r.Scan(&f.ID, &f.AssetID, &f.Thumb, &f.AssetW, &f.AssetH, &f.X, &f.Y, &f.W, &f.H, &f.Person, &f.Name, &f.ByUser)
}

// clearest orders a person's faces best first for a cover: the user's pick,
// then big, sure faces.
const clearest = `f.id = (SELECT cover_face FROM people WHERE id = f.person_id) DESC,
	f.w * a.width * f.h * a.height * f.score DESC`

// People lists everyone who appears in a visible photo: named people first,
// then by how many photos they are in.
func (s *Store) People(ctx context.Context) ([]core.Person, error) {
	people, err := queryAll(ctx, s.r, scanPerson, `SELECT p.id, COALESCE(p.name, ''), p.hidden, count(DISTINCT f.asset_id)
		FROM people p JOIN faces f ON f.person_id = p.id JOIN assets a ON a.id = f.asset_id AND `+visibleAs("a")+`
		GROUP BY p.id ORDER BY p.name IS NULL, count(DISTINCT f.asset_id) DESC, p.name, p.id`)
	if err != nil {
		return nil, err
	}
	return people, s.addCovers(ctx, people)
}

// Person returns one person, with how many visible photos they are in.
func (s *Store) Person(ctx context.Context, id int64) (core.Person, error) {
	people, err := queryAll(ctx, s.r, scanPerson, `SELECT p.id, COALESCE(p.name, ''), p.hidden,
			(SELECT count(DISTINCT f.asset_id) FROM faces f JOIN assets a ON a.id = f.asset_id
				WHERE f.person_id = p.id AND `+visibleAs("a")+`)
		FROM people p WHERE p.id = ?`, id)
	if err != nil {
		return core.Person{}, err
	}
	if len(people) == 0 {
		return core.Person{}, ErrNotFound
	}
	return people[0], s.addCovers(ctx, people)
}

func scanPerson(r *sql.Rows) (p core.Person, err error) {
	return p, r.Scan(&p.ID, &p.Name, &p.Hidden, &p.Count)
}

// addCovers picks each person's clearest face with a thumbnail to show.
func (s *Store) addCovers(ctx context.Context, people []core.Person) error {
	st, err := s.r.PrepareContext(ctx, faceSelect+` WHERE f.person_id = ? AND `+visibleAs("a")+` AND a.thumb_version > 0
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

// PersonFaces returns a person's faces in visible photos with their identity
// vectors (vecs[i] belongs to faces[i]), newest photos first.
func (s *Store) PersonFaces(ctx context.Context, id int64) (faces []core.Face, vecs [][]float32, err error) {
	type row struct {
		f   core.Face
		vec []float32
	}
	rows, err := queryAll(ctx, s.r, func(r *sql.Rows) (x row, err error) {
		var vec []byte
		err = r.Scan(&x.f.ID, &x.f.AssetID, &x.f.Thumb, &x.f.AssetW, &x.f.AssetH, &x.f.X, &x.f.Y, &x.f.W, &x.f.H,
			&x.f.Person, &x.f.Name, &x.f.ByUser, &vec)
		x.vec = bytesFloat32(vec)
		return x, err
	}, "SELECT "+faceColumns+", f.vec"+faceFrom+
		` WHERE f.person_id = ? AND `+visibleAs("a")+` ORDER BY a.taken_at DESC, f.id`, id)
	if err != nil {
		return nil, nil, err
	}
	faces, vecs = make([]core.Face, len(rows)), make([][]float32, len(rows))
	for i, x := range rows {
		faces[i], vecs[i] = x.f, x.vec
	}
	return faces, vecs, nil
}

// AssetFaces returns the faces in one photo, left to right.
func (s *Store) AssetFaces(ctx context.Context, assetID int64) ([]core.Face, error) {
	return queryAll(ctx, s.r, scanFace, faceSelect+" WHERE f.asset_id = ? ORDER BY f.x", assetID)
}

// NamedPeople returns everyone the user has named (for finding names in a
// search).
func (s *Store) NamedPeople(ctx context.Context) ([]core.Person, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (p core.Person, err error) {
		return p, r.Scan(&p.ID, &p.Name, &p.Hidden)
	}, "SELECT id, name, hidden FROM people WHERE name IS NOT NULL ORDER BY length(name) DESC, name")
}

// PeopleAssets returns the visible assets in which every one of people
// appears, newest first.
func (s *Store) PeopleAssets(ctx context.Context, people []int64) ([]int64, error) {
	list, err := json.Marshal(people)
	if err != nil {
		return nil, err
	}
	return queryAll(ctx, s.r, func(r *sql.Rows) (id int64, err error) { return id, r.Scan(&id) },
		`SELECT f.asset_id FROM faces f JOIN assets a ON a.id = f.asset_id
		WHERE f.person_id IN (SELECT value FROM json_each(?1)) AND `+visibleAs("a")+`
		GROUP BY f.asset_id HAVING count(DISTINCT f.person_id) = json_array_length(?1)
		ORDER BY max(a.taken_at) DESC, f.asset_id DESC`, string(list))
}

// SearchPeople returns up to limit named, shown people whose names contain
// text, names starting with it first.
func (s *Store) SearchPeople(ctx context.Context, text string, limit int) ([]core.Person, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []core.Person{}, nil
	}
	like := "%" + likeEscaper.Replace(text) + "%"
	people, err := queryAll(ctx, s.r, func(r *sql.Rows) (p core.Person, err error) {
		return p, r.Scan(&p.ID, &p.Name, &p.Hidden)
	}, `SELECT id, name, hidden FROM people WHERE name LIKE ?1 ESCAPE '\' AND hidden = 0
		ORDER BY name LIKE ?2 ESCAPE '\' DESC, length(name), name LIMIT ?3`, like, strings.TrimPrefix(like, "%"), limit)
	if err != nil {
		return nil, err
	}
	return people, s.addCovers(ctx, people)
}

// PersonByName finds someone by name (case-insensitive).
func (s *Store) PersonByName(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.r.QueryRowContext(ctx, "SELECT id FROM people WHERE name = ?", strings.TrimSpace(name)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// NamePerson names (or with "", unnames) a person.
func (s *Store) NamePerson(ctx context.Context, id int64, name string) error {
	res, err := s.w.ExecContext(ctx, "UPDATE people SET name = NULLIF(?, '') WHERE id = ?", strings.TrimSpace(name), id)
	if isUnique(err) {
		return ErrPersonExists
	}
	return affected(res, err)
}

// SetPersonHidden hides a person from People, or shows them again.
func (s *Store) SetPersonHidden(ctx context.Context, id int64, hidden bool) error {
	return affected(s.w.ExecContext(ctx, "UPDATE people SET hidden = ? WHERE id = ?", hidden, id))
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
		return 0, errors.New("no faces to name")
	}
	name = strings.TrimSpace(name)
	var person int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT id FROM people WHERE name = ?", name).Scan(&person)
		if errors.Is(err, sql.ErrNoRows) {
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
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

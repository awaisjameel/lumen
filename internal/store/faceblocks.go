package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"

	"lumen/internal/core"
)

// A face block remembers a blocked face by where it was, because a face's id
// does not outlive a re-analysis: SaveFaceVersions keeps the block's anchor
// (the face at the stored box) and its vector per face model current, and
// keeps face_block_faces, the faces the block covers, in step with them, all
// in the transaction that saves the faces. A face is only ever compared with
// a block vector of its own model.

// faceShown is true when no face block covers the faces row aliased f (as
// faceFrom names it): the face keeps its box, name and place in face lists.
// Every read that lists faces or counts people through them uses it, so a
// covered face is left out in label mode as well (in photo mode its photo is
// hidden anyway).
const faceShown = "NOT EXISTS (SELECT 1 FROM face_block_faces blk_shown WHERE blk_shown.face_id = f.id)"

// faceBlockTarget returns the face_blocks row for the face faceID, making it
// if the face has none yet: where the face is, its vector, and the faces it
// covers (with lookalikes, the faces like it too), and the block's name. A
// block the face already anchors is returned as it is, except that asking for
// look-alikes widens it: that hides more, so it needs no PIN, where narrowing
// would reveal photos and is left to removing the block.
func (s *Store) faceBlockTarget(ctx context.Context, tx *sql.Tx, faceID int64, lookalikes bool) (faceBlockID int64, name string, err error) {
	var f struct {
		asset, frame int64
		box          box
		model        string
		vec          []byte
		person, file string
	}
	err = tx.QueryRowContext(ctx, `SELECT f.asset_id, f.at_ms, f.x, f.y, f.w, f.h, f.model, f.vec, COALESCE(p.name, ''),
			COALESCE((SELECT path FROM files WHERE asset_id = f.asset_id ORDER BY id LIMIT 1), '')
		FROM faces f LEFT JOIN people p ON p.id = f.person_id WHERE f.id = ?`, faceID).
		Scan(&f.asset, &f.frame, &f.box.X, &f.box.Y, &f.box.W, &f.box.H, &f.model, &f.vec, &f.person, &f.file)
	if err != nil {
		return 0, "", err
	}
	name = f.person
	if name == "" {
		name = "Face in " + filepath.Base(f.file)
	}
	vec := bytesFloat32(f.vec)
	if len(vec) == 0 {
		return 0, "", ErrInvalidBlock
	}

	var had bool
	err = tx.QueryRowContext(ctx, "SELECT id, lookalikes FROM face_blocks WHERE face_id = ?", faceID).Scan(&faceBlockID, &had)
	switch {
	case err == nil:
		if lookalikes && !had {
			err = widenFaceBlock(ctx, tx, faceBlockID)
		}
		return faceBlockID, name, err
	case !errors.Is(err, sql.ErrNoRows):
		return 0, "", err
	}

	res, err := tx.ExecContext(ctx, `INSERT INTO face_blocks (asset_id, at_ms, x, y, w, h, face_id, lookalikes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, f.asset, f.frame, f.box.X, f.box.Y, f.box.W, f.box.H, faceID, lookalikes)
	if err != nil {
		return 0, "", err
	}
	if faceBlockID, err = res.LastInsertId(); err != nil {
		return 0, "", err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO face_block_vectors (face_block_id, model, vec) VALUES (?, ?, ?)",
		faceBlockID, f.model, f.vec); err != nil {
		return 0, "", err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO face_block_faces (face_block_id, face_id, asset_id) VALUES (?, ?, ?)",
		faceBlockID, faceID, f.asset); err != nil {
		return 0, "", err
	}
	if lookalikes {
		err = coverLookalikes(ctx, tx, faceBlockID, f.model, vec, faceID)
	}
	return faceBlockID, name, err
}

// widenFaceBlock makes block blockID cover look-alikes, for every model it
// has a vector for.
func widenFaceBlock(ctx context.Context, tx *sql.Tx, blockID int64) error {
	type held struct {
		model string
		vec   []float32
	}
	vectors, err := queryAll(ctx, tx, func(r *sql.Rows) (h held, err error) {
		var b []byte
		err = r.Scan(&h.model, &b)
		h.vec = bytesFloat32(b)
		return h, err
	}, "SELECT model, vec FROM face_block_vectors WHERE face_block_id = ?", blockID)
	if err != nil {
		return err
	}
	var anchor sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT face_id FROM face_blocks WHERE id = ?", blockID).Scan(&anchor); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE face_blocks SET lookalikes = 1 WHERE id = ?", blockID); err != nil {
		return err
	}
	for _, h := range vectors {
		if err := coverLookalikes(ctx, tx, blockID, h.model, h.vec, anchor.Int64); err != nil {
			return err
		}
	}
	return nil
}

// FaceBlockAssets returns the photos that hold a blocked face's anchor, for
// the faces stage to take first during a face model upgrade (they are few).
func (s *Store) FaceBlockAssets(ctx context.Context) ([]int64, error) {
	return queryAll(ctx, s.r, scanID, "SELECT DISTINCT asset_id FROM face_blocks ORDER BY asset_id")
}

// coverLookalikes makes block blockID cover exactly the faces of model that
// are like vec (and the anchor, whatever its vector), among that model's
// faces: it adds the faces now alike and drops the ones no longer so, such as
// after the anchor was found again with a slightly different vector. Faces of
// other models are left as they are.
func coverLookalikes(ctx context.Context, tx *sql.Tx, blockID int64, model string, vec []float32, anchor int64) error {
	like, err := findLookalikes(ctx, tx, model, vec)
	if err != nil {
		return err
	}
	if anchor != 0 {
		like = append(like, anchor)
	}
	list, err := json.Marshal(like)
	if err != nil {
		return err
	}
	// Looked up by key from the block's few rows: the model has no index, and
	// a join the other way would walk every face.
	if _, err := tx.ExecContext(ctx, `DELETE FROM face_block_faces WHERE face_block_id = ?1 AND face_id != ?3
		AND EXISTS (SELECT 1 FROM faces f WHERE f.id = face_block_faces.face_id AND f.model = ?2)
		AND face_id NOT IN (SELECT value FROM json_each(?4))`, blockID, model, anchor, string(list)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO face_block_faces (face_block_id, face_id, asset_id)
		SELECT ?1, f.id, f.asset_id FROM faces f WHERE f.id IN (SELECT value FROM json_each(?2))`, blockID, string(list))
	return err
}

// findLookalikes returns the faces of model whose cosine similarity to vec is
// at least core.FaceLookalikeMin. Face vectors are unit length, so that is
// their dot product. It streams the vectors without decoding them into
// slices: at 300,000 faces the allocations would cost more than the maths.
func findLookalikes(ctx context.Context, q querier, model string, vec []float32) ([]int64, error) {
	rows, err := q.QueryContext(ctx, "SELECT id, vec FROM faces WHERE model = ?", model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	like := []int64{}
	for rows.Next() {
		var id int64
		var raw sql.RawBytes
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if len(raw) == 4*len(vec) && dotBytes(vec, raw) >= core.FaceLookalikeMin {
			like = append(like, id)
		}
	}
	return like, rows.Err()
}

// dotBytes is the dot product of a with the little-endian float32s in b, which
// holds len(a) of them. It is Dot over float32Bytes, four sums at a time.
func dotBytes(a []float32, b []byte) float32 {
	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= len(a); i += 4 {
		w := b[4*i : 4*i+16 : 4*i+16]
		s0 += a[i] * math.Float32frombits(binary.LittleEndian.Uint32(w[0:]))
		s1 += a[i+1] * math.Float32frombits(binary.LittleEndian.Uint32(w[4:]))
		s2 += a[i+2] * math.Float32frombits(binary.LittleEndian.Uint32(w[8:]))
		s3 += a[i+3] * math.Float32frombits(binary.LittleEndian.Uint32(w[12:]))
	}
	for ; i < len(a); i++ {
		s0 += a[i] * math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return s0 + s1 + s2 + s3
}

// savedFaces is one photo's faces as SaveFaceVersions wrote them: ids[i] is
// the faces row of faces[i].
type savedFaces struct {
	asset int64
	faces []DetectedFace
	ids   []int64
}

// saveFaceBlocks keeps face blocks current with faces just written, in the
// same transaction. First every block anchored in a re-analysed photo finds
// its face again by box and takes its vector under model; then the new and
// changed faces are compared with the vectors of the look-alike blocks, and
// covered or uncovered. In that order, so a face is judged by the anchor's
// vector as it now stands.
func saveFaceBlocks(ctx context.Context, tx *sql.Tx, model string, saved []savedFaces) error {
	var exist bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM face_blocks)").Scan(&exist); err != nil || !exist {
		return err
	}
	if err := reanchor(ctx, tx, model, saved); err != nil {
		return err
	}
	return judgeFaces(ctx, tx, model, saved)
}

// anchorBlock is a face block as re-anchoring reads it.
type anchorBlock struct {
	id, asset  int64
	face       int64 // the anchor face now; 0 while none
	lookalikes bool
	box        storedBox
}

func reanchor(ctx context.Context, tx *sql.Tx, model string, saved []savedFaces) error {
	assets := make([]int64, 0, len(saved))
	for _, s := range saved {
		if len(s.faces) > 0 {
			assets = append(assets, s.asset)
		}
	}
	list, err := json.Marshal(assets)
	if err != nil {
		return err
	}
	blocks, err := queryAll(ctx, tx, func(r *sql.Rows) (b anchorBlock, err error) {
		var face sql.NullInt64
		err = r.Scan(&b.id, &b.asset, &b.box.Frame, &b.box.X, &b.box.Y, &b.box.W, &b.box.H, &face, &b.lookalikes)
		b.face, b.box.id = face.Int64, b.id
		return b, err
	}, `SELECT id, asset_id, at_ms, x, y, w, h, face_id, lookalikes FROM face_blocks
		WHERE asset_id IN (SELECT value FROM json_each(?))`, string(list))
	if err != nil || len(blocks) == 0 {
		return err
	}
	byAsset := map[int64][]anchorBlock{}
	for _, b := range blocks {
		byAsset[b.asset] = append(byAsset[b.asset], b)
	}

	for _, s := range saved {
		held := byAsset[s.asset]
		if len(held) == 0 {
			continue
		}
		boxes := make([]storedBox, len(held))
		for i, b := range held {
			boxes[i] = b.box
		}
		took := matchFaces(boxes, s.faces)
		for i, id := range took {
			if id == 0 || len(s.faces[i].Vec) == 0 {
				continue
			}
			for _, b := range held {
				if b.id == id {
					if err := anchorAt(ctx, tx, model, b, s.asset, s.ids[i], s.faces[i]); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// anchorAt makes face (the faces row faceID, in photo asset) the anchor of
// block b, with its vector under model, and recomputes the block's
// look-alikes for that model.
func anchorAt(ctx context.Context, tx *sql.Tx, model string, b anchorBlock, asset, faceID int64, face DetectedFace) error {
	if _, err := tx.ExecContext(ctx, `UPDATE face_blocks SET face_id = ?, at_ms = ?, x = ?, y = ?, w = ?, h = ? WHERE id = ?`,
		faceID, face.Frame, face.X, face.Y, face.W, face.H, b.id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO face_block_vectors (face_block_id, model, vec) VALUES (?, ?, ?)",
		b.id, model, float32Bytes(face.Vec)); err != nil {
		return err
	}
	if b.face != 0 && b.face != faceID && !b.lookalikes {
		// A block of one face covers only its anchor: the face it left is
		// no longer it. (With look-alikes the recomputation below decides.)
		if _, err := tx.ExecContext(ctx, "DELETE FROM face_block_faces WHERE face_block_id = ? AND face_id = ?", b.id, b.face); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO face_block_faces (face_block_id, face_id, asset_id) VALUES (?, ?, ?)",
		b.id, faceID, asset); err != nil {
		return err
	}
	if !b.lookalikes {
		return nil
	}
	return coverLookalikes(ctx, tx, b.id, model, face.Vec, faceID)
}

// judgeFaces compares the faces just saved with every look-alike block's
// vector of their model: a face like it is covered, one that is not (a face
// found again with a different vector) is uncovered. A block with no vector
// for the model yet is left alone: its anchor has not been found at this
// model, and re-anchoring will judge every face of the model then.
func judgeFaces(ctx context.Context, tx *sql.Tx, model string, saved []savedFaces) error {
	type likeBlock struct {
		id, face int64
		vec      []float32
	}
	blocks, err := queryAll(ctx, tx, func(r *sql.Rows) (b likeBlock, err error) {
		var face sql.NullInt64
		var raw []byte
		err = r.Scan(&b.id, &face, &raw)
		b.face, b.vec = face.Int64, bytesFloat32(raw)
		return b, err
	}, `SELECT b.id, b.face_id, v.vec FROM face_blocks b JOIN face_block_vectors v ON v.face_block_id = b.id
		WHERE b.lookalikes = 1 AND v.model = ?`, model)
	if err != nil || len(blocks) == 0 {
		return err
	}
	type pair struct{ Block, Face, Asset int64 }
	var cover, uncover []pair
	for _, s := range saved {
		for i, f := range s.faces {
			for _, b := range blocks {
				p := pair{b.id, s.ids[i], s.asset}
				switch {
				case s.ids[i] == b.face || (len(f.Vec) == len(b.vec) && dot(b.vec, f.Vec) >= core.FaceLookalikeMin):
					cover = append(cover, p)
				default:
					uncover = append(uncover, p)
				}
			}
		}
	}
	if len(cover) > 0 {
		list, err := json.Marshal(cover)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO face_block_faces (face_block_id, face_id, asset_id)
			SELECT json_extract(j.value, '$.Block'), json_extract(j.value, '$.Face'), json_extract(j.value, '$.Asset')
			FROM json_each(?) j`, string(list)); err != nil {
			return err
		}
	}
	if len(uncover) > 0 {
		list, err := json.Marshal(uncover)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM face_block_faces WHERE (face_block_id, face_id) IN
			(SELECT json_extract(j.value, '$.Block'), json_extract(j.value, '$.Face') FROM json_each(?) j)`, string(list)); err != nil {
			return err
		}
	}
	return nil
}

// dot is the dot product of two vectors of one length (the cosine similarity
// of unit vectors; store cannot import ai, which loads the model runtime).
func dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

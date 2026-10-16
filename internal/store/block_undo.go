package store

import (
	"context"
	"database/sql"
)

// Keep the positions at removal so restoring an event's tag can distinguish
// members that moved away during the Undo window from the ones that stayed.
type removedEventMember struct {
	ID      int64    `json:"id"`
	TakenAt int64    `json:"takenAt"`
	Lat     *float64 `json:"lat"`
	Lon     *float64 `json:"lon"`
	Moved   bool     `json:"moved"`
}

// A removed face block retains its box and model-specific vectors, rather
// than a face ID that re-analysis can replace during the toast's lifetime.
type removedFaceBlock struct {
	asset      int64
	box        DetectedFace
	lookalikes bool
	vectors    []removedFaceVector
}

type removedFaceVector struct {
	model string
	vec   []byte
}

func captureFaceBlock(ctx context.Context, tx *sql.Tx, id int64) (*removedFaceBlock, error) {
	b := &removedFaceBlock{}
	err := tx.QueryRowContext(ctx, `SELECT asset_id, at_ms, x, y, w, h, lookalikes FROM face_blocks WHERE id = ?`, id).
		Scan(&b.asset, &b.box.Frame, &b.box.X, &b.box.Y, &b.box.W, &b.box.H, &b.lookalikes)
	if err != nil {
		return nil, err
	}
	b.vectors, err = queryAll(ctx, tx, func(r *sql.Rows) (v removedFaceVector, err error) {
		err = r.Scan(&v.model, &v.vec)
		return v, err
	}, "SELECT model, vec FROM face_block_vectors WHERE face_block_id = ?", id)
	return b, err
}

func restoreFaceBlock(ctx context.Context, tx *sql.Tx, b *removedFaceBlock) (int64, error) {
	if b == nil {
		return 0, ErrNotFound
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO face_blocks(asset_id, at_ms, x, y, w, h, lookalikes)
		SELECT id, ?, ?, ?, ?, ?, ? FROM assets WHERE id = ?`, b.box.Frame, b.box.X, b.box.Y, b.box.W, b.box.H, b.lookalikes, b.asset)
	if err := affected(result, err); err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	insert, err := tx.PrepareContext(ctx, "INSERT INTO face_block_vectors(face_block_id, model, vec) VALUES (?, ?, ?)")
	if err != nil {
		return 0, err
	}
	defer insert.Close()
	for _, vector := range b.vectors {
		if _, err := insert.ExecContext(ctx, id, vector.model, vector.vec); err != nil {
			return 0, err
		}
	}
	type currentFace struct {
		id    int64
		model string
		face  DetectedFace
	}
	faces, err := queryAll(ctx, tx, func(r *sql.Rows) (f currentFace, err error) {
		var vec []byte
		err = r.Scan(&f.id, &f.model, &f.face.Frame, &f.face.X, &f.face.Y, &f.face.W, &f.face.H, &vec)
		f.face.Vec = bytesFloat32(vec)
		return f, err
	}, "SELECT id, model, at_ms, x, y, w, h, vec FROM faces WHERE asset_id = ? ORDER BY id", b.asset)
	if err != nil {
		return 0, err
	}
	byModel := map[string]savedFaces{}
	for _, f := range faces {
		saved := byModel[f.model]
		saved.asset = b.asset
		saved.ids, saved.faces = append(saved.ids, f.id), append(saved.faces, f.face)
		byModel[f.model] = saved
	}
	for model, saved := range byModel {
		if err := reanchor(ctx, tx, model, []savedFaces{saved}); err != nil {
			return 0, err
		}
	}
	if b.lookalikes {
		if err := widenFaceBlock(ctx, tx, id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

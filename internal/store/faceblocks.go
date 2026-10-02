package store

import (
	"context"
	"database/sql"
	"errors"
)

// faceBlockTarget returns the face_blocks row for the face faceID, making it
// if the face has none yet: where the face is, its vector, and the faces it
// covers (with lookalikes, the faces like it too), and the block's name.
// Phase 5 unit U7 builds it.
func (s *Store) faceBlockTarget(_ context.Context, _ *sql.Tx, _ int64, _ bool) (faceBlockID int64, name string, err error) {
	return 0, "", errors.ErrUnsupported
}

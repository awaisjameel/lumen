package store

import (
	"context"
	"database/sql"

	"lumen/internal/core"
)

// BatchResult reports an item's outcome only after its transaction commits.
// Services translate the errors into the wire contract's codes and messages.
type BatchResult struct {
	ID  int64
	Err error
}

// ApplyBatch reuses the single-item mutations in one transaction per part.
// Visibility is checked beside each write, so a stale selection cannot edit
// covered or trashed assets. Restore instead takes items from the Trash.
func (s *Store) ApplyBatch(ctx context.Context, ids []int64, action core.BatchAction, albumID int64) ([]BatchResult, error) {
	var one func(*sql.Tx, int64) error
	condition := visible
	switch action {
	case core.BatchFavourite, core.BatchUnfavourite:
		one = func(tx *sql.Tx, id int64) error { return setFavourite(ctx, tx, id, action == core.BatchFavourite) }
	case core.BatchAlbum, core.BatchRemoveAlbum:
		one = func(tx *sql.Tx, id int64) error { return setAlbumItem(ctx, tx, id, albumID, action == core.BatchAlbum) }
	case core.BatchTrash, core.BatchRestore:
		one = func(tx *sql.Tx, id int64) error { return setTrashed(ctx, tx, []int64{id}, action == core.BatchTrash) }
		if action == core.BatchRestore {
			condition = trashed
		}
	default:
		return nil, ErrNotFound
	}
	return s.batch(ctx, ids, condition, one)
}

// TagAssets applies a resolved tag to visible assets in one transaction.
func (s *Store) TagAssets(ctx context.Context, ids []int64, tagID int64) ([]BatchResult, error) {
	return s.batch(ctx, ids, visible, func(tx *sql.Tx, id int64) error { return tagAsset(ctx, tx, id, tagID) })
}

func (s *Store) batch(ctx context.Context, ids []int64, condition string, one func(*sql.Tx, int64) error) ([]BatchResult, error) {
	outcomes := make([]BatchResult, len(ids))
	if len(ids) == 0 {
		return outcomes, nil
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		check, err := tx.PrepareContext(ctx, "SELECT EXISTS (SELECT 1 FROM assets WHERE id = ? AND "+condition+")")
		if err != nil {
			return err
		}
		defer check.Close()
		for i, id := range ids {
			if err := ctx.Err(); err != nil {
				return err // Cancellation rolls back the entire part.
			}
			var ok bool
			if err := check.QueryRowContext(ctx, id).Scan(&ok); err != nil {
				return err
			}
			outcomes[i].ID = id
			if !ok {
				outcomes[i].Err = ErrNotFound
			} else {
				outcomes[i].Err = one(tx, id)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err // No successes are reported for a rolled-back part.
	}
	return outcomes, nil
}

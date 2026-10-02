package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"lumen/internal/core"
)

// BlockCoverage lists the photos the block blockID covers, for the Blocked
// screen's browser: the present, untrashed, unhidden assets its target
// matches, whatever its mode (a label-only block's photos are listed too,
// though the library still shows them), newest first. Each carries the other
// photo-mode blocks that cover it as well, so the browser can say what still
// hides it once this block is removed. The match is blockMatch, the SQL
// visibility uses. A block that does not exist fails with ErrNotFound.
func (s *Store) BlockCoverage(ctx context.Context, blockID int64) ([]core.CoveredItem, error) {
	var items []core.CoveredItem
	err := s.read(ctx, func(tx *sql.Tx) error {
		var found bool
		err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM blocks WHERE id = ?)", blockID).Scan(&found)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		items, err = queryAll(ctx, tx, func(r *sql.Rows) (it core.CoveredItem, err error) {
			it.HiddenBy = []core.BlockRef{} // never null on the wire
			err = r.Scan(append([]any{&it.ID}, gridFields(&it.GridItem)...)...)
			return it, err
		}, `SELECT id, `+gridColumns+`
			FROM assets
			WHERE `+baseVisibleOf("")+` AND EXISTS (SELECT 1 FROM blocks target WHERE target.id = ? AND `+blockMatch("assets", "target")+`)
			ORDER BY taken_at DESC, id DESC`, blockID)
		if err != nil || len(items) == 0 {
			return err
		}
		return addHiddenBy(ctx, tx, blockID, items)
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// addHiddenBy fills in the other photo-mode blocks that cover each of items,
// all in one query over the covered assets.
func addHiddenBy(ctx context.Context, tx *sql.Tx, blockID int64, items []core.CoveredItem) error {
	ids := make([]int64, len(items))
	at := make(map[int64]int, len(items))
	for i, it := range items {
		ids[i], at[it.ID] = it.ID, i
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	type overlap struct {
		asset int64
		by    core.BlockRef
	}
	overlaps, err := queryAll(ctx, tx, func(r *sql.Rows) (o overlap, err error) {
		err = r.Scan(&o.asset, &o.by.ID, &o.by.Name)
		return o, err
	}, `SELECT assets.id, other.id, `+blockName("other")+`
		FROM blocks other, assets
		WHERE other.mode = 'photos' AND other.id != ?1
		AND assets.id IN (SELECT value FROM json_each(?2)) AND `+blockMatch("assets", "other")+`
		ORDER BY other.created_at DESC, other.id DESC`, blockID, string(list))
	if err != nil {
		return err
	}
	for _, o := range overlaps {
		items[at[o.asset]].HiddenBy = append(items[at[o.asset]].HiddenBy, o.by)
	}
	return nil
}

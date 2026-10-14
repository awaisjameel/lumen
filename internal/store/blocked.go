package store

import (
	"context"
	"database/sql"
)

// Local removal must seek dirty asset keys rather than scan existing coverage.
const removeDirtyBlocksWhere = "asset_id IN (SELECT asset_id FROM blocked_dirty WHERE is_blocked = 0)"
const removeDirtyBlocksSQL = "DELETE FROM blocked_assets WHERE " + removeDirtyBlocksWhere

// storeWriter keeps single-statement writes on the same settlement path as
// batches. Embedding the pool retains connection access for backups and schema
// migrations, which deliberately use runTx without settlement.
type storeWriter struct {
	*sql.DB
	execContext func(context.Context, string, ...any) (sql.Result, error)
}

func (w *storeWriter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return w.execContext(ctx, query, args...)
}

func (s *Store) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	var result sql.Result
	err := s.tx(ctx, func(tx *sql.Tx) (err error) {
		result, err = tx.ExecContext(ctx, query, args...)
		return err
	})
	return result, err
}

// settleBlockedAssets evaluates the original predicate only for changed
// assets, after event settling has finished changing membership. Both the
// answers and the dirty markers commit with their inputs, so readers cannot
// observe an indexed photo before its block has been applied.
func settleBlockedAssets(ctx context.Context, tx *sql.Tx) error {
	var rebuild bool
	var dirty, events, visibility int64
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM blocked_rebuild), count(*),
		COALESCE(sum(event_increments), 0), COALESCE(sum(visible_increments), 0)
		FROM blocked_dirty`).Scan(&rebuild, &dirty, &events, &visibility); err != nil {
		return err
	}
	if !rebuild && dirty == 0 {
		return nil
	}
	// Keep the original revision totals without a second write for each tag,
	// face or file row. Readers see these increments only after the commit.
	if events > 0 || visibility > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE event_revision SET value = value + ?, visible_value = visible_value + ? WHERE id = 1", events, visibility); err != nil {
			return err
		}
	}
	var photos bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos')").Scan(&photos); err != nil {
		return err
	}
	if !photos {
		var removed int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM blocked_assets").Scan(&removed); err != nil {
			return err
		}
		if err := markKeptCountsForChange(ctx, tx, removed); err != nil {
			return err
		}
		deleted, err := tx.ExecContext(ctx, "DELETE FROM blocked_assets")
		if err != nil {
			return err
		}
		removed, err = deleted.RowsAffected()
		if err != nil {
			return err
		}
		return finishBlockedAssets(ctx, tx, removed)
	}
	match := "EXISTS (SELECT 1 FROM blocks blk WHERE blk.mode = 'photos' AND " + blockMatch("a", "blk") + ")"
	if !rebuild {
		var covered bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM blocked_dirty d
			CROSS JOIN blocked_assets b ON b.asset_id = d.asset_id)`).Scan(&covered); err != nil {
			return err
		}
		if !covered {
			// New or previously unblocked assets cannot require a removal.
			// Insert their matches directly to avoid temporary answers during ingest.
			candidates := " FROM blocked_dirty d CROSS JOIN assets a ON a.id = d.asset_id WHERE " + match
			// Only a batch with that many dirty assets can reach the bulk
			// limit, so an ordinary ingest batch evaluates the blocks once.
			if dirty >= keptCountsLimit(ctx) {
				var added int64
				if err := tx.QueryRowContext(ctx, "SELECT count(*)"+candidates).Scan(&added); err != nil {
					return err
				}
				if err := markKeptCountsForChange(ctx, tx, added); err != nil {
					return err
				}
			}
			inserted, err := tx.ExecContext(ctx, "INSERT INTO blocked_assets(asset_id) SELECT a.id"+candidates)
			if err != nil {
				return err
			}
			added, err := inserted.RowsAffected()
			if err != nil {
				return err
			}
			return finishBlockedAssets(ctx, tx, added)
		}
	}
	// Evaluate each asset once and retain the answer for both removal and
	// insertion. Repeating the correlated probes adds indexing cost.
	if rebuild {
		if _, err := tx.ExecContext(ctx, "DELETE FROM blocked_dirty"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO blocked_dirty(asset_id, is_blocked) SELECT a.id, 1 FROM assets a WHERE "+match); err != nil {
			return err
		}
	} else {
		// Dirty ids drive primary-key asset lookups, even on a large library.
		answer := "EXISTS (SELECT 1 FROM assets a WHERE a.id = blocked_dirty.asset_id AND " + match + ")"
		if _, err := tx.ExecContext(ctx, "UPDATE blocked_dirty SET is_blocked = "+answer); err != nil {
			return err
		}
	}
	removeWhere := "asset_id NOT IN (SELECT asset_id FROM blocked_dirty WHERE is_blocked = 1)"
	if !rebuild {
		removeWhere = removeDirtyBlocksWhere
	}
	// Without a rebuild, no more assets can change than are dirty.
	if rebuild || dirty >= keptCountsLimit(ctx) {
		var changed int64
		if err := tx.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM blocked_assets WHERE "+removeWhere+
			") + (SELECT count(*) FROM blocked_dirty d WHERE d.is_blocked = 1 AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = d.asset_id))").Scan(&changed); err != nil {
			return err
		}
		if err := markKeptCountsForChange(ctx, tx, changed); err != nil {
			return err
		}
	}
	deleted, err := tx.ExecContext(ctx, "DELETE FROM blocked_assets WHERE "+removeWhere)
	if err != nil {
		return err
	}
	removed, err := deleted.RowsAffected()
	if err != nil {
		return err
	}
	inserted, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO blocked_assets(asset_id) SELECT asset_id FROM blocked_dirty WHERE is_blocked = 1")
	if err != nil {
		return err
	}
	added, err := inserted.RowsAffected()
	if err != nil {
		return err
	}
	return finishBlockedAssets(ctx, tx, removed+added)
}

func finishBlockedAssets(ctx context.Context, tx *sql.Tx, changed int64) error {
	// Preserve unchanged rows so metadata batches do not invalidate every
	// cached reader. Actual coverage changes must invalidate them even for
	// predicate inputs that the older revision triggers did not cover.
	if changed > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1"); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM blocked_dirty; DELETE FROM blocked_rebuild")
	return err
}

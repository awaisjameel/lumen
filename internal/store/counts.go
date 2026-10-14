package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

// keptCountsThreshold is the number of assets one write may change before the
// kept counts are recounted rather than maintained row by row. The
// 100,000-photo fixture costs about 0.72 seconds to recount and 23
// microseconds per photo in row maintenance, crossing over near 31,200 photos.
const keptCountsThreshold int64 = 32000

// keptCountsThresholdKey lets a test lower the limit for one context, so both
// paths meet the reference without shared mutable state.
type keptCountsThresholdKey struct{}

func keptCountsLimit(ctx context.Context) int64 {
	if n, ok := ctx.Value(keptCountsThresholdKey{}).(int64); ok {
		return n
	}
	return keptCountsThreshold
}

var keptCounts = []struct {
	name    string
	recount string
}{
	{"day_counts", recountDayCountsSQL},
	{"tag_counts", recountTagCountsSQL},
	{"person_counts", recountPersonCountsSQL},
	{"job_counts", recountJobCountsSQL},
}

// settleKeptCounts evaluates each definition after visibility has settled.
// Marks remain until the recount finishes, so its own triggers stay quiet.
func settleKeptCounts(ctx context.Context, tx *sql.Tx) error {
	marks, err := queryAll(ctx, tx, func(r *sql.Rows) (name string, err error) {
		err = r.Scan(&name)
		return name, err
	}, "SELECT name FROM count_rebuild")
	if err != nil {
		return err
	}
	for _, table := range keptCounts {
		if !slices.Contains(marks, table.name) {
			continue
		}
		if _, err := tx.ExecContext(ctx, table.recount); err != nil {
			return fmt.Errorf("recount %s: %w", table.name, err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM count_rebuild WHERE name = ?", table.name); err != nil {
			return err
		}
	}
	return nil
}

// markKeptCountsStale marks every kept table for a recount before commit, so
// their triggers stand aside for a change that is cheaper to count once.
func markKeptCountsStale(ctx context.Context, tx *sql.Tx) error {
	for _, table := range keptCounts {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO count_rebuild(name) VALUES (?)", table.name); err != nil {
			return err
		}
	}
	return nil
}

// markKeptCountsForChange marks the kept tables when a write is about to
// change the visibility of at least the bulk limit's number of assets.
func markKeptCountsForChange(ctx context.Context, tx *sql.Tx, changed int64) error {
	if changed < keptCountsLimit(ctx) {
		return nil
	}
	return markKeptCountsStale(ctx, tx)
}

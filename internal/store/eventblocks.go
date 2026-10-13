package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// eventBlocked is true when an event block holds the assets row alias asset,
// in either mode: a view of one event re-checks it in the snapshot that reads
// the photos, as viewSQL does for the targets that have a row to name.
func eventBlocked(asset string) string {
	return `EXISTS (SELECT 1 FROM event_members blk_held JOIN blocks blk_event
		ON blk_event.kind = 'event' AND blk_event.target_id = blk_held.tag_id
		WHERE blk_held.asset_id = ` + asset + ".id)"
}

// eventBlockTarget finds the event holding the asset eventID, as Event groups
// it, and returns the block tag (an event_tags row of kind 'block') whose
// members are that event's photos, making it if the event has none yet, and
// the event's name. The members are the photos of the base timeline within the
// event, as a name's or a pin's are, so settling keeps them equal to the event
// as it regroups: a photo that joins is hidden in the transaction that adds
// it, and one that leaves comes back.
func (s *Store) eventBlockTarget(ctx context.Context, tx *sql.Tx, eventID int64) (tagID int64, name string, err error) {
	// An event that is blocked already is found through its photos, hidden
	// ones included: a photo-mode block takes the event out of Events, so
	// Event could no longer say which event the asset belongs to. A photo
	// that is trashed, hidden or missing belongs to no event.
	err = tx.QueryRowContext(ctx, `SELECT t.id, b.name FROM assets a
		CROSS JOIN event_members m ON m.asset_id = a.id
		JOIN event_tags t ON t.id = m.tag_id AND t.kind = 'block'
		JOIN blocks b ON b.kind = 'event' AND b.target_id = t.id
		WHERE a.id = ? AND `+baseVisibleOf("a")+" ORDER BY t.id LIMIT 1", eventID).Scan(&tagID, &name)
	if !errors.Is(err, sql.ErrNoRows) {
		return tagID, name, err // found, and the stored name stays: a mode change renames nothing
	}
	gap, err := eventGapIn(ctx, tx)
	if err != nil {
		return 0, "", err
	}
	event, err := eventIn(ctx, tx, eventID, int(gap/3600))
	if err != nil {
		return 0, "", err
	}
	res, err := tx.ExecContext(ctx, "INSERT INTO event_tags (kind, created_at) VALUES ('block', ?)", time.Now().Unix())
	if err != nil {
		return 0, "", err
	}
	if tagID, err = res.LastInsertId(); err != nil {
		return 0, "", err
	}
	where, args := eventAssets(event)
	if _, err := tx.ExecContext(ctx, "INSERT INTO event_members (tag_id, asset_id) SELECT ?, id FROM assets WHERE "+where,
		append([]any{tagID}, args...)...); err != nil {
		return 0, "", err
	}
	return tagID, event.Name, nil
}

package store

import (
	"context"
	"database/sql"
	"errors"
)

// eventBlockTarget finds the event holding the asset eventID, as Event groups
// it, and returns the block tag (an event_tags row of kind 'block') whose
// members are that event's photos, making it if the event has none yet, and
// the event's name. Phase 5 unit U6 builds it.
func (s *Store) eventBlockTarget(_ context.Context, _ *sql.Tx, _ int64) (tagID int64, name string, err error) {
	return 0, "", errors.ErrUnsupported
}

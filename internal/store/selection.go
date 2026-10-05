package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"lumen/internal/core"
)

// ErrSelectionChanged refuses a snapshot whose membership may have changed.
var ErrSelectionChanged = errors.New("the selection changed; select the results again")

// Selection resolves stable visible IDs and their membership revision.
func (s *Store) Selection(ctx context.Context, view core.View, ids []int64, search bool) (core.Selection, error) {
	var before int64
	if err := s.r.QueryRowContext(ctx, selectionRevisionSQL).Scan(&before); err != nil {
		return core.Selection{}, err
	}
	confidence, err := s.Float(ctx, core.SettingTagConfidence, core.TagConfidenceDefault)
	if err != nil {
		return core.Selection{}, err
	}
	where, args, err := s.viewSQL(ctx, view, confidence)
	if err != nil {
		return core.Selection{}, err
	}
	order := "taken_at DESC, id DESC"
	if search {
		data, err := json.Marshal(ids)
		if err != nil {
			return core.Selection{}, err
		}
		// Ranked results keep the order they were given in.
		where, args = visible+" AND id IN (SELECT value FROM json_each(?1))", []any{string(data)}
		order = "(SELECT key FROM json_each(?1) WHERE value = assets.id)"
	}
	var sel core.Selection
	err = s.read(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, selectionRevisionSQL).Scan(&sel.Revision); err != nil {
			return err
		}
		// View planning reads album/event rules before this transaction. Reject
		// changes spanning those reads instead of binding old rules to a new revision.
		if sel.Revision != before {
			return ErrSelectionChanged
		}
		var err error
		sel.IDs, err = queryAll(ctx, tx, scanID, "SELECT id FROM assets WHERE "+where+" ORDER BY "+order, args...)
		return err
	})
	return sel, err
}

// ValidateSelection rejects stale query selections before batch admission.
func (s *Store) ValidateSelection(ctx context.Context, sel core.Selection) error {
	var revision int64
	if err := s.r.QueryRowContext(ctx, selectionRevisionSQL).Scan(&revision); err != nil {
		return err
	}
	if revision != sel.Revision {
		return ErrSelectionChanged
	}
	return nil
}

const selectionRevisionSQL = "SELECT (SELECT value FROM selection_revision WHERE id = 1) + visible_value FROM event_revision WHERE id = 1"

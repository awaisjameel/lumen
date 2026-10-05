package store

import (
	"context"
	"database/sql"

	"lumen/internal/core"
)

// Adjustment reads presentation metadata under the visibility predicate.
func (s *Store) Adjustment(ctx context.Context, id int64, unlocked bool) (core.DisplayAdjustment, error) {
	var a core.DisplayAdjustment
	err := s.r.QueryRowContext(ctx, "SELECT display_rotation, display_flip, display_version FROM assets WHERE id = ? AND "+servableOf(unlocked), id).Scan(&a.Rotation, &a.Flip, &a.Version)
	if err == sql.ErrNoRows {
		return a, ErrNotFound
	}
	return a, err
}

// SetAdjustment stores a presentation change without altering source pixels.
func (s *Store) SetAdjustment(ctx context.Context, id int64, rotation int, flip bool) (core.DisplayAdjustment, error) {
	rotation = ((rotation % 4) + 4) % 4
	err := s.tx(ctx, func(tx *sql.Tx) error {
		return affected(tx.ExecContext(ctx, "UPDATE assets SET display_rotation = ?, display_flip = ?, display_version = display_version + 1 WHERE id = ? AND kind = 'photo' AND "+visible, rotation, flip, id))
	})
	if err != nil {
		return core.DisplayAdjustment{}, err
	}
	return s.Adjustment(ctx, id, false)
}

// PlaybackPosition reads history only for an ordinary visible video.
func (s *Store) PlaybackPosition(ctx context.Context, id int64) (int64, error) {
	var position int64
	err := s.r.QueryRowContext(ctx, "SELECT COALESCE((SELECT position_ms FROM playback_history WHERE asset_id = assets.id), 0) FROM assets WHERE id = ? AND kind = 'video' AND "+visible, id).Scan(&position)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return position, err
}

// SetPlaybackPosition bounds and records an ordinary visible video position.
func (s *Store) SetPlaybackPosition(ctx context.Context, id, position int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var duration int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(duration_ms, 0) FROM assets WHERE id = ? AND kind = 'video' AND "+visible, id).Scan(&duration); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		if position < 0 {
			position = 0
		}
		if duration > 0 {
			position = min(position, duration)
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO playback_history(asset_id, position_ms) VALUES (?, ?) ON CONFLICT(asset_id) DO UPDATE SET position_ms = excluded.position_ms", id, position)
		return err
	})
}

// ClearPlaybackHistory removes all saved resume positions.
func (s *Store) ClearPlaybackHistory(ctx context.Context) error {
	_, err := s.w.ExecContext(ctx, "DELETE FROM playback_history")
	return err
}

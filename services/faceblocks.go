package services

import (
	"context"
	"errors"

	"lumen/internal/core"
	"lumen/internal/store"
)

// BlockFace blocks one face, and with lookalikes the faces like it too, in
// existing and future photos. It behaves as SetBlock does: it needs the PIN
// to have been created, offers Undo for a new block only, and refuses to
// change an existing block's mode while the Blocked screen is locked. Asking
// again for a face already blocked widens its block to look-alikes (that
// hides more, so it needs no unlock) and otherwise returns it as it is.
func (s *LibraryService) BlockFace(ctx context.Context, faceID int64, mode core.BlockMode, lookalikes bool) (core.BlockChange, error) {
	configured, err := s.store.HasBlockPIN(ctx)
	if err != nil {
		return core.BlockChange{}, err
	}
	if !configured {
		return core.BlockChange{}, ErrBlockLocked
	}
	s.blocked.Lock()
	defer s.blocked.Unlock()
	id, created, err := s.store.SetFaceBlock(ctx, faceID, mode, lookalikes, s.unlockedNow())
	if errors.Is(err, store.ErrBlockChangeLocked) {
		return core.BlockChange{}, ErrBlockLocked
	}
	if err == nil {
		if created {
			s.blocked.lastID, s.blocked.lastAt = id, s.now()
		} else if id == s.blocked.lastID {
			s.blocked.lastID = 0
		}
		s.viewsChanged()
	}
	return core.BlockChange{ID: id, Created: created}, err
}

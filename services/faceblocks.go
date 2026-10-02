package services

import (
	"context"

	"lumen/internal/core"
)

// BlockFace blocks one face, and with lookalikes the faces like it too, in
// existing and future photos. It behaves as SetBlock does: it needs the PIN
// to have been created, offers Undo for a new block only, and refuses to
// change an existing block's mode while the Blocked screen is locked. Asking
// again for a face already blocked widens its block to look-alikes (that
// hides more, so it needs no unlock), narrows it to the face alone only while
// unlocked (that reveals photos, like a mode change), and otherwise returns it
// as it is.
func (s *LibraryService) BlockFace(ctx context.Context, faceID int64, mode core.BlockMode, lookalikes bool) (core.BlockChange, error) {
	return s.saveBlock(ctx, func(allowChange bool) (int64, bool, error) {
		return s.store.SetFaceBlock(ctx, faceID, mode, lookalikes, allowChange)
	})
}

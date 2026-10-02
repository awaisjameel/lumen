package services

import (
	"context"
	"errors"

	"lumen/internal/core"
)

// unbuilt is what a Phase 5 method answers until its unit lands: the lead
// declares each new method first, so the bindings and the bridge are fixed
// before the units are built in parallel. A unit moves its methods out of
// this file into its own; the file goes once none is left.
func unbuilt(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.ErrUnsupported
}

// BlockFace blocks one face, and with lookalikes the faces like it too, in
// existing and future photos. Phase 5 unit U7.
func (s *LibraryService) BlockFace(ctx context.Context, faceID int64, mode core.BlockMode, lookalikes bool) (core.BlockChange, error) {
	_, _, _ = faceID, mode, lookalikes
	return core.BlockChange{}, unbuilt(ctx)
}

// BlockedItems lists the photos the block blockID covers, for the Blocked
// screen's browser; it needs the PIN unlock. Phase 5 unit U8.
func (s *LibraryService) BlockedItems(ctx context.Context, blockID int64) ([]core.CoveredItem, error) {
	_ = blockID
	return nil, unbuilt(ctx)
}

// TrashCovered moves photos a block covers to the Trash, from the Blocked
// screen's browser; it needs the PIN unlock. Phase 5 unit U8.
func (s *LibraryService) TrashCovered(ctx context.Context, ids []int64) error {
	_ = ids
	return unbuilt(ctx)
}

// DeleteCopies moves every file of the asset assetID except keep to the
// Windows Recycle Bin: the extra files of exact copies. Phase 5 unit U9.
func (s *LibraryService) DeleteCopies(ctx context.Context, assetID int64, keep string) (core.DiskDeletion, error) {
	_, _ = assetID, keep
	return core.DiskDeletion{}, unbuilt(ctx)
}

// PlanOrganise previews where template would move the files of the source
// sourceID (0: every source), and saves the preview as a planned run.
// Phase 5 unit U10.
func (s *LibraryService) PlanOrganise(ctx context.Context, template string, sourceID int64) (core.OrganisePlan, error) {
	_, _ = template, sourceID
	return core.OrganisePlan{}, unbuilt(ctx)
}

// StartOrganise makes (or resumes) the moves of the run runID. Phase 5 unit U10.
func (s *LibraryService) StartOrganise(ctx context.Context, runID int64) error {
	_ = runID
	return unbuilt(ctx)
}

// UndoOrganise moves the files of the run runID back. Phase 5 unit U10.
func (s *LibraryService) UndoOrganise(ctx context.Context, runID int64) error {
	_ = runID
	return unbuilt(ctx)
}

// OrganiseRuns lists the runs, newest first. Phase 5 unit U10.
func (s *LibraryService) OrganiseRuns(ctx context.Context) ([]core.OrganiseRun, error) {
	return nil, unbuilt(ctx)
}

// SetXMP turns writing XMP sidecars on or off. Phase 5 unit U11.
func (s *LibraryService) SetXMP(ctx context.Context, on bool) error {
	_ = on
	return unbuilt(ctx)
}

// ExportXMPNow writes every photo's sidecar that is out of date now.
// Phase 5 unit U11.
func (s *LibraryService) ExportXMPNow(ctx context.Context) (core.XMPExport, error) {
	return core.XMPExport{}, unbuilt(ctx)
}

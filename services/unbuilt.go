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

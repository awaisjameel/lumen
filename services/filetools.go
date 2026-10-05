package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
)

// PlanFileMoves previews one selected copy per asset. Explicit moves share
// organise's journal, scan hold, recovery, progress and Undo.
func (s *LibraryService) PlanFileMoves(ctx context.Context, ids []int64, directory, name string) (core.OrganisePlan, error) {
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	if len(ids) == 0 || (name != "" && len(ids) != 1) {
		return core.OrganisePlan{}, store.ErrNotFound
	}
	if name != "" && (filepath.Base(name) != name || strings.ContainsAny(name, `<>:"/\|?*`) || name == "." || name == "..") {
		return core.OrganisePlan{}, errors.New("choose a valid file name")
	}
	snap, err := s.store.SelectedFiles(ctx, ids)
	if err != nil {
		return core.OrganisePlan{}, err
	}
	paths := map[int64]string{}
	seen := map[int64]bool{}
	for _, f := range snap.Files {
		if seen[f.AssetID] {
			continue
		}
		seen[f.AssetID] = true
		dir, base := directory, filepath.Base(f.Path)
		if dir == "" {
			dir = filepath.Dir(f.Path)
		}
		if name != "" {
			base = name
		}
		paths[f.FileID] = filepath.Join(dir, base)
	}
	if len(seen) != len(ids) {
		return core.OrganisePlan{}, store.ErrNotFound
	}
	destinations := make([]string, 0, len(paths))
	for _, path := range paths {
		destinations = append(destinations, path)
	}
	snap.Known, err = s.store.KnownDestinations(ctx, destinations)
	if err != nil {
		return core.OrganisePlan{}, err
	}
	p, err := fileops.PlanPaths(ctx, snap, paths)
	if err != nil {
		return core.OrganisePlan{}, err
	}
	run, err := s.store.SaveOrganisePlan(ctx, "Selected files", p.Moves)
	if err != nil {
		return core.OrganisePlan{}, err
	}
	moves := make([]core.OrganiseMove, len(p.Moves))
	for i, m := range p.Moves {
		moves[i] = organiseMoveOf(m)
	}
	return core.OrganisePlan{RunID: run, Template: "Selected files", Moves: moves, InPlace: p.InPlace, Sidecars: p.Sidecars}, nil
}

// FileMoveOutcomes returns current journal rows without disclosing covered paths.
func (s *LibraryService) FileMoveOutcomes(ctx context.Context, runID int64) ([]core.OrganiseMove, error) {
	moves, err := s.store.VisibleOrganiseMoves(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := make([]core.OrganiseMove, len(moves))
	for i, m := range moves {
		out[i] = organiseMoveOf(m)
	}
	return out, nil
}

// OpenMedia hands one validated visible file copy to the Windows shell.
func (s *LibraryService) OpenMedia(ctx context.Context, id int64, path string, choose bool) error {
	f, err := s.store.AssetFile(ctx, id, false)
	if err != nil {
		return err
	}
	if path == "" {
		var ok bool
		path, ok = readablePath(f)
		if !ok {
			return store.ErrNotFound
		}
	}
	if !slices.Contains(f.Paths, path) {
		return store.ErrNotFound
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return store.ErrNotFound
	}
	return fileops.Open(path, choose)
}

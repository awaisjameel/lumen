package fileops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"lumen/internal/core"
	"lumen/internal/scanner"
	"lumen/internal/store"
)

// PlanPaths routes explicit rename/move actions into the existing durable
// journal. They retain the source ownership and same-volume Undo rules.
func PlanPaths(ctx context.Context, snap store.OrganiseSnapshot, paths map[int64]string) (Planned, error) {
	p := planner{snap: snap, reserved: map[string]bool{}, dirs: map[string]dirState{}}
	result := Planned{}
	for _, f := range snap.Files {
		to, ok := paths[f.FileID]
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		to = filepath.Clean(to)
		base := filepath.Base(to)
		if isReserved(base) || strings.HasSuffix(base, ".") || strings.HasSuffix(base, " ") || strings.ContainsFunc(base, forbiddenRune) || len(utf16.Encode([]rune(to+SidecarExt))) > maxPathLen {
			return result, ErrInvalidTemplate
		}
		if !filepath.IsAbs(to) || !core.IsWithin(to, f.Root) || !strings.EqualFold(filepath.Ext(to), filepath.Ext(f.Path)) {
			return result, errors.New("the destination must keep the format and stay inside its source folder")
		}
		if core.Fold(to) == core.Fold(f.Path) {
			result.InPlace++
			continue
		}
		if !p.folderUsable(f.Root, filepath.Dir(to)) {
			return result, errors.New("the destination crosses a link or is not a folder")
		}
		if _, _, excluded := scanner.Exclusion(f.Root, to, nil); excluded {
			return result, errors.New("the destination is excluded from indexing")
		}
		for _, blocked := range snap.BlockedDirs {
			if core.IsWithin(to, blocked) {
				return result, errors.New("the destination is blocked")
			}
		}
		if p.taken(core.Fold(to), to) || p.taken(core.Fold(sidecarOf(to)), sidecarOf(to)) {
			return result, ErrExists
		}
		fi, err := os.Lstat(f.Path)
		if err != nil {
			return result, err
		}
		if !fi.Mode().IsRegular() || fi.Size() != f.Size || fi.ModTime().UnixNano() != f.MTime {
			return result, errors.New("the source changed; preview again")
		}
		m := store.OrganiseMove{FileID: f.FileID, AssetID: f.AssetID, Root: f.Root, From: f.Path, To: to, Size: f.Size, MTime: f.MTime, State: core.MovePlanned}
		if present, _ := exists(sidecarOf(f.Path)); present {
			m.Sidecar = store.SidecarPlanned
			result.Sidecars++
		}
		result.Moves = append(result.Moves, m)
		p.reserved[core.Fold(to)] = true
		p.reserved[core.Fold(sidecarOf(to))] = true
	}
	return result, nil
}

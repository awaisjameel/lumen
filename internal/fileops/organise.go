package fileops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"lumen/internal/core"
	"lumen/internal/store"
)

// ErrExists means a move found something at its destination and left it
// alone: moves never replace.
var ErrExists = errors.New("something is already at the destination")

// Why a file is left out of a plan (as a skipped move) or skipped when a run
// reaches it. They are the Error of the move, and frontend/src/bridge
// mirrors them so that the UI can explain each in its own words; anything
// else in a move's Error is an operating system message.
const (
	ReasonMissing       = store.MoveRefusedMissing       // the file is not at its place on disk (or in the library)
	ReasonNotVisible    = store.MoveRefusedNotVisible    // blocked, trashed or hidden since the preview
	ReasonBlockedFolder = store.MoveRefusedBlockedFolder // the destination folder is blocked
	ReasonChanged       = "changed"                      // its size or time differ from the preview
	ReasonTaken         = "taken"                        // something appeared at the destination
	ReasonTooLong       = "too_long"                     // the new path would pass what Windows allows
	ReasonNotAFolder    = "not_a_folder"                 // a file, link or junction is in the way of a folder
	// ReasonSidecarTaken is on a move whose photo moved but whose sidecar
	// stayed, because something is at the sidecar's new name.
	ReasonSidecarTaken = "sidecar_taken"
)

// SidecarExt follows a photo's whole file name to name its XMP sidecar
// (IMG_1.jpg.xmp), as Lumen's own export and other programs write it.
const SidecarExt = ".xmp"

// sidecarOf is the name of a photo's sidecar. The file system folds case, so
// a sidecar spelt ".XMP" is the one this name finds.
func sidecarOf(photo string) string { return photo + SidecarExt }

// maxCollisions is how many same-named files one folder takes before the
// planner gives up on a name: far more than a folder of photos holds.
const maxCollisions = 9999

// collisionSuffix is what a name takes when n-1 others have it: " (2)".
func collisionSuffix(n int) string {
	if n <= 1 {
		return ""
	}
	return " (" + strconv.Itoa(n) + ")"
}

// Planned is a dry run: the moves a template would make.
type Planned struct {
	// Moves are the files that would move (planned) and those left out with a
	// reason (skipped), in the order of the snapshot. Files already where the
	// template puts them are not among them.
	Moves   []store.OrganiseMove
	InPlace int // files already where the template puts them
	Renamed int // moves whose name took a " (2)" for a collision
	// Sidecars is how many of the moves take an XMP sidecar along: one is
	// found beside the photo, and nothing is at its new name.
	Sidecars int
}

// Plan works out where template would put every file of the snapshot. Each
// file stays inside its own source. A destination is never a path anything
// else has: a file on disk, a file the library knows, another planned file,
// or a case-folded twin of one of them all push the name on to " (2)",
// " (3)"…; so nothing is overwritten. The disk is only read.
func Plan(ctx context.Context, template string, snap store.OrganiseSnapshot) (Planned, error) {
	tmpl, err := ParseTemplate(template)
	if err != nil {
		return Planned{}, err
	}
	p := planner{snap: snap, reserved: map[string]bool{}, dirs: map[string]dirState{}}
	out := Planned{Moves: []store.OrganiseMove{}}
	for i, f := range snap.Files {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return Planned{}, err
			}
		}
		move, inPlace := p.plan(tmpl, f)
		switch {
		case inPlace:
			out.InPlace++
		case move.State == core.MovePlanned:
			out.Moves = append(out.Moves, move)
			if move.Renamed {
				out.Renamed++
			}
			if move.Sidecar == store.SidecarPlanned {
				out.Sidecars++
			}
		default:
			out.Moves = append(out.Moves, move)
		}
	}
	return out, nil
}

// planner holds what planning one file needs to know about the others.
type planner struct {
	snap     store.OrganiseSnapshot
	reserved map[string]bool     // folded destinations of the files planned so far
	dirs     map[string]dirState // folded folder -> what is at that path
}

type dirState int

const (
	dirFolder  dirState = iota // a real folder
	dirMissing                 // nothing there
	dirBlocked                 // a file, link or junction, or unreadable: nothing may go through it
)

// plan decides the destination of one file. inPlace reports a file that is
// already where the template puts it.
func (p *planner) plan(tmpl Template, f store.OrganiseFile) (move store.OrganiseMove, inPlace bool) {
	move = store.OrganiseMove{FileID: f.FileID, AssetID: f.AssetID, From: f.Path, State: core.MovePlanned}
	skip := func(reason string) (store.OrganiseMove, bool) {
		move.State, move.Error = core.MoveSkipped, reason
		return move, false
	}
	ext := filepath.Ext(f.Path)
	r := tmpl.Render(Values{
		Taken: time.Unix(f.TakenAt, 0).UTC(), Event: f.Event, Place: f.Place, Camera: f.Camera,
		Stem: strings.TrimSuffix(filepath.Base(f.Path), ext), Ext: ext,
	})
	dir := filepath.Join(append([]string{f.Root}, r.Dirs...)...)
	for _, blocked := range p.snap.BlockedDirs {
		if core.IsWithin(dir, blocked) {
			move.To = filepath.Join(dir, r.Stem+r.Ext)
			return skip(ReasonBlockedFolder)
		}
	}

	own := core.Fold(f.Path)
	for n := 1; ; n++ {
		if n > maxCollisions {
			move.To = filepath.Join(dir, r.Stem+r.Ext)
			return skip(ReasonTaken)
		}
		candidate := filepath.Join(dir, r.Stem+collisionSuffix(n)+r.Ext)
		key := core.Fold(candidate)
		if key == own {
			return move, true // it is already here (under the name a collision gave it, perhaps)
		}
		if p.taken(key, candidate) {
			continue
		}
		move.To, move.Renamed = candidate, n > 1
		break
	}

	switch {
	case len(utf16.Encode([]rune(move.To))) > maxPathLen:
		return skip(ReasonTooLong)
	case !p.folderUsable(f.Root, dir):
		return skip(ReasonNotAFolder)
	}
	info, err := os.Lstat(f.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return skip(ReasonMissing)
	case err != nil:
		return skip(err.Error())
	case info.Size() != f.Size || info.ModTime().UnixNano() != f.MTime:
		// The library has not caught up with the file yet; it is not moved
		// on a guess about what is in it.
		return skip(ReasonChanged)
	}
	move.Size, move.MTime = info.Size(), info.ModTime().UnixNano()
	if present, _ := exists(sidecarOf(f.Path)); present {
		// The photo moves either way; its sidecar stays if its new name is taken.
		if taken, _ := exists(sidecarOf(move.To)); taken {
			move.Sidecar, move.Error = store.SidecarKept, ReasonSidecarTaken
		} else {
			move.Sidecar = store.SidecarPlanned
		}
	}
	p.reserved[core.Fold(move.To)] = true
	return move, false
}

// taken reports whether a name is not free: planned for another file, a
// file the library has, or anything on disk (a case-folded twin included: the
// file system folds as Windows does). A path that cannot be read counts as
// taken, to be safe.
func (p *planner) taken(key, path string) bool {
	if p.reserved[key] || p.snap.Known[key] {
		return true
	}
	_, err := os.Lstat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// folderUsable reports whether dir, inside root, can be made or used: no
// folder on the way down is a file, a link or a junction, which would take a
// file out of its source or make a folder impossible.
func (p *planner) folderUsable(root, dir string) bool {
	var chain []string // dir and its parents, down to just below root
	for d := dir; core.Fold(d) != core.Fold(root) && core.IsWithin(d, root); d = filepath.Dir(d) {
		chain = append(chain, d)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		state := p.dirState(chain[i])
		if state == dirBlocked {
			return false
		}
		if state == dirMissing {
			return true // nothing below a folder that is not there can be in the way
		}
	}
	return true
}

func (p *planner) dirState(dir string) dirState {
	key := core.Fold(dir)
	if s, ok := p.dirs[key]; ok {
		return s
	}
	s := dirFolder
	switch info, err := os.Lstat(dir); {
	case errors.Is(err, fs.ErrNotExist):
		s = dirMissing
	case err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
		s = dirBlocked
	}
	p.dirs[key] = s
	return s
}

// describe is a move for log lines and errors: never the paths of photos,
// which may be blocked ones.
func describe(m store.OrganiseMove) string {
	return fmt.Sprintf("move %d of asset %d", m.Seq, m.AssetID)
}

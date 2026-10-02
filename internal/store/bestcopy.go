package store

import (
	"cmp"
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"lumen/internal/core"
)

// The best-copy rule decides which photo of a duplicate group Review keeps.
// Two items compare by these steps, the first that tells them apart deciding:
//
//  1. resolution (width × height), higher first;
//  2. format and re-save signals, each deciding only if the ones before it tie:
//     an original-camera file (EXIF names a camera and gave the date) before
//     one without; then lossless or camera-native formats (PNG, TIFF, HEIC,
//     HEIF, AVIF) before JPEG before every other format (WebP, GIF, BMP); then
//     a file with a messenger re-save name (IMG-20240101-WA0001, "photo (1)",
//     "photo-edited") after one without;
//  3. file size, larger first;
//  4. metadata completeness: an EXIF date, GPS and a camera, one point each,
//     more points first;
//  5. the oldest file.
//
// If every step ties, the lower id wins, so the answer never depends on the
// order the rows came in. The rule only suggests: nothing is removed until
// the user confirms.
//
// Exact copies are one photo in several files with identical bytes, so only
// the path can differ: the oldest file is kept, then the shortest path.

// formatRank orders file formats: lossless and camera-native first, then
// JPEG, then everything else (WebP, GIF, BMP). ext is lower case with its dot.
func formatRank(ext string) int {
	switch strings.ToLower(ext) {
	case ".png", ".tif", ".tiff", ".heic", ".heif", ".avif":
		return 2
	case ".jpg", ".jpeg", ".jpe":
		return 1
	}
	return 0
}

// resaveName matches the names messengers and editors give to a copy of a
// photo, without its extension: WhatsApp's IMG-20240101-WA0001, a numbered
// copy "photo (1)", and an edited export "photo-edited".
var resaveName = regexp.MustCompile(`(?i)^(?:img-\d{8}-wa\d+|.*\s\(\d+\)|.*-edited)$`)

// fileName is the last element of a Windows or slash-separated path.
func fileName(path string) string {
	return path[strings.LastIndexAny(path, `\/`)+1:]
}

// resaved is true when every file of the item carries a re-save name: an item
// that also has a file under its camera name is not a re-save, since that
// file can be kept.
func resaved(it core.DuplicateItem) bool {
	if len(it.Paths) == 0 {
		return false
	}
	return !slices.ContainsFunc(it.Paths, func(p string) bool {
		name := fileName(p)
		return !resaveName.MatchString(strings.TrimSuffix(name, filepath.Ext(name)))
	})
}

// original is true for a file straight from a camera: EXIF names the camera
// and gave the date.
func original(it core.DuplicateItem) bool { return it.Camera && it.ExifDate }

// completeness is the metadata points an item holds: EXIF date, GPS, camera.
func completeness(it core.DuplicateItem) int {
	n := 0
	for _, has := range []bool{it.ExifDate, it.GPS, it.Camera} {
		if has {
			n++
		}
	}
	return n
}

// oldest is the earliest modified time of the item's files in Unix seconds,
// or, with none recorded, when Lumen first saw it.
func oldest(it core.DuplicateItem) int64 {
	if i := mtimeOf(it, -1); i < math.MaxInt64 {
		return i
	}
	return it.AddedAt
}

// mtimeOf is the modified time of the item's file i, or the earliest of them
// for i < 0; a file without a recorded time (zero or less) is the newest
// possible, so it never wins a tie by accident.
func mtimeOf(it core.DuplicateItem, i int) int64 {
	known := func(m int64) int64 {
		if m <= 0 {
			return math.MaxInt64
		}
		return m
	}
	if i >= 0 {
		if i >= len(it.Mtimes) {
			return math.MaxInt64
		}
		return known(it.Mtimes[i])
	}
	earliest := int64(math.MaxInt64)
	for _, m := range it.Mtimes {
		earliest = min(earliest, known(m))
	}
	return earliest
}

// betterCopy is negative when a is the better copy to keep, positive when b
// is, and never zero for two items with different ids.
func betterCopy(a, b core.DuplicateItem) int {
	return cmp.Or(
		cmp.Compare(int64(b.Width)*int64(b.Height), int64(a.Width)*int64(a.Height)),
		cmp.Compare(boolRank(original(b)), boolRank(original(a))),
		cmp.Compare(formatRank(b.Ext), formatRank(a.Ext)),
		cmp.Compare(boolRank(resaved(a)), boolRank(resaved(b))),
		cmp.Compare(b.Size, a.Size),
		cmp.Compare(completeness(b), completeness(a)),
		cmp.Compare(oldest(a), oldest(b)),
		cmp.Compare(a.ID, b.ID),
	)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// bestOf is the index of the item the rule keeps, or -1 for no items.
func bestOf(items []core.DuplicateItem) int {
	if len(items) == 0 {
		return -1
	}
	best := 0
	for i := 1; i < len(items); i++ {
		if betterCopy(items[i], items[best]) < 0 {
			best = i
		}
	}
	return best
}

// keptPath is the file of an item's identical copies to keep: the oldest,
// then the shortest path, then the first in path order. It is "" for an item
// with no files.
func keptPath(it core.DuplicateItem) string {
	keep := -1
	for i, p := range it.Paths {
		if keep < 0 {
			keep = i
			continue
		}
		if cmp.Or(
			cmp.Compare(mtimeOf(it, i), mtimeOf(it, keep)),
			cmp.Compare(utf8.RuneCountInString(p), utf8.RuneCountInString(it.Paths[keep])),
			strings.Compare(p, it.Paths[keep]),
		) < 0 {
			keep = i
		}
	}
	if keep < 0 {
		return ""
	}
	return it.Paths[keep]
}

// markBest sets Best on one item of every group and, in an exact group, Keep
// to the path to keep.
func markBest(groups []core.DuplicateGroup) {
	for g := range groups {
		items := groups[g].Items
		best := bestOf(items)
		if best < 0 {
			continue
		}
		items[best].Best = true
		if groups[g].Kind == core.DuplicateExact {
			items[best].Keep = keptPath(items[best])
		}
	}
}

// ExtraCopies returns the files of the asset assetID other than keep, the
// ones DeleteCopies moves to the Recycle Bin. The asset must be one the
// library shows (not hidden, trashed, missing or blocked), and keep must be
// one of its files; otherwise the call fails with ErrNotFound and names
// nothing, so a stale or forged request can never reach another photo's
// files. Files come in path order.
func (s *Store) ExtraCopies(ctx context.Context, assetID int64, keep string) ([]TrashedFile, error) {
	var extra []TrashedFile
	err := s.read(ctx, func(tx *sql.Tx) error {
		var shown int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM assets a WHERE a.id = ? AND "+visibleA, assetID).Scan(&shown); err != nil {
			return err
		}
		if shown == 0 {
			return ErrNotFound
		}
		files, err := queryAll(ctx, tx, func(r *sql.Rows) (f TrashedFile, err error) {
			err = r.Scan(&f.ID, &f.AssetID, &f.Path)
			return f, err
		}, "SELECT id, asset_id, path FROM files WHERE asset_id = ? ORDER BY path, id", assetID)
		if err != nil {
			return err
		}
		kept := false
		for _, f := range files {
			if core.Fold(f.Path) == core.Fold(keep) {
				kept = true
			} else {
				extra = append(extra, f)
			}
		}
		if !kept {
			extra = nil
			return ErrNotFound
		}
		return nil
	})
	return extra, err
}

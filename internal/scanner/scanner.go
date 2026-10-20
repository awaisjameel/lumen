// Package scanner finds media files in the user's folders, fingerprints them
// and keeps the database in step with the disk. It only ever reads files.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zeebo/xxh3"

	"lumen/internal/core"
	"lumen/internal/store"
)

// Options control what a scan picks up.
type Options struct {
	Videos   bool     // videos are opt-in (off by default)
	SkipDirs []string // e.g. Lumen's own data folder
	// Excluded are the formats the user turned off: their files are not added
	// or read again, and those the library already holds are kept as they are.
	Excluded []core.Family
}

// Result summarises one scan.
type Result struct {
	Seen      int // media files found on disk
	Changed   int // new or modified files that were hashed
	NewAssets int // of which were content Lumen had never seen
	Removed   int // files gone from disk
}

// Scanner walks sources. Progress and ingest callbacks may be nil.
type Scanner struct {
	Store       *store.Store
	HashWorkers int
	OnProgress  func(seen int)      // called now and then while walking
	OnIngested  func(newAssets int) // called after each committed batch
	seekPenalty func(context.Context, string) (bool, error)
}

const ingestBatch = 256

type found struct {
	path  string
	size  int64
	mtime time.Time
	kind  core.Kind
}

// Scan brings one source, or just the folder under inside it, in line with
// the disk. Unchanged files (same size and modified time) cost one stat.
func (s *Scanner) Scan(ctx context.Context, src core.Source, under string, opt Options) (Result, error) {
	if err := CheckRoot(ctx, src.Path); err != nil {
		return Result{}, err
	}
	root := src.Path
	if under != "" {
		if Excluded(src.Path, under, opt.SkipDirs) {
			return Result{}, nil // a full scan leaves it out too
		}
		root = under
	}
	// A source can be a junction or symbolic link to its folder (a moved
	// Pictures folder often is). The walk reads the folder it points to, and
	// reports paths under the source's own name.
	walkRoot, ok, err := walkable(root, under == "")
	if !ok {
		return Result{}, err
	}
	known, err := s.Store.KnownFiles(ctx, src.ID, under)
	if err != nil {
		return Result{}, fmt.Errorf("load known files: %w", err)
	}
	// If saving fails, stop the walk and the hashing rather than finish for nothing.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var res Result
	toHash := make(chan found, 512)
	hashed := make(chan store.IngestItem, 512)

	// Hash workers: I/O bound, a few in parallel.
	var hw sync.WaitGroup
	for range s.hashWorkers(ctx, walkRoot) {
		hw.Go(func() {
			for f := range toHash {
				sum, err := Fingerprint(ctx, f.path, f.size)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					slog.Warn("fingerprint failed", "path", f.path, "err", err)
					continue
				}
				takenAt, from := core.GuessDate(filepath.Base(f.path), f.mtime)
				select {
				case hashed <- store.IngestItem{
					SourceID: src.ID, Path: f.path, Size: f.size, MTime: f.mtime.UnixNano(),
					Hash: sum, Kind: f.kind, Ext: filepath.Ext(f.path), TakenAt: takenAt, DateFrom: from,
				}:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	go func() { hw.Wait(); close(hashed) }()
	defer hw.Wait()

	// Collector: commit in batches, or every half second so the grid fills in live.
	ingestErr := make(chan error, 1)
	go func() {
		var batch []store.IngestItem
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		fail := func(err error) {
			cancel()
			for range hashed { // drain so hash workers can exit
			}
			ingestErr <- err
		}
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			n, err := s.Store.Ingest(ctx, batch)
			if err != nil {
				return err
			}
			res.Changed += len(batch)
			res.NewAssets += n
			batch = batch[:0]
			if s.OnIngested != nil {
				s.OnIngested(n)
			}
			return nil
		}
		for {
			select {
			case it, ok := <-hashed:
				if !ok {
					ingestErr <- flush()
					return
				}
				if batch = append(batch, it); len(batch) >= ingestBatch {
					if err := flush(); err != nil {
						fail(err)
						return
					}
				}
			case <-tick.C:
				if err := flush(); err != nil {
					fail(err)
					return
				}
			}
		}
	}()

	walkErr := core.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		isRoot := path == walkRoot
		path = root + path[len(walkRoot):]
		if err != nil {
			if isRoot {
				return err
			}
			slog.Debug("skip unreadable", "path", path, "err", err)
			if !errors.Is(err, fs.ErrNotExist) {
				preserveKnown(known, path)
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !isRoot && skipDir(path, d, opt.SkipDirs) {
				return fs.SkipDir
			}
			return nil
		}
		kind := core.KindOf(path)
		if kind == "" || (kind == core.KindVideo && !opt.Videos) || d.Type()&fs.ModeSymlink != 0 {
			return nil // a link's own size and time say nothing about its target
		}
		info, err := d.Info()
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				preserveKnown(known, path)
			}
			return nil // unavailable since it was listed: nothing to add
		}
		if skipName(d.Name()) || isHiddenOrSystem(info) {
			return nil // e.g. macOS's "._IMG_0001.JPG" beside the real photo
		}
		if notTransportStream(path) {
			return nil // e.g. a TypeScript module (.mts), which a library leaves forgotten
		}
		key := core.Fold(path)
		if slices.Contains(opt.Excluded, core.FamilyOf(path)) {
			// Still on disk, so not gone: the library keeps what it knew of it.
			delete(known, key)
			return nil
		}
		res.Seen++
		if s.OnProgress != nil && res.Seen%500 == 0 {
			s.OnProgress(res.Seen)
		}
		prev, ok := known[key]
		delete(known, key)
		if ok && prev.Size == info.Size() && prev.MTime == info.ModTime().UnixNano() {
			return nil
		}
		select {
		case toHash <- found{path: path, size: info.Size(), mtime: info.ModTime(), kind: kind}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	close(toHash)
	if err := <-ingestErr; err != nil {
		return res, fmt.Errorf("save scan results: %w", err)
	}
	if walkErr != nil {
		// Don't treat an interrupted or unreadable walk as "everything was deleted".
		return res, walkErr
	}
	if s.OnProgress != nil {
		s.OnProgress(res.Seen)
	}

	// A drive can disappear halfway through an otherwise successful walk.
	// Absence inside it is only evidence of deletion while its root is readable.
	if err := CheckRoot(ctx, src.Path); err != nil {
		return res, err
	}

	// Whatever is left in known was not found on disk.
	gone := make([]int64, 0, len(known))
	for _, st := range known {
		gone = append(gone, st.ID)
	}
	for i := 0; i < len(gone); i += 1000 {
		if err := s.Store.RemoveFiles(ctx, gone[i:min(i+1000, len(gone))]); err != nil {
			return res, fmt.Errorf("forget removed files: %w", err)
		}
	}
	res.Removed = len(gone)
	return res, nil
}

// An unreadable path is not evidence of deletion. Remove its entries from
// the reconciliation candidates while retaining them in the database.
func preserveKnown(known map[string]store.FileStat, path string) {
	for key := range known {
		if core.IsWithin(key, path) {
			delete(known, key)
		}
	}
}

// skipDir leaves out hidden and system folders, and any folder in skip.
func skipDir(path string, d fs.DirEntry, skip []string) bool {
	if skipName(d.Name()) || withinAny(path, skip) {
		return true
	}
	info, err := d.Info()
	return err == nil && isHiddenOrSystem(info)
}

// skipName reports whether a file or folder name marks it as not the user's
// media: dot names (hidden by convention, and AppleDouble "._" files), "$"
// names (the recycle bin) and Windows' own folder.
func skipName(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") ||
		strings.EqualFold(name, "System Volume Information")
}

// SkipName is skipName for fileops: organising must never give a file or a
// folder a name the next full scan would leave out, which would make the
// library forget the file.
func SkipName(name string) bool { return skipName(name) }

func withinAny(path string, dirs []string) bool {
	for _, d := range dirs {
		if core.IsWithin(path, d) {
			return true
		}
	}
	return false
}

// Excluded reports whether a full scan of root would leave path out: some
// folder between them (or path itself) is hidden, a system folder, or in
// skip. The indexer checks folder changes with it before scanning them.
func Excluded(root, path string, skip []string) bool {
	_, _, excluded := Exclusion(root, path, skip)
	return excluded
}

// Exclusion is Excluded, also saying which folder (or file) inside root is
// left out, and whether that is for its hidden or system attribute (which the
// user can change in Explorer) rather than its name or place.
func Exclusion(root, path string, skip []string) (at string, byAttribute, excluded bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, false
	}
	cur := root
	for name := range strings.SplitSeq(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, name)
		if skipName(name) || withinAny(cur, skip) {
			return cur, false, true
		}
		// A deleted path has no attributes left; its name rules still apply.
		if info, err := os.Lstat(cur); err == nil && isHiddenOrSystem(info) {
			return cur, true, true
		}
	}
	return "", false, false
}

// walkable returns the folder to walk for root. A source's root that is a
// junction or link resolves to its target (the walk does not follow links).
// A linked folder inside a source is not walked (ok is false): the source's
// full scan does not enter it either.
func walkable(root string, isSource bool) (walk string, ok bool, err error) {
	fi, err := os.Lstat(root)
	if err != nil || fi.IsDir() {
		return root, true, nil //nolint:nilerr // a missing root fails in the walk itself, which reports it
	}
	if !isSource {
		return "", false, nil
	}
	target, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false, fmt.Errorf("resolve %s: %w", root, err)
	}
	return target, true, nil
}

// notTransportStream reports whether a file with a transport stream's name
// (.ts, .mts, .m2ts: broadcast, AVCHD and Blu-ray video) isn't one: TypeScript
// gives its sources and modules the same names, and a downloads folder holding a project's
// node_modules offered hundreds of them as videos, each failing in every
// stage. A stream is packets with a 0x47 sync byte every 188 bytes, or every
// 192 after a 4-byte time code. Other files, and one that can't be read (its
// stages say why), are kept.
func notTransportStream(path string) bool {
	if core.FamilyOf(path) != core.FamilyTransport {
		return false
	}
	f, err := core.OpenShared(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [2*192 + 5]byte // through the third packet's sync byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) // too short to be a video
	}
	return (b[0] != 0x47 || b[188] != 0x47 || b[376] != 0x47) && (b[4] != 0x47 || b[196] != 0x47 || b[388] != 0x47)
}

// Files up to this size are hashed in full. Bigger ones (videos, mostly) are
// sampled: size plus three 4 MB windows, which is plenty to tell media apart
// and avoids reading gigabytes just to recognise a file.
const (
	fullHashLimit = 64 << 20
	sampleWindow  = 4 << 20
)

// Fingerprint returns the 16-byte content hash that identifies an asset,
// stopping between reads when ctx ends.
func Fingerprint(ctx context.Context, path string, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := core.OpenShared(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := xxh3.New128()
	if size <= fullHashLimit {
		if _, err := io.Copy(h, hashReader{ctx: ctx, r: f}); err != nil {
			return nil, fmt.Errorf("fingerprint %s: %w", path, err)
		}
	} else {
		fmt.Fprintf(h, "lumen-sampled:%d:", size)
		buf := make([]byte, sampleWindow)
		for _, off := range []int64{0, size/2 - sampleWindow/2, size - sampleWindow} {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			n, err := f.ReadAt(buf, off)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("fingerprint %s at %d: %w", path, off, err)
			}
			_, _ = h.Write(buf[:n]) // hashing into memory never fails
		}
	}
	sum := h.Sum128().Bytes()
	return sum[:], nil
}

// hashReader stops a full-file hash between reads when the scan is cancelled.
type hashReader struct {
	ctx context.Context
	r   io.Reader
}

func (r hashReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// CheckRoot verifies that a source is a readable directory, including an empty
// one. Call only in background work: filesystem calls can wait on a dead share.
func CheckRoot(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := core.OpenDirShared(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // The probe owns no writable data.
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source is not a directory: %s", path)
	}
	_, err = f.Readdirnames(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return ctx.Err()
}

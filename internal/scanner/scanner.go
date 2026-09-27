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
	"strings"
	"sync"
	"time"

	"github.com/zeebo/xxh3"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

// Options control what a scan picks up.
type Options struct {
	Videos   bool     // videos are opt-in (off by default)
	SkipDirs []string // e.g. Lumen's own data folder
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
	root := src.Path
	if under != "" {
		root = under
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
	for range max(1, s.HashWorkers) {
		hw.Go(func() {
			for f := range toHash {
				sum, err := Fingerprint(f.path, f.size)
				if err != nil {
					slog.Warn("fingerprint failed", "path", f.path, "err", err)
					continue
				}
				takenAt, from := media.GuessDate(filepath.Base(f.path), f.mtime)
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

	// Collector: commit in batches, or every half second so the grid fills in live.
	ingestErr := make(chan error, 1)
	go func() {
		var batch []store.IngestItem
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		fail := func(err error) {
			ingestErr <- err
			cancel()
			for range hashed { // drain so hash workers can exit
			}
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

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if path == root {
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
			if path != root && skipDir(path, d, opt.SkipDirs) {
				return fs.SkipDir
			}
			return nil
		}
		kind := core.KindOf(path)
		if kind == "" || (kind == core.KindVideo && !opt.Videos) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				preserveKnown(known, path)
			}
			return nil // unavailable since it was listed: nothing to add
		}
		res.Seen++
		if s.OnProgress != nil && res.Seen%500 == 0 {
			s.OnProgress(res.Seen)
		}
		key := strings.ToLower(path)
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
		if store.IsWithin(key, path) {
			delete(known, key)
		}
	}
}

// skipDir leaves out hidden and system folders, and any folder in skip.
func skipDir(path string, d fs.DirEntry, skip []string) bool {
	name := d.Name()
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") ||
		strings.EqualFold(name, "System Volume Information") {
		return true
	}
	for _, s := range skip {
		if store.IsWithin(path, s) {
			return true
		}
	}
	return isHiddenOrSystem(d)
}

// Files up to this size are hashed in full. Bigger ones (videos, mostly) are
// sampled: size plus three 4 MB windows, which is plenty to tell media apart
// and avoids reading gigabytes just to recognise a file.
const (
	fullHashLimit = 64 << 20
	sampleWindow  = 4 << 20
)

// Fingerprint returns the 16-byte content hash that identifies an asset.
func Fingerprint(path string, size int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := xxh3.New128()
	if size <= fullHashLimit {
		if _, err := io.Copy(h, f); err != nil {
			return nil, err
		}
	} else {
		fmt.Fprintf(h, "lumen-sampled:%d:", size)
		buf := make([]byte, sampleWindow)
		for _, off := range []int64{0, size/2 - sampleWindow/2, size - sampleWindow} {
			n, err := f.ReadAt(buf, off)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			_, _ = h.Write(buf[:n]) // hashing into memory never fails
		}
	}
	sum := h.Sum128().Bytes()
	return sum[:], nil
}

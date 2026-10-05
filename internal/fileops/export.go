package fileops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"lumen/internal/core"
	"lumen/internal/store"
)

// FreeName is name, or name with the organise planner's " (2)", " (3)"…
// before its extension: the first that nothing in dir has on disk and that
// reserved (folded paths already planned) does not hold.
func FreeName(dir, name string, reserved map[string]bool) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; n <= maxCollisions; n++ {
		candidate := filepath.Join(dir, stem+collisionSuffix(n)+ext)
		if reserved[core.Fold(candidate)] {
			continue
		}
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}
	return "", ErrExists
}

// ExportRunner publishes new copies from an adjacent temporary file. The
// journal records its digest before the no-replace rename, so a restart can
// distinguish a committed output from an unrelated file at the destination.
type ExportRunner struct {
	Store  *store.Store
	Render func(context.Context, core.ExportJob) ([]byte, error)
}

// Run publishes each pending item and records its independent outcome.
func (r *ExportRunner) Run(ctx context.Context, batch int64) error {
	jobs, err := r.Store.ExportJobs(ctx, batch)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.State != core.ExportPlanned && job.State != core.ExportFailed && job.State != core.ExportCancelled {
			continue
		}
		state := core.ExportDone
		err := r.one(ctx, job)
		if err != nil {
			var published *publishedExportError
			if errors.As(err, &published) {
				return err
			}
			state = core.ExportFailed
			if ctx.Err() != nil {
				state = core.ExportCancelled
			}
		}
		message := ""
		if err != nil {
			message = reasonOf(err)
		}
		// one settles published copies itself. An unsuccessful item still
		// receives an outcome when the batch was cancelled.
		if state != core.ExportDone {
			if err := r.Store.SetExportJob(context.WithoutCancel(ctx), job.ID, state, message, "", ""); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *ExportRunner) one(ctx context.Context, j core.ExportJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := r.Store.AssetFile(ctx, j.AssetID, false)
	if err != nil {
		return err
	}
	if !slices.Contains(f.Paths, j.From) {
		return store.ErrNotFound
	}
	if err := r.Store.ExportAllowed(ctx, j.AssetID, j.To); err != nil {
		return err
	}
	info, err := os.Lstat(j.From)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != j.Size || info.ModTime().UnixNano() != j.MTime {
		return errors.New("source changed; preview again")
	}
	if err := exportFolder(filepath.Dir(j.To)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(j.To), ".lumen-export-*")
	if err != nil {
		return err
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }() // The journal can recover any interrupted temporary file.
	if err := r.Store.SetExportJob(ctx, j.ID, core.ExportWriting, "", tmp.Name(), ""); err != nil {
		return err
	}
	hash := sha256.New()
	out := &checkedWriter{ctx: ctx, out: io.MultiWriter(tmp, hash)}
	if j.Options.Format == core.ExportOriginal {
		in, err := core.OpenShared(j.From)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		_ = in.Close()
		if err != nil {
			return err
		}
	} else {
		data, err := r.Render(ctx, j)
		if err != nil {
			return err
		}
		if _, err := out.Write(data); err != nil {
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	after, err := os.Stat(j.From)
	if err != nil {
		return err
	}
	if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return errors.New("source changed during export")
	}
	if err := r.Store.ExportAllowed(ctx, j.AssetID, j.To); err != nil {
		return err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if err := r.Store.SetExportJob(ctx, j.ID, core.ExportReady, "", tmp.Name(), digest); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := Move(tmp.Name(), j.To); err != nil {
		return err
	}
	if err := r.Store.SetExportJob(context.WithoutCancel(ctx), j.ID, core.ExportDone, "", "", digest); err != nil {
		return &publishedExportError{err}
	}
	return nil
}

// Preserve the ready journal when publication succeeded but settlement did
// not. Recovery verifies the digest before reporting this output as complete.
type publishedExportError struct{ error }

type checkedWriter struct {
	ctx context.Context
	out io.Writer
}

func (w *checkedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.out.Write(p)
}

// Every existing parent must be a real directory; an export never follows
// a junction or symbolic link into a different destination.
func exportFolder(dir string) error {
	for current := dir; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("the destination contains a link or is not a folder")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}

// Recover settles interrupted outputs by checking their recorded digest.
func (r *ExportRunner) Recover(ctx context.Context) error {
	jobs, err := r.Store.ExportJobs(ctx, 0)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.State != core.ExportReady && j.State != core.ExportWriting {
			continue
		}
		state, message := core.ExportFailed, "Interrupted; retry after checking the destination."
		if j.State == core.ExportReady {
			f, err := core.OpenShared(j.To)
			if err == nil {
				hash := sha256.New()
				_, err = io.Copy(hash, f)
				_ = f.Close()
				if err == nil && hex.EncodeToString(hash.Sum(nil)) == j.Digest {
					state, message = core.ExportDone, ""
				}
			}
		}
		if j.Temp != "" && filepath.Dir(j.Temp) == filepath.Dir(j.To) && strings.HasPrefix(filepath.Base(j.Temp), ".lumen-export-") {
			if err := os.Remove(j.Temp); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove interrupted export: %w", err)
			}
		}
		if err := r.Store.SetExportJob(ctx, j.ID, state, message, "", j.Digest); err != nil {
			return err
		}
	}
	return nil
}

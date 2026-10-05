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
	"lumen/internal/media"
	"lumen/internal/store"
)

// FreeName is name, or name with the organise planner's " (2)", " (3)"…
// before its extension: the first that nothing in dir has on disk and that
// reserved (folded paths already planned) does not hold.
func FreeName(ctx context.Context, dir, name string, reserved map[string]bool) (string, error) {
	if err := exportFolder(ctx, dir); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; n <= maxCollisions; n++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		candidate := filepath.Join(dir, stem+collisionSuffix(n)+ext)
		if reserved[core.Fold(candidate)] {
			continue
		}
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
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
	if len(jobs) == 0 {
		return store.ErrNotFound
	}
	for _, job := range jobs {
		if job.State != core.ExportPlanned && job.State != core.ExportFailed && job.State != core.ExportCancelled {
			continue
		}
		state := core.ExportDone
		err := r.one(ctx, job)
		if err != nil {
			var pending *pendingExportError
			if errors.As(err, &pending) {
				return err
			}
			state = core.ExportFailed
			if ctx.Err() != nil {
				state = core.ExportCancelled
			}
		}
		message := ""
		if state == core.ExportFailed {
			message = exportReason(err)
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

func (r *ExportRunner) one(ctx context.Context, j core.ExportJob) (result error) {
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
		return ErrSourceChanged
	}
	if err := exportFolder(ctx, filepath.Dir(j.To)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(j.To), ".lumen-export-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close() // Already closed before publication; close on failure too.
		if err := os.Remove(tmp.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			// Keep the writing/ready row and its owned path for recovery instead
			// of settling a failure that forgets an undeleted temporary copy.
			result = &pendingExportError{errors.Join(result, fmt.Errorf("remove export temporary copy: %w", err))}
		}
	}()
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
		return ErrSourceChanged
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
		return &pendingExportError{err}
	}
	return nil
}

// exportReason is why a copy was not made, as its row says it: one of the
// reasons the UI words itself, else the operating system's own message.
func exportReason(err error) string {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return ReasonNotVisible
	case errors.Is(err, os.ErrNotExist):
		return ReasonMissing
	case errors.Is(err, ErrSourceChanged):
		return ReasonChanged
	case errors.Is(err, ErrDestinationFolder):
		return ReasonNotAFolder
	case errors.Is(err, ErrExists):
		return ReasonTaken
	case errors.Is(err, media.ErrUnsupported):
		return ReasonUnsupported
	}
	return reasonOf(err)
}

// Preserve the journal when settlement or temporary-file cleanup failed.
// Recovery checks committed bytes and removes only the recorded owned copy.
type pendingExportError struct{ error }

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
func exportFolder(ctx context.Context, dir string) error {
	for current := dir; ; current = filepath.Dir(current) {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrDestinationFolder
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
		state, message := core.ExportFailed, ReasonInterrupted
		if j.State == core.ExportReady {
			f, err := core.OpenShared(j.To)
			if err == nil {
				hash := sha256.New()
				_, err = io.Copy(&checkedWriter{ctx: ctx, out: hash}, f)
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

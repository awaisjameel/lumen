package fileops

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"lumen/internal/core"
	"lumen/internal/scanner"
	"lumen/internal/store"
)

// FileDigest checks full bytes while holding a Windows read lease.
func FileDigest(ctx context.Context, path string) (digest string, size int64, err error) {
	f, err := core.OpenStable(ctx, path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	return core.FullDigest(ctx, f)
}

// CompressionPublisher publishes exactly a reviewed candidate, with a durable
// same-volume temporary path and a non-overwriting rename. Source files never change.
type CompressionPublisher struct{ Store *store.Store }

// Commit validates identity and visibility again, keeping a lease until publication finishes.
func (p *CompressionPublisher) Commit(ctx context.Context, j core.CompressionJob) (result error) {
	if j.State != core.CompressionReady || j.Digest == "" || j.OutputDigest == "" {
		return ErrSourceChanged
	}
	release, err := LeaseCopyFolders(ctx, filepath.Dir(j.From), filepath.Dir(j.To))
	if err != nil {
		return err
	}
	defer release()
	if err := p.Store.ExportAllowed(ctx, j.AssetID, j.To); err != nil {
		return err
	}
	f, err := p.Store.AssetFile(ctx, j.AssetID, false)
	if err != nil {
		return err
	}
	found := false
	for _, path := range f.Paths {
		if core.Fold(path) == core.Fold(j.From) {
			found = true
			break
		}
	}
	if !found {
		if err := p.Store.CompressionRestoreAllowed(ctx, j); err != nil {
			return err
		}
	}
	if err := exportFolder(ctx, filepath.Dir(j.From)); err != nil {
		return err
	}
	in, err := core.OpenStable(ctx, j.From)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != j.Size || info.ModTime().UnixNano() != j.MTime {
		return ErrSourceChanged
	}
	digest, _, err := core.FullDigest(ctx, in)
	if err != nil {
		return err
	}
	if digest != j.Digest {
		return ErrSourceChanged
	}
	if err := exportFolder(ctx, filepath.Dir(j.To)); err != nil {
		return err
	}
	known, err := p.Store.KnownDestinations(ctx, []string{j.To})
	if err != nil {
		return err
	}
	if known[core.Fold(j.To)] {
		return ErrExists
	}
	if err := CopySpace(ctx, filepath.Dir(j.To), j.OutputSize+j.SidecarSize); err != nil {
		return err
	}
	if j.SidecarDigest != "" {
		side, err := core.OpenStable(ctx, j.From+SidecarExt)
		if err != nil {
			return err
		}
		defer func() { _ = side.Close() }()
		d, _, err := core.FullDigest(ctx, side)
		if err != nil {
			return err
		}
		if d != j.SidecarDigest {
			return ErrSourceChanged
		}
	} else if _, err := os.Lstat(j.From + SidecarExt); !errors.Is(err, os.ErrNotExist) {
		return ErrSourceChanged
	}
	if _, err := os.Lstat(j.To + SidecarExt); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return ErrExists
	}
	tmp, err := os.CreateTemp(filepath.Dir(j.To), ".lumen-compress-*")
	if err != nil {
		return err
	}
	j.PublishTemp = tmp.Name()
	j.State = core.CompressionCommitting
	if err := p.Store.SetCompressionJob(ctx, j); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	defer func() { _ = tmp.Close() }()
	candidate, err := core.OpenStable(ctx, j.Temp)
	if err != nil {
		return err
	}
	defer func() { _ = candidate.Close() }()
	d, n, err := core.FullDigest(ctx, candidate)
	if err != nil {
		return err
	}
	if d != j.OutputDigest || n != j.OutputSize {
		return ErrSourceChanged
	}
	if _, err := candidate.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(&checkedWriter{ctx: ctx, out: tmp}, candidate); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chtimes(j.PublishTemp, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	if err := p.Store.ExportAllowed(ctx, j.AssetID, j.To); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := Move(j.PublishTemp, j.To); err != nil {
		return err
	}
	return p.Settle(context.WithoutCancel(ctx), j)
}

// Settle reconciles a committing row only if its destination matches the reviewed bytes.
func (p *CompressionPublisher) Settle(ctx context.Context, j core.CompressionJob) error {
	release, err := LeaseCopyFolders(ctx, filepath.Dir(j.To))
	if err != nil {
		return err
	}
	defer release()
	// Registration must describe the bytes just verified, even when another
	// process tries to edit the output while the database writer is occupied.
	output, err := core.OpenStable(ctx, j.To)
	if err != nil {
		return ErrSourceChanged
	}
	defer func() { _ = output.Close() }()
	d, size, err := core.FullDigest(ctx, output)
	if err != nil || d != j.OutputDigest || size != j.OutputSize {
		return ErrSourceChanged
	}
	if j.SidecarDigest != "" {
		side, err := core.OpenStable(ctx, j.To+SidecarExt)
		if errors.Is(err, os.ErrNotExist) {
			if err := publishSidecar(ctx, j); err != nil {
				return err
			}
			side, err = core.OpenStable(ctx, j.To+SidecarExt)
		}
		if err != nil {
			return ErrExists
		}
		defer func() { _ = side.Close() }()
		existing, _, err := core.FullDigest(ctx, side)
		if err != nil || existing != j.SidecarDigest {
			return ErrExists
		}
	}
	hash, err := scanner.Fingerprint(ctx, j.To, j.OutputSize)
	if err != nil {
		return err
	}
	info, err := output.Stat()
	if err != nil {
		return err
	}
	if err := p.Store.FinishCompression(ctx, j, hash, info.ModTime().UnixNano()); err != nil {
		return err
	}
	if j.PublishTemp != "" && filepath.Dir(j.PublishTemp) == filepath.Dir(j.To) && strings.HasPrefix(filepath.Base(j.PublishTemp), ".lumen-compress-") {
		if err := os.Remove(j.PublishTemp); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func publishSidecar(ctx context.Context, j core.CompressionJob) error {
	// The snapshot belongs to the candidate. No XMP ownership row is copied:
	// external sidecars remain external and cannot be overwritten by Lumen.
	path := filepath.Join(filepath.Dir(j.Temp), "sidecar.xmp")
	in, err := core.OpenStable(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	d, _, err := core.FullDigest(ctx, in)
	if err != nil {
		return err
	}
	if d != j.SidecarDigest {
		return ErrSourceChanged
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(j.To), ".lumen-compress-sidecar-*")
	if err != nil {
		return err
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(&checkedWriter{ctx: ctx, out: tmp}, in); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return Move(tmp.Name(), j.To+SidecarExt)
}

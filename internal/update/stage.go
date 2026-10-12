package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"lumen/internal/core"
)

// Stage extracts into a new owned directory, rejecting every non-inventory entry.
func Stage(ctx context.Context, archive, destination string, r Release) error {
	if _, err := r.validate(); err != nil {
		return err
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	if len(z.File) != len(r.Files) {
		return ErrInvalid
	}
	want := make(map[string]File)
	for _, f := range r.Files {
		want[core.Fold(f.Path)] = f
	}
	for _, entry := range z.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, ok := want[core.Fold(entry.Name)]
		if !ok || entry.Name != f.Path || !entry.Mode().IsRegular() || entry.UncompressedSize64 != uint64(f.Size) {
			return ErrInvalid
		}
		delete(want, core.Fold(entry.Name))
		name := filepath.Join(destination, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			return err
		}
		if err := extract(ctx, entry, name, f); err != nil {
			return err
		}
	}
	// Every file was hashed as it was written; what is left to prove is that
	// the folder holds exactly the inventory.
	return VerifyLayout(ctx, destination, r)
}

func extract(ctx context.Context, entry *zip.File, name string, expected File) error {
	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(&contextReader{ctx: ctx, r: src}, expected.Size+1))
	if err != nil {
		return err
	}
	if n != expected.Size || hex.EncodeToString(h.Sum(nil)) != expected.SHA256 {
		return ErrInvalid
	}
	if err := dst.Sync(); err != nil {
		return err
	}
	return dst.Close()
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// VerifyGeneration checks complete inventory, hashes and unsafe filesystem links.
func VerifyGeneration(ctx context.Context, dir string, r Release) error {
	return verify(ctx, dir, r, true)
}

// VerifyLayout checks that dir holds exactly the inventory's files at their
// sizes, with no links, without reading them: what an ordinary launch can
// afford. It tells a mixed or truncated generation from a whole one; bytes
// are proven by VerifyGeneration before a generation
// is first selected and before it is selected again in recovery.
func VerifyLayout(ctx context.Context, dir string, r Release) error {
	return verify(ctx, dir, r, false)
}

func verify(ctx context.Context, dir string, r Release, hashes bool) error {
	if _, err := r.validate(); err != nil {
		return err
	}
	if err := linked(dir); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	want := make(map[string]File)
	for _, f := range r.Files {
		want[f.Path] = f
	}
	err = filepath.WalkDir(dir, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// The walk reaches a folder before what it holds, so each entry
		// answers for itself alone.
		if err := linked(name); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		expected, ok := want[key]
		if !ok {
			return ErrInvalid
		}
		delete(want, key)
		if !hashes {
			fi, err := entry.Info()
			if err != nil {
				return err
			}
			if fi.Size() != expected.Size {
				return fmt.Errorf("runtime inventory mismatch: %w", ErrInvalid)
			}
			return nil
		}
		f, err := root.Open(rel)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, copyErr := io.Copy(h, &contextReader{ctx: ctx, r: f})
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != expected.Size || hex.EncodeToString(h.Sum(nil)) != expected.SHA256 {
			return fmt.Errorf("runtime inventory mismatch: %w", ErrInvalid)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(want) != 0 {
		return ErrInvalid
	}
	return nil
}

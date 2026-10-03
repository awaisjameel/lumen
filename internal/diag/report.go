package diag

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// devVersion is the version of a build that carries none.
const devVersion = "dev"

// versionString writes a four-part version as people do: without a fourth
// part of 0.
func versionString(major, minor, patch, build uint32) string {
	if build == 0 {
		return fmt.Sprintf("%d.%d.%d", major, minor, patch)
	}
	return fmt.Sprintf("%d.%d.%d.%d", major, minor, patch, build)
}

// PC describes the computer Lumen runs on, for a problem report.
type PC struct {
	Windows     string
	CPU         string
	Threads     int
	MemoryBytes uint64
	Graphics    []string
}

// String is the description as lines of "name: value".
func (p PC) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Windows: %s\n", p.Windows)
	fmt.Fprintf(&b, "Processor: %s (%d threads)\n", p.CPU, p.Threads)
	fmt.Fprintf(&b, "Memory: %.1f GB\n", float64(p.MemoryBytes)/(1<<30))
	fmt.Fprintf(&b, "Graphics: %s\n", strings.Join(p.Graphics, "; "))
	return b.String()
}

// Report is what a problem report holds besides the crash reports.
type Report struct {
	// About is system.txt: which Lumen, on which PC, and how its library
	// stands. It names none of the library's files or folders.
	About string
	// Logs are the log files, oldest first. One that isn't there is left out.
	Logs []string
}

// WriteReport writes a problem report to w as a zip: system.txt, the logs
// and the crash reports.
func (r *Reports) WriteReport(ctx context.Context, w io.Writer, rep Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	z := zip.NewWriter(w)
	if err := addText(z, "system.txt", rep.About); err != nil {
		return err
	}
	for _, log := range rep.Logs {
		if err := addFile(ctx, z, "logs/"+filepath.Base(log), log); err != nil {
			return err
		}
	}
	names, err := r.Files()
	if err != nil {
		return fmt.Errorf("list crash reports: %w", err)
	}
	for _, n := range names {
		if err := addFile(ctx, z, "crashes/"+n, filepath.Join(r.dir, n)); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := z.Close(); err != nil {
		return err
	}
	return ctx.Err()
}

func addText(z *zip.Writer, name, text string) error {
	f, err := z.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, text)
	return err
}

// addFile copies a file into the zip; one that isn't there is left out.
func addFile(ctx context.Context, z *zip.Writer, name, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	src, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = src.Close() }()
	fi, err := src.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("read %s: not a regular file", filepath.Base(path))
	}
	dst, err := z.Create(name)
	if err != nil {
		return err
	}
	// Capture only the size observed on opening: the active log can keep
	// growing while it is compressed. Check cancellation between reads.
	if _, err := io.Copy(dst, reportReader{ctx, io.LimitReader(src, fi.Size())}); err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return nil
}

type reportReader struct {
	ctx context.Context
	io.Reader
}

func (r reportReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

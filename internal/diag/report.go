package diag

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

func threads() int { return runtime.NumCPU() }

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
	// stands. It names no file or folder.
	About string
	// Logs are the log files, oldest first. One that isn't there is left out.
	Logs []string
}

// WriteReport writes a problem report to w as a zip: system.txt, the logs
// and the crash reports.
func (r *Reports) WriteReport(w io.Writer, rep Report) error {
	z := zip.NewWriter(w)
	if err := addText(z, "system.txt", rep.About); err != nil {
		return err
	}
	for _, log := range rep.Logs {
		if err := addFile(z, "logs/"+filepath.Base(log), log); err != nil {
			return err
		}
	}
	names, err := r.Files()
	if err != nil {
		return fmt.Errorf("list crash reports: %w", err)
	}
	for _, n := range names {
		if err := addFile(z, "crashes/"+n, filepath.Join(r.dir, n)); err != nil {
			return err
		}
	}
	return z.Close()
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
func addFile(z *zip.Writer, name, path string) error {
	src, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = src.Close() }()
	dst, err := z.Create(name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return nil
}

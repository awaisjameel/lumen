// Package core holds the domain types, configuration and paths shared by
// every other package. It depends on nothing else inside Lumen.
package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths are the on-disk locations Lumen uses. Nothing here is ever a user's
// original file.
type Paths struct {
	DataDir  string // %LOCALAPPDATA%\Lumen
	DBPath   string // DataDir\library.db
	ThumbDir string // DataDir\thumbs
	// Runtime holds what ships beside Lumen.exe: ONNX Runtime and DirectML
	// DLLs, ffmpeg\ and models\ (deps/runtime in development).
	Runtime string
}

// DefaultPaths resolves the standard per-user app-data location and the
// runtime folder beside the executable. LUMEN_DATA_DIR and LUMEN_RUNTIME_DIR
// override them (tests, scratch libraries, `go run`); a relative one is taken
// from the working folder, and made absolute so it compares with the
// absolute paths scans find.
func DefaultPaths() (Paths, error) {
	dir, err := absEnv("LUMEN_DATA_DIR")
	if err != nil {
		return Paths{}, err
	}
	if dir == "" {
		if dir, err = DefaultDataDir(); err != nil {
			return Paths{}, err
		}
	}
	p := PathsIn(dir)
	if p.Runtime, err = absEnv("LUMEN_RUNTIME_DIR"); err != nil {
		return Paths{}, err
	}
	if p.Runtime == "" {
		exe, err := os.Executable()
		if err != nil {
			return Paths{}, fmt.Errorf("locate Lumen.exe: %w", err)
		}
		p.Runtime = filepath.Dir(exe)
	}
	return p, nil
}

// DefaultDataDir is the user's own library: Lumen in the per-user app-data
// folder (%LOCALAPPDATA% on Windows), where LUMEN_DATA_DIR is not set.
func DefaultDataDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate app data folder: %w", err)
	}
	return filepath.Join(base, "Lumen"), nil
}

// absEnv is the environment variable name as an absolute path, "" if unset.
func absEnv(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", nil
	}
	abs, err := filepath.Abs(v)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return abs, nil
}

// PathsIn lays out Lumen's data files inside dir (Runtime is left empty).
func PathsIn(dir string) Paths {
	return Paths{
		DataDir:  dir,
		DBPath:   filepath.Join(dir, "library.db"),
		ThumbDir: filepath.Join(dir, "thumbs"),
	}
}

// FFmpeg is the bundled ffmpeg.exe, "" if it isn't there (no runtime
// folder, or removed, e.g. quarantined): what needs it is then skipped
// (media.ErrUnsupported) rather than failed file by file.
func (p Paths) FFmpeg() string { return p.runtimeTool("ffmpeg", "ffmpeg.exe") }

// FFprobe is the bundled ffprobe.exe, "" if it isn't there (see FFmpeg).
func (p Paths) FFprobe() string { return p.runtimeTool("ffmpeg", "ffprobe.exe") }

// SigLIP is the folder holding the SigLIP 2 model files ("" if there is no
// runtime folder).
func (p Paths) SigLIP() string { return p.runtimeFile("models", "siglip2") }

// Faces is the folder holding the face models, YuNet and SFace ("" if there
// is no runtime folder).
func (p Paths) Faces() string { return p.runtimeFile("models", "faces") }

// runtimeFile is a path inside the runtime folder, "" without one: a
// relative path would be looked for wherever Lumen was started.
func (p Paths) runtimeFile(parts ...string) string {
	if p.Runtime == "" {
		return ""
	}
	return filepath.Join(append([]string{p.Runtime}, parts...)...)
}

// runtimeTool is runtimeFile, "" unless it is a file that exists.
func (p Paths) runtimeTool(parts ...string) string {
	name := p.runtimeFile(parts...)
	if fi, err := os.Stat(name); name == "" || err != nil || fi.IsDir() {
		return ""
	}
	return name
}

// Ensure creates the data folders if they are missing.
func (p Paths) Ensure() error {
	for _, d := range []string{p.DataDir, p.ThumbDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

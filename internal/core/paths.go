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
// override them (tests, scratch libraries, `go run`).
func DefaultPaths() (Paths, error) {
	dir := os.Getenv("LUMEN_DATA_DIR")
	if dir == "" {
		base, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
		if err != nil {
			return Paths{}, fmt.Errorf("locate app data folder: %w", err)
		}
		dir = filepath.Join(base, "Lumen")
	}
	p := PathsIn(dir)
	p.Runtime = os.Getenv("LUMEN_RUNTIME_DIR")
	if p.Runtime == "" {
		exe, err := os.Executable()
		if err != nil {
			return Paths{}, fmt.Errorf("locate Lumen.exe: %w", err)
		}
		p.Runtime = filepath.Dir(exe)
	}
	return p, nil
}

// PathsIn lays out Lumen's data files inside dir (Runtime is left empty).
func PathsIn(dir string) Paths {
	return Paths{
		DataDir:  dir,
		DBPath:   filepath.Join(dir, "library.db"),
		ThumbDir: filepath.Join(dir, "thumbs"),
	}
}

// FFmpeg is the bundled ffmpeg.exe ("" if there is no runtime folder).
func (p Paths) FFmpeg() string { return p.runtimeFile("ffmpeg", "ffmpeg.exe") }

// FFprobe is the bundled ffprobe.exe ("" if there is no runtime folder).
func (p Paths) FFprobe() string { return p.runtimeFile("ffmpeg", "ffprobe.exe") }

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

// Ensure creates the data folders if they are missing.
func (p Paths) Ensure() error {
	for _, d := range []string{p.DataDir, p.ThumbDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

// Package core holds the domain types, configuration and paths shared by
// every other package. It depends on nothing else inside Lumen.
package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths are the on-disk locations Lumen owns. Nothing here is ever a user's
// original file.
type Paths struct {
	DataDir  string // %LOCALAPPDATA%\Lumen
	DBPath   string // DataDir\library.db
	ThumbDir string // DataDir\thumbs
}

// DefaultPaths resolves the standard per-user app-data location. LUMEN_DATA_DIR
// overrides it, which is handy for tests and for trying a scratch library.
func DefaultPaths() (Paths, error) {
	dir := os.Getenv("LUMEN_DATA_DIR")
	if dir == "" {
		base, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
		if err != nil {
			return Paths{}, fmt.Errorf("locate app data folder: %w", err)
		}
		dir = filepath.Join(base, "Lumen")
	}
	return PathsIn(dir), nil
}

// PathsIn lays out Lumen's files inside dir.
func PathsIn(dir string) Paths {
	return Paths{
		DataDir:  dir,
		DBPath:   filepath.Join(dir, "library.db"),
		ThumbDir: filepath.Join(dir, "thumbs"),
	}
}

// Ensure creates the folders if they are missing.
func (p Paths) Ensure() error {
	for _, d := range []string{p.DataDir, p.ThumbDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

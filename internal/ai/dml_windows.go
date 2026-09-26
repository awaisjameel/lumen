package ai

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// preloadDirectML loads the DirectML.dll in dir by full path. Windows ships
// an older DirectML.dll in System32 that the normal DLL search can find
// first; once ours is loaded, ONNX Runtime uses it.
func preloadDirectML(dir string) error {
	_, err := windows.LoadLibraryEx(filepath.Join(dir, "DirectML.dll"), 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH)
	return err
}

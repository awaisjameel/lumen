//go:build windows

package index

import (
	"testing"

	"lumen/internal/testenv"
)

func TestIndexerTakesCameraRawFilesFromAShortPathIntoTheLibrary(t *testing.T) {
	checkIndexerTakesCameraRawFilesIntoTheLibrary(t, testenv.ShortPath(t))
}

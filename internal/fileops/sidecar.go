package fileops

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// Sidecar files (IMG_1.jpg.xmp) are the one kind of file Lumen creates in
// the user's folders. The rules:
//
//   - A new sidecar is written to a temporary name beside it (created with
//     O_EXCL) and then moved into place with a rename that does not replace,
//     so a file that appeared meanwhile is never overwritten and a reader
//     never sees half a sidecar.
//   - A sidecar is replaced only if it is still byte for byte what Lumen
//     wrote there: the caller passes the hash it recorded, and a file that
//     differs from it (edited by someone since) or that Lumen never wrote is
//     left exactly as it is.
//   - The photo or video the sidecar describes is never opened.

// Sidecar says what WriteSidecar did.
type Sidecar int

// The outcomes of WriteSidecar.
const (
	// SidecarFailed is what comes with an error: the file is as it was.
	SidecarFailed Sidecar = iota
	// SidecarCreated means there was no file, and the sidecar was created.
	SidecarCreated
	// SidecarReplaced means Lumen's own earlier sidecar was brought up to date.
	SidecarReplaced
	// SidecarSame means the file already holds exactly these bytes.
	SidecarSame
	// SidecarForeign means a file Lumen did not write is there. It was not
	// touched.
	SidecarForeign
	// SidecarEdited means Lumen wrote the file, but it has been changed since.
	// It was not touched.
	SidecarEdited
)

// SidecarHash is the hash the caller records of the bytes it wrote, and
// passes back to WriteSidecar later as wrote.
func SidecarHash(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// WriteSidecar brings the sidecar at path to data. wrote is the hash of
// what Lumen last wrote at path (SidecarHash), or nil if it never has:
// only a file that still matches wrote is replaced. A file that already
// holds data is reported as SidecarSame whoever wrote it, since nothing
// changes.
//
// The check and the rename are two steps, so a file swapped in between them
// could be replaced. The window is the time to write a few kilobytes, and
// only a file that was Lumen's own a moment before is exposed to it.
func WriteSidecar(ctx context.Context, path string, data, wrote []byte) (Sidecar, error) {
	if err := ctx.Err(); err != nil {
		return SidecarFailed, err
	}
	have, err := hashFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return createSidecar(path, data)
	case err != nil:
		return SidecarFailed, err
	case bytes.Equal(have, SidecarHash(data)):
		return SidecarSame, nil
	case wrote == nil:
		return SidecarForeign, nil
	case !bytes.Equal(have, wrote):
		return SidecarEdited, nil
	}
	return replaceSidecar(path, data)
}

// hashFile is the hash of the file at path, read from its start.
func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() // read only
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// createSidecar makes a sidecar that did not exist a moment ago. If another
// file takes the name first, that file wins and is reported as foreign.
func createSidecar(path string, data []byte) (Sidecar, error) {
	tmp, err := writeTemp(path, data)
	if err != nil {
		return SidecarFailed, err
	}
	if err := publishNew(tmp, path); err != nil {
		discardTemp(tmp)
		if errors.Is(err, fs.ErrExist) {
			return SidecarForeign, nil
		}
		return SidecarFailed, fmt.Errorf("move the sidecar into place: %w", err)
	}
	return SidecarCreated, nil
}

// replaceSidecar swaps Lumen's own sidecar for a new one in one rename.
func replaceSidecar(path string, data []byte) (Sidecar, error) {
	tmp, err := writeTemp(path, data)
	if err != nil {
		return SidecarFailed, err
	}
	if err := os.Rename(tmp, path); err != nil {
		discardTemp(tmp)
		return SidecarFailed, fmt.Errorf("replace the sidecar: %w", err)
	}
	return SidecarReplaced, nil
}

// writeTemp writes data to a new file beside path, named so that nothing
// mistakes it for a photo or a sidecar, and returns its name. It is made with
// O_EXCL, so it is always a file this call created.
func writeTemp(path string, data []byte) (string, error) {
	var salt [6]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	tmp := path + "." + hex.EncodeToString(salt[:]) + ".lumen-tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create a temporary sidecar: %w", err)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		discardTemp(tmp)
		return "", fmt.Errorf("write a temporary sidecar: %w", werr)
	}
	return tmp, nil
}

// discardTemp removes a temporary file writeTemp made. It is the one place
// outside the journal's empty folders where fileops removes anything, and it
// cannot take a user's file: the name is one writeTemp created itself with
// O_EXCL a moment ago, and it holds only Lumen's own bytes.
func discardTemp(tmp string) {
	_ = os.Remove(tmp) // best effort: the failure being reported is the one that matters
}

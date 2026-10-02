//go:build windows

package fileops

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// binCount is how many items the Recycle Bin of path's drive holds
// (SHQueryRecycleBin).
func binCount(t *testing.T, path string) int64 {
	t.Helper()
	root, err := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
	if err != nil {
		t.Fatal(err)
	}
	// SHQUERYRBINFO: a DWORD size, padding, then two 64-bit counts.
	var info [24]byte
	binary.LittleEndian.PutUint32(info[:], uint32(len(info)))
	proc := shell32.NewProc("SHQueryRecycleBinW")
	hr, _, _ := proc.Call(uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&info[0])))
	if failed(hr) {
		t.Fatalf("SHQueryRecycleBin: %v", hresultError(hr))
	}
	return int64(binary.LittleEndian.Uint64(info[16:]))
}

func write(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("lumen fileops test"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRecycleMovesFilesToTheRecycleBinAndReportsGoneOnes(t *testing.T) {
	dir := t.TempDir()
	a, b := write(t, filepath.Join(dir, "a.jpg")), write(t, filepath.Join(dir, "b ünï.jpg"))
	missing := filepath.Join(dir, "missing.jpg")
	before := binCount(t, dir)

	// A path listed twice, and in another spelling, is one file, one outcome.
	out, err := Recycle(context.Background(), []string{a, missing, b, strings.ToUpper(a)})
	if err != nil {
		t.Fatal(err)
	}
	want := []Status{Recycled, Gone, Recycled, Recycled}
	if len(out) != len(want) {
		t.Fatalf("outcomes = %+v", out)
	}
	for i, o := range out {
		if o.Status != want[i] {
			if o.Status == Kept {
				t.Skip("this drive has no Recycle Bin, so the shell refused to recycle")
			}
			t.Fatalf("outcome %d = %+v, want status %d", i, o, want[i])
		}
	}
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still in its folder: %v", p, err)
		}
	}
	if after := binCount(t, dir); after < before+2 {
		t.Errorf("the Recycle Bin holds %d items, was %d: want at least 2 more", after, before)
	}
}

func TestRecycleLeavesFoldersAndRelativePathsAlone(t *testing.T) {
	dir := t.TempDir()
	child := write(t, filepath.Join(dir, "inside.jpg"))
	out, err := Recycle(context.Background(), []string{dir, "relative.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range out {
		if o.Status != Failed || o.Err == nil {
			t.Errorf("outcome %+v: want Failed with a reason", o)
		}
	}
	if _, err := os.Stat(child); err != nil {
		t.Errorf("the folder's file was touched: %v", err)
	}
}

func TestRecycleStopsWhenTheContextHasEnded(t *testing.T) {
	file := write(t, filepath.Join(t.TempDir(), "a.jpg"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := Recycle(ctx, []string{file})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(out) != 1 || out[0].Status != Failed {
		t.Fatalf("outcomes = %+v", out)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("a cancelled call touched the file: %v", err)
	}
}

// What the sink answers for an item the shell would destroy is the heart of
// "never delete for good". The real shell cannot be made to refuse on demand,
// so this drives the sink's callbacks as the shell would.
func TestTheSinkRefusesAnItemWithoutTheRecycleFlagAndTheFileIsReportedKept(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "a.jpg"))
	out := []Outcome{{Path: path}}
	op := &operation{outcomes: out, byPath: map[string]int{strings.ToLower(path): 0}, decided: make([]bool, 1), allowed: make([]bool, 1)}

	if op.beforeDelete(path, 0) {
		t.Fatal("the sink let the shell delete a file it would not recycle")
	}
	// The shell reports an aborted item afterwards: that must not undo Kept.
	op.afterDelete(path, 0)
	if out[0].Status != Kept {
		t.Fatalf("outcome = %+v, want Kept", out[0])
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file was touched: %v", err)
	}
}

func TestTheSinkRefusesAnItemItWasNeverGiven(t *testing.T) {
	op := &operation{byPath: map[string]int{}, decided: nil, allowed: nil}
	if op.beforeDelete(`C:\somewhere\else.jpg`, tsfDeleteRecycleIfPossible) {
		t.Fatal("the sink let the shell delete a file that is not one of ours")
	}
}

func TestTheSinkReportsAShellFailureWithItsWindowsReason(t *testing.T) {
	path := `C:\x\a.jpg`
	out := []Outcome{{Path: path}}
	op := &operation{outcomes: out, byPath: map[string]int{strings.ToLower(path): 0}, decided: make([]bool, 1), allowed: make([]bool, 1)}
	if !op.beforeDelete(path, tsfDeleteRecycleIfPossible) {
		t.Fatal("the sink refused a recyclable file")
	}
	op.afterDelete(path, uintptr(0x80070020)) // the HRESULT of a sharing violation (Win32 error 32)
	if out[0].Status != Failed || !errors.Is(out[0].Err, syscall.Errno(32)) {
		t.Fatalf("outcome = %+v, want Failed with the sharing violation", out[0])
	}
}

func TestTheShellInterfaceIdentifiersAreTheDocumentedOnes(t *testing.T) {
	for name, c := range map[string]struct {
		got  windows.GUID
		want string
	}{
		"CLSID_FileOperation":            {clsidFileOperation, "{3AD05575-8857-4850-9277-11B85BDB8E09}"},
		"IID_IFileOperation":             {iidFileOperation, "{947AAB5F-0A5C-4C13-B4D6-4BF7836FC9F8}"},
		"IID_IFileOperationProgressSink": {iidProgressSink, "{04B0F1A7-9490-44BC-96E1-4296A31252E2}"},
		"IID_IShellItem":                 {iidShellItem, "{43826D1E-E718-42EE-BC55-A1E261C37BFE}"},
		"IID_IUnknown":                   {iidUnknown, "{00000000-0000-0000-C000-000000000046}"},
	} {
		want, err := windows.GUIDFromString(c.want)
		if err != nil || c.got != want {
			t.Errorf("%s = %v, want %s (%v)", name, c.got, c.want, err)
		}
	}
}

// A file reached over the network has no Recycle Bin, so the shell would
// delete it for good: the real sink must refuse it, leave it in place, and
// still recycle the files after it. The folder's own administrative share is
// a network path to a local file; without rights to it the test cannot run.
func TestRecycleLeavesAFileOnANetworkPathInPlaceAndRecyclesTheRest(t *testing.T) {
	dir := t.TempDir()
	drive := filepath.VolumeName(dir)
	shared := write(t, filepath.Join(dir, "shared.jpg"))
	local := write(t, filepath.Join(dir, "local.jpg"))
	unc := `\\localhost\` + drive[:1] + `$` + strings.TrimPrefix(shared, drive)
	if _, err := os.Stat(unc); err != nil {
		t.Skipf("no access to the administrative share: %v", err)
	}

	out, err := Recycle(context.Background(), []string{unc, local})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Status != Kept {
		t.Errorf("network file outcome = %+v, want Kept", out[0])
	}
	if out[1].Status != Recycled {
		t.Errorf("local file outcome = %+v, want Recycled", out[1])
	}
	if _, err := os.Stat(shared); err != nil {
		t.Errorf("the network file was touched: %v", err)
	}
	if _, err := os.Stat(local); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the local file is still in its folder: %v", err)
	}
}

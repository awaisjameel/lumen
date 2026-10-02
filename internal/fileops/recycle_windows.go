//go:build windows

package fileops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"lumen/internal/core"
)

// The shell's IFileOperation does the recycling: it is what Explorer uses,
// so the Recycle Bin shows the file with its original location and Restore
// works. It is a COM object, called here through its vtable with
// golang.org/x/sys/windows and no further dependency.

const (
	// FOF_SILENT | FOF_NOCONFIRMATION | FOF_NOERRORUI | FOF_NOCONFIRMMKDIR
	// (FOF_NO_UI), FOF_ALLOWUNDO, and FOFX_RECYCLEONDELETE, which makes the
	// shell recycle where it can.
	operationFlags = 0x4 | 0x10 | 0x400 | 0x200 | 0x40 | 0x80000

	sigdnFilesysPath = 0x80058000

	sOK          = 0
	sFalse       = 1
	eAbort       = 0x80004004
	eNoInterface = 0x80004002
)

var (
	clsidFileOperation = windows.GUID{Data1: 0x3ad05575, Data2: 0x8857, Data3: 0x4850,
		Data4: [8]byte{0x92, 0x77, 0x11, 0xb8, 0x5b, 0xdb, 0x8e, 0x09}}
	iidFileOperation = windows.GUID{Data1: 0x947aab5f, Data2: 0x0a5c, Data3: 0x4c13,
		Data4: [8]byte{0xb4, 0xd6, 0x4b, 0xf7, 0x83, 0x6f, 0xc9, 0xf8}}
	iidProgressSink = windows.GUID{Data1: 0x04b0f1a7, Data2: 0x9490, Data3: 0x44bc,
		Data4: [8]byte{0x96, 0xe1, 0x42, 0x96, 0xa3, 0x12, 0x52, 0xe2}}
	iidShellItem = windows.GUID{Data1: 0x43826d1e, Data2: 0xe718, Data3: 0x42ee,
		Data4: [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
	iidUnknown = windows.GUID{Data1: 0, Data2: 0, Data3: 0,
		Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}

	ole32                           = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	shell32                         = windows.NewLazySystemDLL("shell32.dll")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
)

// Recycle moves the files at paths to the Recycle Bin and says what happened
// to each, in order. A file the shell would not put in the bin is left where
// it was (Kept), and one that is not there is Gone. The error is for a
// failure that stopped the call: COM would not start, or ctx ended; the
// outcomes then still say which files made it to the bin before it did.
//
// The call works through the files in rounds, and ctx is checked between
// rounds, not inside the shell's own work.
func Recycle(ctx context.Context, paths []string) ([]Outcome, error) {
	out := make([]Outcome, len(paths))
	op := &operation{outcomes: out, byPath: make(map[string]int, len(paths)), decided: make([]bool, len(paths)),
		allowed: make([]bool, len(paths))}
	var queue []int
	for i, p := range paths {
		out[i].Path = p
		key := core.Fold(filepath.Clean(p))
		if first, dup := op.byPath[key]; dup {
			op.duplicates = append(op.duplicates, [2]int{i, first})
			continue
		}
		op.byPath[key] = i
		if !filepath.IsAbs(p) {
			// A relative path would mean whatever the working directory is.
			op.decide(i, Failed, errors.New("the path is not absolute"))
			continue
		}
		switch info, err := os.Lstat(p); {
		case errors.Is(err, fs.ErrNotExist):
			op.decide(i, Gone, nil)
		case err != nil:
			op.decide(i, Failed, err)
		case info.IsDir():
			// The shell would recycle a whole folder; Lumen only ever means files.
			op.decide(i, Failed, errors.New("the path is a folder"))
		default:
			queue = append(queue, i)
		}
	}
	err := op.run(ctx, queue)
	for _, d := range op.duplicates {
		out[d[0]].Status, out[d[0]].Err = out[d[1]].Status, out[d[1]].Err
	}
	return out, err
}

// operation is what one Recycle call knows: the shell reports its decisions
// through the progress sink, possibly on another thread, so the state is
// guarded by mu.
type operation struct {
	mu         sync.Mutex
	outcomes   []Outcome
	byPath     map[string]int // folded, cleaned path -> index into outcomes
	decided    []bool         // the outcome is final
	allowed    []bool         // the shell announced it would recycle the file
	duplicates [][2]int       // [later, first]: paths listed twice share one outcome
}

// run recycles the files at the queued indices on a COM apartment thread.
func (o *operation) run(ctx context.Context, queue []int) error {
	if len(queue) == 0 {
		return nil
	}
	// COM state belongs to the thread, so the whole call stays on one.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); {
	case err == nil || errors.Is(err, syscall.Errno(sFalse)): // S_FALSE: already initialised here, still counted
		defer windows.CoUninitialize()
	default:
		o.failAll(queue, err)
		return fmt.Errorf("start COM: %w", err)
	}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			o.failAll(queue, err)
			return err
		}
		performErr := o.round(queue)
		var undecided []int
		for _, i := range queue {
			if !o.isDecided(i) {
				undecided = append(undecided, i)
			}
		}
		// An aborted item can end the shell's whole operation, so what it did
		// not reach goes round again. Each round decides at least one file;
		// one that decides none is the shell failing, not progress.
		if len(undecided) == len(queue) {
			if performErr == nil {
				performErr = errors.New("the shell did not process the file")
			}
			o.failAll(undecided, performErr)
			return nil
		}
		queue = undecided
	}
	return nil
}

// round gives the shell the files at idx in one IFileOperation.
func (o *operation) round(idx []int) error {
	var fo *fileOperation
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOperation)), 0, windows.CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidFileOperation)), uintptr(unsafe.Pointer(&fo)))
	if failed(hr) {
		return fmt.Errorf("create the shell's file operation: %w", hresultError(hr))
	}
	defer fo.release()
	if hr := fo.setOperationFlags(operationFlags); failed(hr) {
		return fmt.Errorf("set the file operation's flags: %w", hresultError(hr))
	}

	s := &sink{vtbl: sinkVtable(), op: o}
	var pin runtime.Pinner // the shell keeps the sink's address until Unadvise
	pin.Pin(s)
	defer pin.Unpin()
	var cookie uint32
	if hr := fo.advise(s, &cookie); failed(hr) {
		return fmt.Errorf("listen to the shell's file operation: %w", hresultError(hr))
	}
	defer fo.unadvise(cookie)

	queued := 0
	for _, i := range idx {
		item, err := newShellItem(o.outcomes[i].Path)
		if err != nil {
			o.fail(i, err)
			continue
		}
		hr := fo.deleteItem(item)
		item.release()
		if failed(hr) {
			o.fail(i, fmt.Errorf("queue the file for the Recycle Bin: %w", hresultError(hr)))
			continue
		}
		queued++
	}
	if queued == 0 {
		return nil
	}
	if hr := fo.performOperations(); failed(hr) {
		return fmt.Errorf("recycle: %w", hresultError(hr))
	}
	runtime.KeepAlive(s)
	return nil
}

func (o *operation) isDecided(i int) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.decided[i]
}

// decide fixes an outcome. The first decision wins: the shell reports an
// item it was told to abort afterwards too, and that must not undo Kept.
func (o *operation) decide(i int, status Status, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.decided[i] {
		o.decided[i] = true
		o.outcomes[i].Status, o.outcomes[i].Err = status, err
	}
}

// fail decides that the file at i could not be recycled, or, if the reason is
// that it is no longer there (it went between the check and the shell), that
// it is gone.
func (o *operation) fail(i int, err error) {
	if errors.Is(err, fs.ErrNotExist) {
		o.decide(i, Gone, nil)
		return
	}
	o.decide(i, Failed, err)
}

func (o *operation) failAll(idx []int, err error) {
	for _, i := range idx {
		o.decide(i, Failed, err)
	}
}

// beforeDelete is the shell asking leave to delete the file at path, with the
// flags it will do it with. Only a file that goes to the Recycle Bin may be
// deleted; anything the shell does not recognise as one of ours is refused
// too, whatever it says.
func (o *operation) beforeDelete(path string, flags uint32) (proceed bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	i, ok := o.byPath[core.Fold(filepath.Clean(path))]
	if !ok || o.decided[i] {
		return false
	}
	if !recyclable(flags) {
		o.decided[i] = true
		o.outcomes[i].Status = Kept
		return false
	}
	o.allowed[i] = true
	return true
}

// afterDelete is the shell reporting how deleting the file at path went.
func (o *operation) afterDelete(path string, hr uintptr) {
	o.mu.Lock()
	i, ok := o.byPath[core.Fold(filepath.Clean(path))]
	allowed := ok && o.allowed[i]
	o.mu.Unlock()
	if !allowed {
		return
	}
	if failed(hr) {
		o.fail(i, hresultError(hr))
		return
	}
	o.decide(i, Recycled, nil)
}

// failed reports whether an HRESULT is an error.
func failed(hr uintptr) bool { return int32(hr) < 0 }

// hresultError turns an HRESULT into an error: a Win32 error wrapped in one
// (the common case) comes out as that error, so its message is Windows's own.
func hresultError(hr uintptr) error {
	code := uint32(hr)
	if code&0xffff0000 == 0x80070000 {
		return syscall.Errno(code & 0xffff)
	}
	return fmt.Errorf("HRESULT 0x%08x", code)
}

// The COM objects below are a pointer to a vtable: a struct whose methods are
// the table's entries, in the order the interface declares them.

type unknownVtbl struct{ queryInterface, addRef, release uintptr }

type shellItemVtbl struct {
	unknownVtbl
	_              [2]uintptr // BindToHandler, GetParent
	getDisplayName uintptr
	_              [2]uintptr // GetAttributes, Compare
}

type shellItem struct{ vtbl *shellItemVtbl }

func newShellItem(path string) (*shellItem, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var item *shellItem
	hr, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&iidShellItem)),
		uintptr(unsafe.Pointer(&item)))
	if failed(hr) {
		return nil, fmt.Errorf("find the file in the shell: %w", hresultError(hr))
	}
	return item, nil
}

func (i *shellItem) release() {
	_, _, _ = syscall.SyscallN(i.vtbl.release, uintptr(unsafe.Pointer(i))) // the count it returns is of no use
}

// path is the item's file system path.
func (i *shellItem) path() (string, error) {
	var name *uint16
	hr, _, _ := syscall.SyscallN(i.vtbl.getDisplayName, uintptr(unsafe.Pointer(i)), sigdnFilesysPath, uintptr(unsafe.Pointer(&name)))
	if failed(hr) {
		return "", hresultError(hr)
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(name))
	return windows.UTF16PtrToString(name), nil
}

type fileOperationVtbl struct {
	unknownVtbl
	advise, unadvise, setOperationFlags uintptr
	// SetProgressMessage to CopyItems, then DeleteItem below, then DeleteItems and NewItem.
	_                 [12]uintptr
	deleteItem        uintptr
	_                 [2]uintptr
	performOperations uintptr
}

type fileOperation struct{ vtbl *fileOperationVtbl }

func (o *fileOperation) release() {
	_, _, _ = syscall.SyscallN(o.vtbl.release, uintptr(unsafe.Pointer(o)))
}

func (o *fileOperation) setOperationFlags(flags uint32) uintptr {
	hr, _, _ := syscall.SyscallN(o.vtbl.setOperationFlags, uintptr(unsafe.Pointer(o)), uintptr(flags))
	return hr
}

func (o *fileOperation) advise(s *sink, cookie *uint32) uintptr {
	hr, _, _ := syscall.SyscallN(o.vtbl.advise, uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(cookie)))
	return hr
}

func (o *fileOperation) unadvise(cookie uint32) {
	_, _, _ = syscall.SyscallN(o.vtbl.unadvise, uintptr(unsafe.Pointer(o)), uintptr(cookie)) // best effort: the operation is over
}

func (o *fileOperation) deleteItem(item *shellItem) uintptr {
	hr, _, _ := syscall.SyscallN(o.vtbl.deleteItem, uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(item)), 0)
	return hr
}

func (o *fileOperation) performOperations() uintptr {
	hr, _, _ := syscall.SyscallN(o.vtbl.performOperations, uintptr(unsafe.Pointer(o)))
	return hr
}

// sink is our IFileOperationProgressSink. Its vtable is built once: a
// callback made with syscall.NewCallback is never freed, so a table per
// operation would leak them.
type sink struct {
	vtbl *sinkVtbl // first, as in every COM object
	refs atomic.Int32
	op   *operation
}

type sinkVtbl struct {
	unknownVtbl
	startOperations, finishOperations, preRenameItem, postRenameItem, preMoveItem, postMoveItem, preCopyItem,
	postCopyItem, preDeleteItem, postDeleteItem, preNewItem, postNewItem, updateProgress, resetTimer, pauseTimer,
	resumeTimer uintptr
}

// sinkVtable is the table every sink points to. Callbacks that ignore their
// arguments (all but the delete ones) are one function: on 64-bit Windows the
// caller cleans up, so extra arguments are harmless.
var sinkVtable = sync.OnceValue(func() *sinkVtbl {
	ok := windows.NewCallback(func(*sink) uintptr { return sOK })
	return &sinkVtbl{
		unknownVtbl: unknownVtbl{
			queryInterface: windows.NewCallback(sinkQueryInterface),
			addRef:         windows.NewCallback(func(s *sink) uintptr { return uintptr(s.refs.Add(1)) }),
			release:        windows.NewCallback(func(s *sink) uintptr { return uintptr(s.refs.Add(-1)) }),
		},
		startOperations: ok, finishOperations: ok, preRenameItem: ok, postRenameItem: ok, preMoveItem: ok,
		postMoveItem: ok, preCopyItem: ok, postCopyItem: ok,
		preDeleteItem:  windows.NewCallback(sinkPreDeleteItem),
		postDeleteItem: windows.NewCallback(sinkPostDeleteItem),
		preNewItem:     ok, postNewItem: ok, updateProgress: ok, resetTimer: ok, pauseTimer: ok, resumeTimer: ok,
	}
})

func sinkQueryInterface(s *sink, iid *windows.GUID, out *unsafe.Pointer) uintptr {
	if *iid == iidUnknown || *iid == iidProgressSink {
		s.refs.Add(1)
		*out = unsafe.Pointer(s)
		return sOK
	}
	*out = nil
	return eNoInterface
}

// sinkPreDeleteItem is IFileOperationProgressSink::PreDeleteItem. Returning
// E_ABORT stops the shell deleting this item: it is how a file the shell would
// destroy is left alone.
func sinkPreDeleteItem(s *sink, flags uint32, item *shellItem) uintptr {
	path, err := item.path()
	if err != nil || !s.op.beforeDelete(path, flags) {
		return eAbort
	}
	return sOK
}

// sinkPostDeleteItem is IFileOperationProgressSink::PostDeleteItem.
func sinkPostDeleteItem(s *sink, _ uint32, item *shellItem, hr uintptr, _ *shellItem) uintptr {
	if path, err := item.path(); err == nil {
		s.op.afterDelete(path, hr)
	}
	return sOK
}

//go:build windows

package signin

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file is the small amount of COM and WinRT that Windows Hello needs,
// written as plain vtable calls so the package needs nothing beyond
// golang.org/x/sys.

var (
	combase                    = windows.NewLazySystemDLL("combase.dll")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")

	user32                          = windows.NewLazySystemDLL("user32.dll")
	procMsgWaitForMultipleObjectsEx = user32.NewProc("MsgWaitForMultipleObjectsEx")
	procPeekMessageW                = user32.NewProc("PeekMessageW")
	procTranslateMessage            = user32.NewProc("TranslateMessage")
	procDispatchMessageW            = user32.NewProc("DispatchMessageW")
)

// The vtable slots used. Every COM interface starts with IUnknown's three
// methods; a WinRT interface adds IInspectable's three more.
const (
	slotQueryInterface = 0
	slotRelease        = 2

	// IAsyncOperation<T>, after IInspectable.
	slotPutCompleted = 6
	slotGetResults   = 8

	// IAsyncInfo, after IInspectable.
	slotCancel = 9
)

const (
	eNoInterface    = 0x80004002
	rpcEChangedMode = 0x80010106
	sFalse          = 1 // CoInitializeEx: this thread was already set up the same way
	waitTimeout     = 258
	qsAllInput      = 0x04FF
	mwmoInputAvail  = 0x0004
	pmRemove        = 0x0001
	infinite        = 0xFFFFFFFF
	// cancelSettleMS is how long a cancelled operation gets to report back.
	// Windows marks it cancelled at once but, for the Hello dialog, calls the
	// completion handler only when that dialog goes away, so waiting longer
	// would only delay the caller.
	cancelSettleMS = 500
)

func mustGUID(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(err) // a malformed constant, caught by every test run
	}
	return g
}

var (
	iidIUnknown     = mustGUID("{00000000-0000-0000-C000-000000000046}")
	iidIAgileObject = mustGUID("{94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90}")
	iidIAsyncInfo   = mustGUID("{00000036-0000-0000-C000-000000000046}")
)

// hresultError is a failed HRESULT.
type hresultError uint32

func (h hresultError) Error() string { return fmt.Sprintf("HRESULT 0x%08X", uint32(h)) }

// hrCheck turns a COM call's result into an error naming the call.
func hrCheck(call string, hr uintptr) error {
	if int32(hr) >= 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", call, hresultError(hr))
}

//go:uintptrescapes

// vcall calls method slot of the COM object obj and returns its raw result
// (an HRESULT, for all that are used here). The directive above keeps what
// the pointer arguments point at alive, and off a stack that could move, for
// the length of the call, as it does for the x/sys procedure calls.
func vcall(obj unsafe.Pointer, slot int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	method := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// release drops one reference to obj.
func release(obj unsafe.Pointer) {
	if obj != nil {
		vcall(obj, slotRelease)
	}
}

// hstring is a WinRT string handle.
type hstring uintptr

func newHString(s string) (hstring, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	return h, hrCheck("create string", hr)
}

func (h hstring) free() { _, _, _ = procWindowsDeleteString.Call(uintptr(h)) }

// activate returns the activation factory of a WinRT class as interface iid.
func activate(class string, iid *windows.GUID) (unsafe.Pointer, error) {
	name, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer name.free()
	var factory unsafe.Pointer
	hr, _, _ := procRoGetActivationFactory.Call(uintptr(name), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&factory)))
	if err := hrCheck("activate "+class, hr); err != nil {
		return nil, err
	}
	return factory, nil
}

// initCOM joins the thread to the multithreaded apartment, so that
// completions are delivered on Windows' threads and need no message loop of
// ours. A thread already in a single-threaded apartment is used as it is
// (waitHandles dispatches messages, so completions reach it too). The
// returned func undoes what was done; the caller keeps the thread locked
// until then.
func initCOM() (func(), error) {
	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	switch {
	case err == nil, errors.Is(err, syscall.Errno(sFalse)):
		return windows.CoUninitialize, nil
	case errors.Is(err, syscall.Errno(rpcEChangedMode)):
		return func() {}, nil
	}
	return nil, fmt.Errorf("start COM: %w", err)
}

// handler is a COM object implementing IAsyncOperationCompletedHandler<T> for
// one operation: when the operation ends it signals event. COM reads the
// first word as the vtable, so vtbl must stay first. The Go heap does not
// move objects, and await keeps the struct reachable for as long as Windows
// may call it.
type handler struct {
	vtbl  *[4]uintptr
	iid   windows.GUID // IAsyncOperationCompletedHandler<T> for this T
	refs  atomic.Int32
	event windows.Handle
}

// handlerVtbl is built once: syscall.NewCallback allocations are never
// freed, so the callbacks are shared by every handler.
var handlerVtbl = sync.OnceValue(func() *[4]uintptr {
	return &[4]uintptr{
		syscall.NewCallback(handlerQueryInterface),
		syscall.NewCallback(handlerAddRef),
		syscall.NewCallback(handlerRelease),
		syscall.NewCallback(handlerInvoke),
	}
})

func handlerQueryInterface(this, riid, out unsafe.Pointer) uintptr {
	h := (*handler)(this)
	// IAgileObject tells the runtime it may call from any thread without
	// marshalling, which a plain Go callback could not survive.
	switch *(*windows.GUID)(riid) {
	case iidIUnknown, iidIAgileObject, h.iid:
		*(*unsafe.Pointer)(out) = this
		h.refs.Add(1)
		return 0
	}
	*(*unsafe.Pointer)(out) = nil
	return eNoInterface
}

func handlerAddRef(this unsafe.Pointer) uintptr { return uintptr(uint32((*handler)(this).refs.Add(1))) }

func handlerRelease(this unsafe.Pointer) uintptr {
	return uintptr(uint32((*handler)(this).refs.Add(-1)))
}

// handlerInvoke runs when the operation ends, however it ends (completed,
// cancelled or failed): GetResults reports which, so the status is not read.
func handlerInvoke(this, _ unsafe.Pointer, _ uintptr) uintptr {
	_ = windows.SetEvent((*handler)(this).event)
	return 0
}

// abandoned keeps handlers that Windows may still call alive for the rest of
// the process, for an operation that never reported back after being
// cancelled. A few dozen bytes is the price of never freeing memory COM may
// still use.
var abandoned struct {
	sync.Mutex
	handlers []*handler // guarded by the mutex
}

// waitHandles blocks until one of handles is signalled (its index), or the
// timeout passes (-1). It dispatches the messages that arrive meanwhile,
// which a thread in a single-threaded apartment needs to be called back.
func waitHandles(handles []windows.Handle, timeoutMS uint32) (int, error) {
	for {
		r, _, err := procMsgWaitForMultipleObjectsEx.Call(uintptr(len(handles)), uintptr(unsafe.Pointer(&handles[0])),
			uintptr(timeoutMS), qsAllInput, mwmoInputAvail)
		switch {
		case r < uintptr(len(handles)):
			return int(r), nil
		case r == uintptr(len(handles)):
			pumpMessages()
		case r == waitTimeout:
			return -1, nil
		default:
			return 0, fmt.Errorf("wait for Windows: %w", err)
		}
	}
}

// pumpMessages dispatches the messages queued for this thread.
func pumpMessages() {
	var msg struct { // MSG
		hwnd    uintptr
		message uint32
		wParam  uintptr
		lParam  uintptr
		time    uint32
		x, y    int32
		private uint32
	}
	for {
		got, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, pmRemove)
		if got == 0 {
			return
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// await waits for the IAsyncOperation op to end without polling: Windows
// signals an event through a completion handler (handlerIID is that handler's
// interface for op's result type). If ctx ends first, op is cancelled and
// ctx's error returned. It returns nil once op has ended, however it ended;
// the caller reads the outcome with GetResults.
func await(ctx context.Context, op unsafe.Pointer, handlerIID windows.GUID) error {
	done, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return fmt.Errorf("create event: %w", err)
	}
	stopped, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(done)
		return fmt.Errorf("create event: %w", err)
	}
	h := &handler{vtbl: handlerVtbl(), iid: handlerIID, event: done}
	h.refs.Store(1)
	if err := hrCheck("watch the operation", vcall(op, slotPutCompleted, uintptr(unsafe.Pointer(h)))); err != nil {
		_ = windows.CloseHandle(done)
		_ = windows.CloseHandle(stopped)
		return err
	}
	ran := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(ran)
		_ = windows.SetEvent(stopped)
	})
	defer func() {
		// The handle is closed only once nothing can still signal it.
		if !stop() {
			<-ran
		}
		_ = windows.CloseHandle(stopped)
	}()

	i, err := waitHandles([]windows.Handle{done, stopped}, infinite)
	if err == nil && i == 0 {
		_ = windows.CloseHandle(done)
		runtime.KeepAlive(h)
		return nil
	}
	if err == nil {
		err = ctx.Err()
	}
	cancelOperation(op)
	if j, werr := waitHandles([]windows.Handle{done}, cancelSettleMS); werr == nil && j == 0 {
		_ = windows.CloseHandle(done)
		runtime.KeepAlive(h)
		return err
	}
	// Windows has not reported back yet, so it may still signal done and call h
	// later: both are kept.
	abandoned.Lock()
	abandoned.handlers = append(abandoned.handlers, h)
	abandoned.Unlock()
	return err
}

// cancelOperation asks Windows to stop op, which closes its dialog.
func cancelOperation(op unsafe.Pointer) {
	var info unsafe.Pointer
	if vcall(op, slotQueryInterface, uintptr(unsafe.Pointer(&iidIAsyncInfo)), uintptr(unsafe.Pointer(&info))) != 0 {
		return
	}
	defer release(info)
	vcall(info, slotCancel)
}

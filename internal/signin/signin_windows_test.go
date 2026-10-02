//go:build windows

package signin

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestOnlyTheUsersOwnRefusalEndsHelloAndAnythingElseFallsBackToThePassword(t *testing.T) {
	for result, want := range map[int32]helloVerdict{
		consentVerified:         helloVerified,
		consentCanceled:         helloRefused,
		consentRetriesExhausted: helloRefused,
		consentDeviceBusy:       helloRefused,
		1:                       helloAbsent, // no device
		2:                       helloAbsent, // not set up for this user
		3:                       helloAbsent, // disabled by policy
		99:                      helloAbsent, // a value a newer Windows adds
	} {
		if got := classifyConsent(result); got != want {
			t.Errorf("result %d: verdict %d, want %d", result, got, want)
		}
	}
}

func TestVerifyDoesNotAskAnythingOfACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// A dialog here would block the test for as long as someone left it up.
	if err := Verify(ctx, 0, "test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify with a cancelled context: %v", err)
	}
}

// The handler Windows calls back is a COM object written in Go, so it is
// exercised through its vtable exactly as Windows would.

func TestTheCompletionHandlerAnswersTheInterfacesWindowsAsksFor(t *testing.T) {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(event) }()
	h := onHeap(&handler{vtbl: handlerVtbl(), iid: iidResultHandler, event: event})
	h.refs.Store(1)
	obj := unsafe.Pointer(h)

	for name, iid := range map[string]windows.GUID{"IUnknown": iidIUnknown, "IAgileObject": iidIAgileObject, "its own interface": iidResultHandler} {
		var out unsafe.Pointer
		if hr := vcall(obj, slotQueryInterface, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out))); hr != 0 || out != obj {
			t.Errorf("QueryInterface(%s) = %#x, %v", name, hr, out)
		}
	}
	if got := h.refs.Load(); got != 4 {
		t.Fatalf("references after three queries = %d, want 4", got)
	}
	var out unsafe.Pointer
	if hr := vcall(obj, slotQueryInterface, uintptr(unsafe.Pointer(&iidAvailabilityHandler)), uintptr(unsafe.Pointer(&out))); uint32(hr) != eNoInterface || out != nil {
		t.Errorf("QueryInterface(another handler) = %#x, %v; want E_NOINTERFACE", hr, out)
	}
	if got := vcall(obj, 1); got != 5 {
		t.Errorf("AddRef = %d, want 5", got)
	}
	if got := vcall(obj, slotRelease); got != 4 {
		t.Errorf("Release = %d, want 4", got)
	}

	if got, _ := windows.WaitForSingleObject(event, 0); got != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("event signalled before Invoke: %d", got)
	}
	if hr := vcall(obj, 3, 0, 1); hr != 0 {
		t.Fatalf("Invoke = %#x", hr)
	}
	if got, _ := windows.WaitForSingleObject(event, 0); got != windows.WAIT_OBJECT_0 {
		t.Fatalf("event after Invoke: %d", got)
	}
}

// fakeOp is an IAsyncOperation written in Go, so that await can be tested
// without Windows Hello. Like the real one it calls the completion handler
// from another thread, or at once if it is already done.
type fakeOp struct {
	vtbl    *[9]uintptr
	info    fakeInfo
	mu      sync.Mutex
	handler unsafe.Pointer // guarded by mu
	when    func(op *fakeOp)
	result  int32
	failure uintptr // GetResults' HRESULT
	stopped atomic.Bool
}

type fakeInfo struct {
	vtbl *[11]uintptr
	op   *fakeOp
}

// complete tells the handler the operation has ended.
func (o *fakeOp) complete() {
	o.mu.Lock()
	h := o.handler
	o.mu.Unlock()
	vcall(h, 3, uintptr(unsafe.Pointer(o)), 1)
}

var fakeVtables = sync.OnceValues(func() (*[9]uintptr, *[11]uintptr) {
	ok := func(...unsafe.Pointer) uintptr { return 0 }
	addRef := syscall.NewCallback(func(unsafe.Pointer) uintptr { return 2 })
	rel := syscall.NewCallback(func(unsafe.Pointer) uintptr { return 1 })
	op := &[9]uintptr{
		syscall.NewCallback(func(this, riid, out unsafe.Pointer) uintptr {
			o := (*fakeOp)(this)
			if *(*windows.GUID)(riid) == iidIAsyncInfo {
				*(*unsafe.Pointer)(out) = unsafe.Pointer(&o.info)
				return 0
			}
			*(*unsafe.Pointer)(out) = this
			return 0
		}),
		addRef, rel,
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }), // GetIids
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }), // GetRuntimeClassName
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }), // GetTrustLevel
		syscall.NewCallback(func(this, handler unsafe.Pointer) uintptr { // put_Completed
			o := (*fakeOp)(this)
			vcall(handler, 1) // AddRef, as a real operation does
			o.mu.Lock()
			o.handler = handler
			o.mu.Unlock()
			o.when(o)
			return 0
		}),
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }), // get_Completed
		syscall.NewCallback(func(this, out unsafe.Pointer) uintptr { // GetResults
			o := (*fakeOp)(this)
			*(*int32)(out) = o.result
			return o.failure
		}),
	}
	info := &[11]uintptr{
		syscall.NewCallback(func(this, _, out unsafe.Pointer) uintptr { *(*unsafe.Pointer)(out) = this; return 0 }),
		addRef, rel,
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }),
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }),
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }),
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }), // get_Id
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }), // get_Status
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }), // get_ErrorCode
		syscall.NewCallback(func(this unsafe.Pointer) uintptr { // Cancel
			o := (*fakeInfo)(this).op
			o.stopped.Store(true)
			go o.complete()
			return 0
		}),
		syscall.NewCallback(func(unsafe.Pointer) uintptr { return ok() }), // Close
	}
	return op, info
})

func newFakeOp(when func(*fakeOp)) *fakeOp {
	opTable, infoTable := fakeVtables()
	o := onHeap(&fakeOp{vtbl: opTable, when: when})
	o.info = fakeInfo{vtbl: infoTable, op: o}
	return o
}

func TestAwaitReturnsWhenTheOperationCompletes(t *testing.T) {
	for name, when := range map[string]func(*fakeOp){
		"later, from another thread":  func(o *fakeOp) { go func() { time.Sleep(20 * time.Millisecond); o.complete() }() },
		"already, before it is asked": func(o *fakeOp) { o.complete() },
	} {
		t.Run(name, func(t *testing.T) {
			o := newFakeOp(when)
			o.result = 6
			if err := awaitSoon(t.Context(), t, o); err != nil {
				t.Fatalf("await: %v", err)
			}
			var got int32
			if hr := vcall(unsafe.Pointer(o), slotGetResults, uintptr(unsafe.Pointer(&got))); hr != 0 || got != 6 {
				t.Fatalf("result %d, %#x", got, hr)
			}
		})
	}
}

func TestAwaitCancelsTheOperationWhenTheContextEnds(t *testing.T) {
	o := newFakeOp(func(*fakeOp) {}) // never completes by itself
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := awaitSoon(ctx, t, o)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("await: %v, want the context's error", err)
	}
	if !o.stopped.Load() {
		t.Fatal("the operation was not cancelled")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("await took %v to notice the context", took)
	}
}

func TestOperationResultReportsAFailedOperation(t *testing.T) {
	o := newFakeOp(func(o *fakeOp) { o.complete() })
	o.failure = 0x80070057 // E_INVALIDARG
	_, err := operationResult(t.Context(), unsafe.Pointer(o), iidResultHandler)
	var hr hresultError
	if !errors.As(err, &hr) || hr != 0x80070057 {
		t.Fatalf("operationResult: %v, want the HRESULT", err)
	}
}

func TestACredentialBufferGivesBackTheUserAndPassword(t *testing.T) {
	user, _ := windows.UTF16PtrFromString(`LUMENTEST\someone`)
	password, _ := windows.UTF16PtrFromString("not-a-real-password")
	proc := windows.NewLazySystemDLL("credui.dll").NewProc("CredPackAuthenticationBufferW")
	var size uint32
	_, _, _ = proc.Call(0, uintptr(unsafe.Pointer(user)), uintptr(unsafe.Pointer(password)), 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		t.Fatal("no size for the packed credentials")
	}
	packed := make([]byte, size)
	if ok, _, err := proc.Call(0, uintptr(unsafe.Pointer(user)), uintptr(unsafe.Pointer(password)),
		uintptr(unsafe.Pointer(&packed[0])), uintptr(unsafe.Pointer(&size))); ok == 0 {
		t.Fatalf("pack: %v", err)
	}

	cred, err := unpackCredentials(unsafe.Pointer(&packed[0]), size)
	if err != nil {
		t.Fatal(err)
	}
	if got := windows.UTF16ToString(cred.password); got != "not-a-real-password" {
		t.Fatalf("password = %q", got)
	}
	if name, domain := logonName(cred.user, cred.domain); name != "someone" || domain != "LUMENTEST" {
		t.Fatalf("account = %q in %q, from %q and %q", name, domain, cred.user, cred.domain)
	}
	wipe(cred.password)
	for i, c := range cred.password {
		if c != 0 {
			t.Fatalf("password character %d survived the wipe", i)
		}
	}
}

func TestUnreadableCredentialsAreNotAVerifiedUser(t *testing.T) {
	junk := []byte("this is not an authentication buffer")
	if _, err := unpackCredentials(unsafe.Pointer(&junk[0]), uint32(len(junk))); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("unpack junk: %v", err)
	}
}

func TestAccountNamesAreGivenToLogonTheWayWindowsExpectsThem(t *testing.T) {
	for _, c := range []struct{ user, domain, name, from string }{
		{"bob", "PC", "bob", "PC"},                                      // a local account, already split
		{`PC\bob`, "", "bob", "PC"},                                     // a local account, not split
		{"bob", "", "bob", "."},                                         // no domain: this PC
		{"bob@outlook.com", "", "bob@outlook.com", ""},                  // a Microsoft account by e-mail
		{`MicrosoftAccount\bob@outlook.com`, "", "bob@outlook.com", ""}, // the same, qualified
		{"bob@outlook.com", "MicrosoftAccount", "bob@outlook.com", ""},
	} {
		if name, from := logonName(c.user, c.domain); name != c.name || from != c.from {
			t.Errorf("logonName(%q, %q) = %q, %q; want %q, %q", c.user, c.domain, name, from, c.name, c.from)
		}
	}
}

func TestAWrongPasswordIsRefusedByTheLogonCheck(t *testing.T) {
	// An account that does not exist, so that nothing real is ever locked out.
	password, err := windows.UTF16FromString("not-a-real-password")
	if err != nil {
		t.Fatal(err)
	}
	err = checkPassword(credentials{user: "lumen-test-no-such-user", password: password})
	if !errors.Is(err, errWrongPassword) {
		t.Fatalf("checkPassword: %v, want a wrong password", err)
	}
}

func TestTheProcessOwnTokenBelongsToTheSignedInUser(t *testing.T) {
	if same, err := sameUser(windows.GetCurrentProcessToken()); err != nil || !same {
		t.Fatalf("the process's own token: %v, %v", same, err)
	}
}

// kept holds the test's COM objects. Windows gets their addresses as plain
// numbers, which the runtime can't update if it moves a stack, so they are
// made reachable from a global, which puts them on the heap, where nothing
// moves.
var kept sync.Map

func onHeap[T any](v *T) *T {
	kept.Store(v, struct{}{})
	return v
}

// awaitSoon is await with a limit, so that a regression that makes it wait
// for ever fails the test instead of stalling the run.
func awaitSoon(ctx context.Context, t *testing.T, o *fakeOp) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- await(ctx, unsafe.Pointer(o), iidResultHandler) }()
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("await did not return")
		return nil
	}
}

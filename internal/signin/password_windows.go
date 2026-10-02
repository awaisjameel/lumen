//go:build windows

package signin

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The account password path, for a PC without Windows Hello: the system's
// own credential dialog (the one that asks for the password of the signed-in
// user and nobody else), then an interactive logon to check what was typed.

var (
	credui                              = windows.NewLazySystemDLL("credui.dll")
	procCredUIPromptForWindowsCredsW    = credui.NewProc("CredUIPromptForWindowsCredentialsW")
	procCredUnPackAuthenticationBufferW = credui.NewProc("CredUnPackAuthenticationBufferW")
	procLogonUserW                      = windows.NewLazySystemDLL("advapi32.dll").NewProc("LogonUserW")
)

const (
	credUIWinEnumerateCurrentUser = 0x200 // list only the signed-in user
	logon32LogonInteractive       = 2
	logon32ProviderDefault        = 0

	errorInsufficientBuffer = 122
	errorCancelled          = 1223
	errorLogonFailure       = 1326 // a wrong user name or password
	errorLogonTypeNotGrant  = 1385 // the account may not log on interactively here

	// passwordAttempts is how many times the dialog is shown after a wrong
	// password (each time saying so) before giving up.
	passwordAttempts = 3
)

// credUIInfo is CREDUI_INFOW.
type credUIInfo struct {
	size    uint32
	parent  uintptr // HWND
	message *uint16
	caption *uint16
	banner  uintptr // HBITMAP
}

// credentials is what the dialog returned. The password is a UTF-16 buffer
// with its terminating zero, never a string, so that wipe can erase it.
type credentials struct {
	user, domain string
	password     []uint16
}

// wipe zeroes b so a secret does not outlive its use in memory the garbage
// collector would leave as it was. The KeepAlive stops the compiler treating
// the writes as dead.
func wipe[T any](b []T) {
	clear(b)
	runtime.KeepAlive(b)
}

// verifyPassword asks for the signed-in user's password and checks it.
func verifyPassword(ctx context.Context, owner uintptr, reason string) error {
	var authError uint32 // set after a wrong password, so the dialog says so
	for range passwordAttempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		cred, err := promptCredentials(owner, reason, authError)
		if err != nil {
			return err
		}
		err = checkPassword(cred)
		wipe(cred.password)
		switch {
		case err == nil:
			// The dialog can't be closed by ctx, so it may have ended while the
			// user answered; what they were asked for is no longer wanted.
			return ctx.Err()
		case errors.Is(err, errWrongPassword):
			authError = errorLogonFailure
		default:
			return err
		}
	}
	return fmt.Errorf("%d wrong passwords: %w", passwordAttempts, ErrNotVerified)
}

// promptCredentials shows the system's sign-in dialog for the current user
// and returns what was typed. A cancelled dialog is ErrNotVerified.
func promptCredentials(owner uintptr, reason string, authError uint32) (credentials, error) {
	message, err := windows.UTF16PtrFromString(reason)
	if err != nil {
		return credentials{}, err
	}
	caption, err := windows.UTF16PtrFromString("Lumen")
	if err != nil {
		return credentials{}, err
	}
	info := credUIInfo{parent: owner, message: message, caption: caption}
	info.size = uint32(unsafe.Sizeof(info))
	var (
		authPackage uint32 // 0: the Negotiate package, which signs in local and Microsoft accounts alike
		out         unsafe.Pointer
		outSize     uint32
	)
	rc, _, _ := procCredUIPromptForWindowsCredsW.Call(uintptr(unsafe.Pointer(&info)), uintptr(authError),
		uintptr(unsafe.Pointer(&authPackage)), 0, 0, uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&outSize)),
		0, credUIWinEnumerateCurrentUser)
	switch syscall.Errno(rc) {
	case 0:
	case errorCancelled:
		return credentials{}, ErrNotVerified
	default:
		return credentials{}, fmt.Errorf("ask for the account password: %w (%w)", ErrUnavailable, syscall.Errno(rc))
	}
	// The packed buffer holds the password in a form that is easy to read
	// back, and it is system memory: erase it before it is freed.
	defer func() {
		wipe(unsafe.Slice((*byte)(out), outSize))
		windows.CoTaskMemFree(out)
	}()
	return unpackCredentials(out, outSize)
}

// unpackCredentials reads the user name, domain and password out of an
// authentication buffer.
func unpackCredentials(buf unsafe.Pointer, size uint32) (credentials, error) {
	var userLen, domainLen, passwordLen uint32
	// The first call, with no room, says how much room is needed.
	ok, _, err := procCredUnPackAuthenticationBufferW.Call(0, uintptr(buf), uintptr(size), 0, uintptr(unsafe.Pointer(&userLen)),
		0, uintptr(unsafe.Pointer(&domainLen)), 0, uintptr(unsafe.Pointer(&passwordLen)))
	if ok != 0 || !errors.Is(err, syscall.Errno(errorInsufficientBuffer)) {
		return credentials{}, fmt.Errorf("read the credentials: %w", errors.Join(ErrNotVerified, err))
	}
	user := make([]uint16, max(userLen, 1))
	domain := make([]uint16, max(domainLen, 1))
	password := make([]uint16, max(passwordLen, 1))
	ok, _, err = procCredUnPackAuthenticationBufferW.Call(0, uintptr(buf), uintptr(size),
		uintptr(unsafe.Pointer(&user[0])), uintptr(unsafe.Pointer(&userLen)),
		uintptr(unsafe.Pointer(&domain[0])), uintptr(unsafe.Pointer(&domainLen)),
		uintptr(unsafe.Pointer(&password[0])), uintptr(unsafe.Pointer(&passwordLen)))
	if ok == 0 {
		wipe(password)
		return credentials{}, fmt.Errorf("read the credentials: %w", errors.Join(ErrNotVerified, err))
	}
	return credentials{user: windows.UTF16ToString(user), domain: windows.UTF16ToString(domain), password: password}, nil
}

var (
	// errWrongPassword is a failed logon the user may try again.
	errWrongPassword = errors.New("wrong user name or password")
	// errOtherAccount means the password was good, but for someone else.
	errOtherAccount = errors.New("signed in as a different account")
)

// logonName chooses how to name the account to LogonUserW. The dialog's
// result is "PC\name" for a local account (the domain is split off already
// or not, so both are handled) and, for a Microsoft account, the e-mail
// address, sometimes as "MicrosoftAccount\name@outlook.com". Windows signs a
// Microsoft account in by its e-mail address as a user principal name with no
// domain, so anything with an @ is passed that way; a local account is
// qualified by its PC (or "." for this one).
func logonName(user, domain string) (name, from string) {
	if domain == "" {
		if before, after, ok := strings.Cut(user, `\`); ok {
			domain, user = before, after
		}
	}
	if strings.Contains(user, "@") {
		return user, ""
	}
	if domain == "" {
		domain = "."
	}
	return user, domain
}

// checkPassword logs the user in interactively with the password the dialog
// returned and confirms the account is the one running Lumen. The token is
// closed straight away: it proves the password and is used for nothing else.
func checkPassword(cred credentials) error {
	name, domain := logonName(cred.user, cred.domain)
	user, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	var domainPtr *uint16
	if domain != "" {
		if domainPtr, err = windows.UTF16PtrFromString(domain); err != nil {
			return err
		}
	}
	var token windows.Token
	ok, _, errno := procLogonUserW.Call(uintptr(unsafe.Pointer(user)), uintptr(unsafe.Pointer(domainPtr)),
		uintptr(unsafe.Pointer(&cred.password[0])), logon32LogonInteractive, logon32ProviderDefault,
		uintptr(unsafe.Pointer(&token)))
	if ok == 0 {
		switch {
		case errors.Is(errno, syscall.Errno(errorLogonFailure)):
			return errWrongPassword
		case errors.Is(errno, syscall.Errno(errorLogonTypeNotGrant)):
			return fmt.Errorf("log on to check the password: %w (%w)", ErrUnavailable, errno)
		}
		// Expired, locked out, disabled or restricted: the password can't be
		// accepted, whatever it is.
		return fmt.Errorf("log on to check the password: %w", errors.Join(ErrNotVerified, errno))
	}
	defer func() { _ = token.Close() }()
	same, err := sameUser(token)
	if err != nil {
		return err
	}
	if !same {
		return errors.Join(ErrNotVerified, errOtherAccount)
	}
	return nil
}

// sameUser reports whether token belongs to the user running this process.
// The dialog lists only that user, but the answer is checked rather than
// trusted: another account's password must not reset this one's PIN.
func sameUser(token windows.Token) (bool, error) {
	got, err := token.GetTokenUser()
	if err != nil {
		return false, fmt.Errorf("read the signed-in user: %w", err)
	}
	want, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false, fmt.Errorf("read the current user: %w", err)
	}
	return windows.EqualSid(got.User.Sid, want.User.Sid), nil
}

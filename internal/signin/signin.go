// Package signin asks Windows to confirm that the person at the keyboard is
// the signed-in user. The Blocked screen uses it to let someone who forgot
// the PIN choose a new one: the PIN guards the list from other people, so
// the reset needs the same proof of identity as signing in to the PC.
package signin

import (
	"context"
	"errors"
)

var (
	// ErrNotVerified means the user cancelled the Windows prompt, or failed
	// it (a wrong password, too many wrong Hello attempts).
	ErrNotVerified = errors.New("could not verify the Windows sign-in")
	// ErrUnavailable means this computer offers no way to check: Windows
	// Hello is not set up and the account password prompt cannot be used
	// (or this is not Windows).
	ErrUnavailable = errors.New("no Windows sign-in is available")
)

// Verify returns nil when the user proved who they are, ErrNotVerified when
// they cancelled or failed, and ErrUnavailable when no method exists. owner
// is the handle of the window the prompt belongs to (0 when there is none).
// reason is shown in Windows Hello's own words; the password prompt shows it
// as its message.
//
// Windows Hello (face, fingerprint or its PIN) is used when it is set up.
// Otherwise the user types their account password in the system's own
// credential dialog, and it is checked with an interactive logon of the same
// account, so a local account and a Microsoft account both work. The
// password never leaves this package and is erased as soon as it is checked.
//
// The prompt blocks until the user answers. While Hello is showing, ctx
// cancels it. The password dialog can't be closed from outside, so ctx is
// checked before it appears and after the user answers.
//
// Verify takes over the calling goroutine's thread for the call, and is safe
// to call from any goroutine.
func Verify(ctx context.Context, owner uintptr, reason string) error {
	return verify(ctx, owner, reason)
}

//go:build windows

package signin

import (
	"context"
	"fmt"
	"log/slog"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows Hello, through the WinRT class UserConsentVerifier. The interface
// ids are those of the Windows SDK's headers (windows.security.credentials.ui.h
// and UserConsentVerifierInterop.h), checked against them by hand and, for
// the ones the completion handler answers to, by the real calls working
// (see the throwaway program the unit's report describes).
const userConsentVerifierClass = "Windows.Security.Credentials.UI.UserConsentVerifier"

var (
	// IUserConsentVerifierStatics: CheckAvailabilityAsync (slot 6) and
	// RequestVerificationAsync (slot 7).
	iidConsentStatics = mustGUID("{AF4F3F91-564C-4DDC-B8B5-973447627C65}")
	// IUserConsentVerifierInterop: RequestVerificationForWindowAsync (slot 6),
	// the variant that parents Hello's dialog to a window.
	iidConsentInterop = mustGUID("{39E050C3-4E74-441A-8DC0-B81104DF949C}")
	// IAsyncOperation<UserConsentVerificationResult>, which is what the
	// request returns, and the completion handler for it.
	iidResultOperation = mustGUID("{FD596FFD-2318-558F-9DBE-D21DF43764A5}")
	iidResultHandler   = mustGUID("{0CFFC6C9-4C2B-5CD4-B38C-7B8DF3FF5AFB}")
	// The completion handler for IAsyncOperation<UserConsentVerifierAvailability>.
	iidAvailabilityHandler = mustGUID("{28988174-ACE2-5C15-A0DF-580A26D94294}")
)

const (
	slotCheckAvailability = 6
	slotRequestVerify     = 7
	slotRequestForWindow  = 6
)

// UserConsentVerifierAvailability and UserConsentVerificationResult values.
const (
	helloAvailable = 0

	consentVerified         = 0
	consentRetriesExhausted = 5
	consentCanceled         = 6
	consentDeviceBusy       = 4
)

// helloVerdict is what Windows Hello made of the request.
type helloVerdict int

const (
	helloVerified helloVerdict = iota // the user proved who they are
	helloRefused                      // they cancelled or failed
	helloAbsent                       // Hello can't be used: ask for the password
)

// classifyConsent maps a UserConsentVerificationResult. Anything that says
// Hello could not run (no device, not set up, off by policy, or a value a
// newer Windows adds) hands over to the password, which proves identity as
// well; only a refusal by the user, or a busy device, ends the attempt.
func classifyConsent(result int32) helloVerdict {
	switch result {
	case consentVerified:
		return helloVerified
	case consentCanceled, consentRetriesExhausted, consentDeviceBusy:
		return helloRefused
	}
	return helloAbsent
}

// helloVerify asks Windows Hello when it is set up. The error is only ever
// ctx's: a Hello that can't be reached for any other reason is logged and
// reported as helloAbsent, so the password can still be used.
func helloVerify(ctx context.Context, owner uintptr, reason string) (helloVerdict, error) {
	verdict, err := tryHello(ctx, owner, reason)
	if err == nil {
		return verdict, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return helloAbsent, ctxErr
	}
	slog.Warn("use windows hello", "err", err)
	return helloAbsent, nil
}

func tryHello(ctx context.Context, owner uintptr, reason string) (helloVerdict, error) {
	undo, err := initCOM()
	if err != nil {
		return helloAbsent, err
	}
	defer undo()
	statics, err := activate(userConsentVerifierClass, &iidConsentStatics)
	if err != nil {
		return helloAbsent, err
	}
	defer release(statics)

	var op unsafe.Pointer
	if err := hrCheck("check Windows Hello", vcall(statics, slotCheckAvailability, uintptr(unsafe.Pointer(&op)))); err != nil {
		return helloAbsent, err
	}
	availability, err := operationResult(ctx, op, iidAvailabilityHandler)
	if err != nil {
		return helloAbsent, err
	}
	if availability != helloAvailable {
		slog.Info("windows hello is not available; using the password", "availability", availability)
		return helloAbsent, nil
	}

	message, err := newHString(reason)
	if err != nil {
		return helloAbsent, err
	}
	defer message.free()
	if owner == 0 {
		// No window to parent the dialog to: the plain request.
		err = hrCheck("ask Windows Hello", vcall(statics, slotRequestVerify, uintptr(message), uintptr(unsafe.Pointer(&op))))
	} else {
		var interop unsafe.Pointer
		if err := hrCheck("find Windows Hello's window interface",
			vcall(statics, slotQueryInterface, uintptr(unsafe.Pointer(&iidConsentInterop)), uintptr(unsafe.Pointer(&interop)))); err != nil {
			return helloAbsent, err
		}
		defer release(interop)
		err = hrCheck("ask Windows Hello", vcall(interop, slotRequestForWindow, owner, uintptr(message),
			uintptr(unsafe.Pointer(&iidResultOperation)), uintptr(unsafe.Pointer(&op))))
	}
	if err != nil {
		return helloAbsent, err
	}
	result, err := operationResult(ctx, op, iidResultHandler)
	if err != nil {
		return helloAbsent, err
	}
	return classifyConsent(result), nil
}

// operationResult waits for an IAsyncOperation of an enumeration to end,
// reads its result and releases it.
func operationResult(ctx context.Context, op unsafe.Pointer, handlerIID windows.GUID) (int32, error) {
	defer release(op)
	if err := await(ctx, op, handlerIID); err != nil {
		return 0, err
	}
	var result int32
	if err := hrCheck("read Windows Hello's answer", vcall(op, slotGetResults, uintptr(unsafe.Pointer(&result)))); err != nil {
		return 0, fmt.Errorf("windows hello did not finish: %w", err)
	}
	return result, nil
}

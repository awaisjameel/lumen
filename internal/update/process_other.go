//go:build !windows

package update

import "context"

func startHelper(context.Context, string, string, string) error      { return ErrUnavailable }
func launchHealth(context.Context, string, string, Activation) error { return ErrUnavailable }
func restoreLibrary(context.Context, string, Activation) error       { return ErrUnavailable }
func updateRegistration(string, string) error                        { return ErrUnavailable }

// Launch refuses to execute a Windows runtime on unsupported systems.
func Launch(context.Context, string, string, []string) error { return ErrUnavailable }

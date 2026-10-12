//go:build !windows

package update

import "context"

func startHelper(context.Context, string, string, string, string) error { return ErrUnavailable }
func launchHealth(context.Context, string, string, Activation) error    { return ErrUnavailable }
func restoreLibrary(context.Context, string, Activation) error          { return ErrUnavailable }
func relaunch(context.Context, string, string) error                    { return ErrUnavailable }
func register(string, string)                                           {}

// Launch refuses to execute a Windows runtime on unsupported systems.
func Launch(context.Context, string, string, []string) error { return ErrUnavailable }

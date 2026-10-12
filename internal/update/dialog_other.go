//go:build !windows

package update

// ShowFailure has no native Windows dialog on unsupported platforms.
func ShowFailure(error) {}

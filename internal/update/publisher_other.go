//go:build !windows

package update

import "context"

// VerifyPublisher refuses to run downloaded code outside Windows.
func VerifyPublisher(context.Context, string, []string) error { return ErrPublisher }

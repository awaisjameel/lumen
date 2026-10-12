//go:build !windows

package update

import (
	"context"
	"os"
)

func replace(from, to string) error    { return os.Rename(from, to) }
func freeSpace(string) (uint64, error) { return 0, ErrUnavailable }
func windowsBuild() uint32             { return 0 }
func noLinks(string) error             { return ErrUnavailable }

// Lease is available only on Windows.
type Lease struct{}

// AcquireLease refuses activation on unsupported operating systems.
func AcquireLease(context.Context, string, bool) (*Lease, error) { return nil, ErrUnavailable }

// Close is inert on an unsupported platform.
func (*Lease) Close() error { return nil }

func processIdentity() (uint32, uint64, error)         { return 0, 0, ErrUnavailable }
func waitParent(context.Context, uint32, uint64) error { return ErrUnavailable }

//go:build !windows

package update

import "os"

// ProtectSecrets restricts external private-key directories to their owner.
func ProtectSecrets(dir string) error { return os.Chmod(dir, 0o700) }

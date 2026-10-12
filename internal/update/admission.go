package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Admission is a running application's place in its installation: the shared
// runtime lease that keeps activation out while it runs.
type Admission struct {
	Install Installation
	// Health is the activation whose first, health-gated launch this is; nil
	// for every ordinary launch.
	Health *Activation
	lease  *Lease
}

// Admit checks that executable may run now: that it is the generation its
// installation selects, and that no activation or installation is under way.
// A build that is not an installation (a source checkout, a portable copy)
// is admitted with nothing to hold.
func Admit(ctx context.Context, executable, version, dataDir string, args []string) (*Admission, error) {
	id := HealthToken(args)
	install, err := DetectInstallation(executable)
	if errors.Is(err, ErrUnavailable) && id == "" {
		return &Admission{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := CheckSelected(install.Root, executable, version); err != nil {
		return nil, err
	}
	if id == "" {
		// Only the helper's own first launch passes an activation's barrier.
		if _, err := os.Lstat(filepath.Join(install.Root, barrierFile)); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrState
		}
	}
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	lease, err := AcquireLease(wait, install.Root, false)
	if err != nil {
		return nil, err
	}
	a := &Admission{Install: install, lease: lease}
	if id != "" {
		activation, err := ValidateHealth(install, id, dataDir, version)
		if err != nil {
			return nil, errors.Join(err, lease.Close())
		}
		a.Health = &activation
	}
	return a, nil
}

// Close lets activation in again, once everything this process ran has stopped.
func (a *Admission) Close() error {
	if a.lease == nil {
		return nil
	}
	return a.lease.Close()
}

package update

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"lumen/internal/core"
)

// CheckSelected refuses manually launching a stale installed generation.
func CheckSelected(root, executable, version string) error {
	var active Active
	if err := readJSON(filepath.Join(root, ".lumen-active.json"), &active); err != nil {
		return err
	}
	if active.Format != 1 || active.Version != version || filepath.Base(filepath.Dir(executable)) != active.Generation {
		return ErrInvalid
	}
	return nil
}

type recoveryRecord struct {
	path       string
	activation Activation
}

func (m *Manager) recoveries() ([]recoveryRecord, error) {
	paths, err := filepath.Glob(filepath.Join(m.dir, "recoveries", "*.json"))
	if err != nil {
		return nil, err
	}
	paths = append(paths, filepath.Join(m.dir, "activation.json"))
	var records []recoveryRecord
	for _, name := range paths {
		var a Activation
		if err := readJSON(name, &a); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if a.Install != m.install || a.Format != 1 {
			return nil, ErrInvalid
		}
		if a.Phase == core.UpdateComplete && !a.RecoveryPruned {
			records = append(records, recoveryRecord{path: name, activation: a})
		}
	}
	return records, nil
}

// PruneRecovery preserves every generation still covered by a seven-day grace
// period, including successive upgrades, and removes only inventoried old files.
func (m *Manager) PruneRecovery(ctx context.Context) error {
	if m.owner == nil {
		return nil
	}
	records, err := m.recoveries()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	var active Active
	if err := readJSON(filepath.Join(m.install.Root, ".lumen-active.json"), &active); err != nil {
		return err
	}
	if active.Pending != "" {
		return nil
	}
	protected := map[string]bool{active.Generation: true}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	for _, record := range records {
		a := record.activation
		if a.CompletedAt == 0 || a.CompletedAt > cutoff {
			protected[a.Previous.Generation] = true
		}
	}
	for _, record := range records {
		a := record.activation
		if a.CompletedAt == 0 || a.CompletedAt > cutoff {
			continue
		}
		decoded, err := hex.DecodeString(a.Token)
		if err != nil || len(decoded) != 16 {
			return ErrInvalid
		}
		if !protected[a.Previous.Generation] {
			if err := removeOwnedGeneration(ctx, m.install.Root, a.Previous.Generation, a.Previous.Release); err != nil {
				return err
			}
		}
		if a.Backup != filepath.Join(m.dir, "library-"+a.Token+".db") {
			return ErrInvalid
		}
		if _, err := os.Lstat(a.Backup); err == nil {
			if err := noLinks(a.Backup); err != nil {
				return err
			}
			if err := os.Remove(a.Backup); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		a.RecoveryPruned = true
		if err := writeJSON(ctx, record.path, a); err != nil {
			return err
		}
	}
	return nil
}

func removeOwnedGeneration(ctx context.Context, root, generation string, release Release) error {
	if !safePath(generation) || filepath.Base(generation) != generation {
		return ErrInvalid
	}
	if _, err := release.validate(); err != nil {
		return err
	}
	base := filepath.Join(root, "runtime", generation)
	for _, file := range release.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.Join(base, filepath.FromSlash(file.Path))
		if _, err := os.Lstat(name); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := noLinks(name); err != nil {
			return err
		}
		if err := os.Remove(name); err != nil {
			return err
		}
		for parent := filepath.Dir(name); parent != filepath.Dir(base); parent = filepath.Dir(parent) {
			if err := os.Remove(parent); err != nil {
				break
			}
		}
	}
	return nil
}

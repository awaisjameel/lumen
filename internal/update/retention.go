package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lumen/internal/core"
)

// recoveryPeriod is how long a healthy update keeps the way back: the
// previous generation and the library backup taken before it.
const recoveryPeriod = 7 * 24 * time.Hour

// CheckSelected refuses manually launching a stale installed generation.
func CheckSelected(root, executable, version string) error {
	active, err := readActive(root)
	if err != nil {
		return err
	}
	if active.Version != version || filepath.Base(filepath.Dir(executable)) != active.Generation {
		return ErrInvalid
	}
	return nil
}

type recoveryRecord struct {
	path       string
	activation Activation
}

// recoveries lists the completed activations whose recovery material is
// still kept: the current record and those archived before later updates.
func recoveries(dir string, install Installation) ([]recoveryRecord, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "recoveries", "*.json"))
	if err != nil {
		return nil, err
	}
	paths = append(paths, filepath.Join(dir, activationFile))
	var records []recoveryRecord
	for _, name := range paths {
		var a Activation
		if err := readJSON(name, &a); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if a.Install != install || a.Format != 1 {
			return nil, ErrInvalid
		}
		if a.Phase == core.UpdateComplete && !a.RecoveryPruned {
			records = append(records, recoveryRecord{path: name, activation: a})
		}
	}
	return records, nil
}

// PruneRecovery preserves every generation still covered by a seven-day grace
// period, including successive upgrades, and removes only inventoried old
// files. It then clears what settled activations left in the state folder.
func (m *Manager) PruneRecovery(ctx context.Context) error {
	if m.owner == nil {
		return nil
	}
	active, err := readActive(m.install.Root)
	if err != nil {
		return err
	}
	if active.Pending != "" {
		return nil
	}
	records, err := recoveries(m.dir, m.install)
	if err != nil {
		return err
	}
	protected := map[string]bool{active.Generation: true}
	kept := make(map[string]bool) // tokens whose backup is still owed
	// An activation still under way owns its backup, whatever its record says.
	var current Activation
	if readJSON(filepath.Join(m.dir, activationFile), &current) == nil && current.Phase != core.UpdateComplete && current.Phase != core.UpdateFailed {
		kept[current.Token] = true
	}
	cutoff := time.Now().Add(-recoveryPeriod).Unix()
	young := func(a Activation) bool { return a.CompletedAt == 0 || a.CompletedAt > cutoff }
	for _, record := range records {
		if a := record.activation; young(a) {
			protected[a.Previous.Generation] = true
			kept[a.Token] = true
		}
	}
	for _, record := range records {
		a := record.activation
		if young(a) {
			continue
		}
		if !validToken(a.Token) || a.Backup != backupFile(m.dir, a.Token) {
			return ErrInvalid
		}
		if !protected[a.Previous.Generation] {
			if err := removeOwnedGeneration(ctx, m.install.Root, a.Previous.Generation, a.Previous.Release); err != nil {
				return err
			}
		}
		a.RecoveryPruned = true
		if err := writeJSON(ctx, record.path, a); err != nil {
			return err
		}
	}
	return m.sweep(kept)
}

// sweep removes the state folder's leftovers that no kept recovery owns:
// backups of updates that failed (their library was never replaced, or was
// restored) or aged out, the helper's acknowledgements, and what a crash left
// of a metadata refresh or an atomic write. Each is one of the updater's own
// named files; nothing else in the folder is touched.
func (m *Manager) sweep(kept map[string]bool) error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return err
	}
	var failed error
	for _, entry := range entries {
		name := entry.Name()
		owned := strings.HasPrefix(name, ".tuf-") || strings.HasPrefix(name, ".lumen-state-")
		for _, pattern := range []struct{ prefix, suffix string }{{"library-", ".db"}, {"closed-", ".json"}, {"helper-ready-", ".json"}} {
			if id, ok := strings.CutPrefix(name, pattern.prefix); ok {
				if id, ok = strings.CutSuffix(id, pattern.suffix); ok && validToken(id) && !kept[id] {
					owned = true
				}
			}
		}
		if !owned {
			continue
		}
		path := filepath.Join(m.dir, name)
		if err := linked(path); err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		failed = errors.Join(failed, os.RemoveAll(path))
	}
	return failed
}

func removeOwnedGeneration(ctx context.Context, root, generation string, release Release) error {
	if !safePath(generation) || filepath.Base(generation) != generation {
		return ErrInvalid
	}
	if _, err := release.validate(); err != nil {
		return err
	}
	base := generationDir(root, generation)
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
		if err := noLinks(root, name); err != nil {
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

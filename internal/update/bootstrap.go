package update

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"lumen/internal/core"
)

// Bootstrap establishes installation identity after NSIS installs a full payload.
func Bootstrap(ctx context.Context, root string) error {
	if !filepath.IsAbs(root) {
		return ErrInvalid
	}
	lease, err := AcquireLease(ctx, root, true)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	if _, err := LoadActive(ctx, root); err != nil {
		return err
	}
	var install Installation
	if err := readJSON(filepath.Join(root, ".lumen-install.json"), &install); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return err
		}
		install = Installation{Format: 1, ID: hex.EncodeToString(id[:]), Root: root}
	}
	if install.Format != 1 || core.Fold(install.Root) != core.Fold(root) || len(install.ID) != 32 {
		return ErrInvalid
	}
	if err := writeJSON(ctx, filepath.Join(root, ".lumen-install.json"), install); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, ".lumen-installing")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// PrepareInstall closes launch admission before NSIS removes old files.
func PrepareInstall(ctx context.Context, root string) error {
	lease, err := AcquireLease(ctx, root, true)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	return writeJSON(ctx, filepath.Join(root, ".lumen-installing"), struct {
		Format int `json:"format"`
	}{1})
}

// RemoveGenerations removes inventoried files only, preserving unknown files.
func RemoveGenerations(ctx context.Context, root string) error {
	lease, err := AcquireLease(ctx, root, true)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	var install Installation
	if err := readJSON(filepath.Join(root, ".lumen-install.json"), &install); err != nil {
		return err
	}
	dir, err := StateDirectory(install)
	if err != nil {
		return err
	}
	var active Active
	if err := readJSON(filepath.Join(root, ".lumen-active.json"), &active); err != nil {
		return err
	}
	if active.Format != 1 || !safePath(active.Generation) || filepath.Base(active.Generation) != active.Generation {
		return ErrInvalid
	}
	if _, err := active.Release.validate(); err != nil {
		return err
	}
	releases := map[string]Release{active.Generation: active.Release}
	var a Activation
	if err := readJSON(filepath.Join(dir, "activation.json"), &a); err == nil && a.Install == install {
		releases[a.Previous.Generation] = a.Previous.Release
		releases[a.Candidate.Release.Version+"-"+a.Token] = a.Candidate.Release
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	records, err := (&Manager{dir: dir, install: install}).recoveries()
	if err != nil {
		return err
	}
	for _, record := range records {
		releases[record.activation.Previous.Generation] = record.activation.Previous.Release
	}
	for generation, release := range releases {
		if err := removeOwnedGeneration(ctx, root, generation, release); err != nil {
			return err
		}
	}
	return nil
}

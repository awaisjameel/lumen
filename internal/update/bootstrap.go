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
	err = readJSON(filepath.Join(root, installFile), &install)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// A record for another folder came with files copied or moved by hand:
	// this folder is a new installation, with updater state of its own.
	if err != nil || install.Format != 1 || len(install.ID) != 32 || core.Fold(filepath.Clean(install.Root)) != core.Fold(filepath.Clean(root)) {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return err
		}
		install = Installation{Format: 1, ID: hex.EncodeToString(id[:]), Root: root}
		if err := writeJSON(ctx, filepath.Join(root, installFile), install); err != nil {
			return err
		}
	}
	// Setup has just written a whole generation and selected it: no earlier
	// activation or installation still closes launches.
	for _, barrier := range []string{barrierFile, installingFile} {
		if err := os.Remove(filepath.Join(root, barrier)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
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
	return writeJSON(ctx, filepath.Join(root, installingFile), struct {
		Format int `json:"format"`
	}{1})
}

// RemoveGenerations removes inventoried files only, preserving unknown files,
// and the coordinator's own staging.
func RemoveGenerations(ctx context.Context, root string) error {
	lease, err := AcquireLease(ctx, root, true)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	install, dir, err := installed(root)
	if err != nil {
		return err
	}
	active, err := readActive(root)
	if err != nil {
		return err
	}
	if _, err := active.Release.validate(); err != nil {
		return err
	}
	releases := map[string]Release{active.Generation: active.Release}
	var a Activation
	if err := readJSON(filepath.Join(dir, activationFile), &a); err == nil && a.Install == install {
		releases[a.Previous.Generation] = a.Previous.Release
		releases[a.candidateGeneration()] = a.Candidate.Release
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	records, err := recoveries(dir, install)
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
	if err := removeStaging(root); err != nil {
		return err
	}
	// The folder itself goes only if nothing else was put there.
	_ = os.Remove(filepath.Join(root, ".update"))
	return nil
}

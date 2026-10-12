package update

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/theupdateframework/go-tuf/v2/metadata"

	"lumen/internal/core"
)

// Build trust is compiled into signed entry points, not loaded from writable files.
//
//go:embed release-config.json
var releaseConfig []byte

// BuildConfig is populated by release engineering before a publicly signed build.
type BuildConfig struct {
	Root          json.RawMessage `json:"root"`
	MetadataURL   string          `json:"metadataURL"`
	TargetsURL    string          `json:"targetsURL"`
	Origins       []string        `json:"origins"`
	PublisherPins []string        `json:"publisherPins"`
}

// ConfiguredBuild returns an inert configuration until all trust inputs exist.
func ConfiguredBuild() (BuildConfig, error) {
	var c BuildConfig
	if err := json.Unmarshal(releaseConfig, &c); err != nil {
		return c, err
	}
	if len(c.Root) == 0 || c.MetadataURL == "" || c.TargetsURL == "" || len(c.PublisherPins) == 0 {
		return c, ErrUnavailable
	}
	for _, pin := range c.PublisherPins {
		if !validHash(pin) {
			return c, ErrInvalid
		}
	}
	root, err := metadata.Root().FromBytes(c.Root)
	if err != nil {
		return c, ErrInvalid
	}
	if err := root.VerifyDelegate(metadata.ROOT, root); err != nil {
		return c, ErrInvalid
	}
	for _, role := range []string{metadata.ROOT, metadata.TARGETS} {
		if policy := root.Signed.Roles[role]; policy == nil || policy.Threshold < 2 {
			return c, ErrInvalid
		}
	}
	transport, err := NewTransport(c.Origins)
	if err != nil {
		return c, err
	}
	for _, address := range []string{c.MetadataURL, c.TargetsURL} {
		u, err := url.Parse(address)
		if err != nil || u.RawQuery != "" || transport.allowed(u) != nil {
			return c, ErrInvalid
		}
	}
	return c, nil
}

// Installation is NSIS-owned identity, independent of the selected generation.
type Installation struct {
	Format int    `json:"format"`
	ID     string `json:"id"`
	Root   string `json:"root"`
}

// DetectInstallation never guesses that a source checkout is an installation.
func DetectInstallation(executable string) (Installation, error) {
	root := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	var install Installation
	if err := readJSON(filepath.Join(root, ".lumen-install.json"), &install); err != nil {
		return install, ErrUnavailable
	}
	if install.Format != 1 || install.ID == "" || !filepath.IsAbs(install.Root) || core.Fold(filepath.Clean(root)) != core.Fold(filepath.Clean(install.Root)) {
		return install, ErrInvalid
	}
	rel, err := filepath.Rel(filepath.Join(root, "runtime"), executable)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.Base(executable) != appName {
		return install, ErrUnavailable
	}
	return install, nil
}

// StateDirectory binds the user's updater state to one installation identity.
func StateDirectory(install Installation) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(core.Fold(filepath.Clean(install.Root)) + "\x00" + install.ID))
	return filepath.Join(base, "LumenUpdater", hex.EncodeToString(h[:16])), nil
}

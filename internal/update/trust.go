package update

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	tuf "github.com/theupdateframework/go-tuf/v2/metadata/updater"

	"lumen/internal/core"
)

// Feed implements full TUF refresh with one durable accepted-metadata owner.
type Feed struct {
	mu          sync.Mutex // TUF instances and the accepted root must never race
	Root        []byte
	MetadataURL string
	TargetsURL  string
	StateDir    string
	Transport   *Transport
}

type trustState struct {
	Root      json.RawMessage `json:"root"`
	Timestamp json.RawMessage `json:"timestamp"`
	Snapshot  json.RawMessage `json:"snapshot"`
	Targets   json.RawMessage `json:"targets"`
}

// Check authenticates discovery only; runtime bytes are never fetched here.
func (f *Feed) Check(ctx context.Context, channel core.UpdateChannel, installed string, schema int) (*Candidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Root) == 0 || f.Transport == nil {
		return nil, ErrUnavailable
	}
	accepted := trustState{Root: f.Root}
	stateFile := filepath.Join(f.StateDir, "trust.json")
	if err := readJSON(stateFile, &accepted); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(accepted.Root) == 0 {
		return nil, ErrInvalid
	}
	work, err := os.MkdirTemp(f.StateDir, ".tuf-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	for role, data := range map[string]json.RawMessage{"timestamp": accepted.Timestamp, "snapshot": accepted.Snapshot, "targets": accepted.Targets} {
		if len(data) > 0 {
			if err := os.WriteFile(filepath.Join(work, role+".json"), data, 0o600); err != nil {
				return nil, err
			}
		}
	}
	cfg, err := config.New(f.MetadataURL, accepted.Root)
	if err != nil {
		return nil, err
	}
	cfg.LocalMetadataDir = work
	cfg.LocalTargetsDir = work
	cfg.RemoteTargetsURL = f.TargetsURL
	cfg.Fetcher = scopedFetcher{transport: f.Transport, ctx: ctx}
	cfg.MaxRootRotations = 32
	cfg.RootMaxLength = 512 << 10
	cfg.TimestampMaxLength = 16 << 10
	cfg.SnapshotMaxLength = 2 << 20
	cfg.TargetsMaxLength = 5 << 20
	client, err := tuf.New(cfg)
	if err != nil {
		return nil, err
	}
	refreshErr := client.Refresh()
	trusted := client.GetTrustedMetadataSet()
	// A valid rotated root or newer timestamp remains accepted even if a later
	// role fails. Publish them together, with flush semantics stronger than TUF's cache.
	if accepted.Root, err = trusted.Root.ToBytes(false); err != nil {
		return nil, err
	}
	if trusted.Timestamp != nil {
		if accepted.Timestamp, err = trusted.Timestamp.ToBytes(false); err != nil {
			return nil, err
		}
	}
	if trusted.Snapshot != nil {
		if accepted.Snapshot, err = trusted.Snapshot.ToBytes(false); err != nil {
			return nil, err
		}
	}
	if targets := trusted.Targets[metadata.TARGETS]; targets != nil {
		if accepted.Targets, err = targets.ToBytes(false); err != nil {
			return nil, err
		}
	}
	if err := writeJSON(context.WithoutCancel(ctx), stateFile, accepted); err != nil {
		return nil, err
	}
	if refreshErr != nil {
		return nil, refreshErr
	}
	var best *Candidate
	for name, target := range client.GetTopLevelTargets() {
		if !safePath(name) || target.Custom == nil || target.Length <= 0 || target.Length > maxPayload {
			continue
		}
		var release Release
		if err := json.Unmarshal(*target.Custom, &release); err != nil {
			continue
		}
		if _, err := release.validate(); err != nil || release.Channel != channel || !newer(release.Version, installed) || release.MinWindows > windowsBuild() || schema < release.ReadMin || schema > release.ReadMax {
			continue
		}
		digest := hex.EncodeToString(target.Hashes["sha256"])
		if !validHash(digest) {
			continue
		}
		if name != product+"/"+string(channel)+"/windows-amd64/"+release.Version+"/runtime.zip" {
			continue
		}
		remote := name
		if trusted.Root.Signed.ConsistentSnapshot {
			remote = path.Join(path.Dir(name), digest+"."+path.Base(name))
		}
		if best == nil || newer(release.Version, best.Release.Version) {
			address := f.TargetsURL + "/" + remote
			if release.ArtifactURL != "" {
				address = release.ArtifactURL
			}
			u, err := url.Parse(address)
			if err != nil || f.Transport.allowed(u) != nil {
				continue
			}
			best = &Candidate{Release: release, Target: name, Length: target.Length, SHA256: digest, URL: address}
		}
	}
	return best, nil
}

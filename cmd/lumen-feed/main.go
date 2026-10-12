// The release tool prepares authenticated metadata locally. Publication is separate.
package main

import (
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"

	"lumen/internal/update"
)

type descriptor struct {
	Target string         `json:"target"`
	Length int64          `json:"length"`
	SHA256 string         `json:"sha256"`
	Custom update.Release `json:"custom"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	rootFile := flag.String("root", "", "reviewed signed public TUF root")
	descriptorFile := flag.String("target", "", "generated update-target.json")
	payload := flag.String("payload", "", "complete signed runtime archive")
	out := flag.String("out", "", "new empty local feed directory")
	targetKeys := flag.String("targets-keys", "", "comma-separated external PEM private keys for targets quorum")
	snapshotKey := flag.String("snapshot-key", "", "external PEM snapshot private key")
	timestampKey := flag.String("timestamp-key", "", "external PEM timestamp private key")
	metaVersion := flag.Int64("metadata-version", 0, "strictly increasing targets/snapshot/timestamp version")
	previousTargets := flag.String("previous-targets", "", "previous signed targets to preserve other channels")
	withdraw := flag.String("withdraw-target", "", "exact authenticated target to remove")
	refresh := flag.Bool("refresh", false, "refresh snapshot/timestamp only, preserving signed targets")
	flag.Parse()
	if *refresh && *withdraw != "" {
		return errors.New("refresh cannot change authenticated targets")
	}
	if *rootFile == "" || *out == "" || *snapshotKey == "" || *timestampKey == "" || *metaVersion < 1 || (!*refresh && *targetKeys == "") {
		return errors.New("all trust, payload and signing arguments are required")
	}
	if (*refresh || *withdraw != "") && *previousTargets == "" {
		return errors.New("refresh and withdrawal require previous authenticated targets")
	}
	if !*refresh && *withdraw == "" && (*descriptorFile == "" || *payload == "") {
		return errors.New("target and payload are required for a new release")
	}
	rootBytes, err := os.ReadFile(*rootFile)
	if err != nil {
		return err
	}
	root, err := metadata.Root().FromBytes(rootBytes)
	if err != nil {
		return err
	}
	if err := root.VerifyDelegate(metadata.ROOT, root); err != nil {
		return err
	}
	if root.Signed.IsExpired(time.Now().UTC()) {
		return errors.New("release root has expired")
	}
	if role := root.Signed.Roles[metadata.ROOT]; role == nil || role.Threshold < 2 {
		return errors.New("public release root must require an offline multi-key quorum")
	}
	now := time.Now().UTC()
	targets := metadata.Targets(now.Add(30 * 24 * time.Hour))
	var previousBytes []byte
	if *previousTargets != "" {
		previousBytes, err = os.ReadFile(*previousTargets)
		if err != nil {
			return err
		}
		old, err := metadata.Targets().FromBytes(previousBytes)
		if err != nil {
			return err
		}
		if err := root.VerifyDelegate(metadata.TARGETS, old); err != nil {
			return err
		}
		if *metaVersion <= old.Signed.Version {
			return errors.New("metadata version must advance")
		}
		if *refresh {
			if old.Signed.IsExpired(now) {
				return errors.New("targets renewal requires the offline targets quorum")
			}
			targets = old
		} else {
			targets.Signed.Targets = old.Signed.Targets
		}
	}
	if !*refresh {
		targets.Signed.Version = *metaVersion
	}
	var d descriptor
	if !*refresh && *withdraw == "" {
		data, err := os.ReadFile(*descriptorFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &d); err != nil {
			return err
		}
		f, err := os.Open(*payload)
		if err != nil {
			return err
		}
		h := sha256.New()
		size, err := io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if size != d.Length || hex.EncodeToString(h.Sum(nil)) != d.SHA256 {
			return errors.New("runtime differs from target descriptor")
		}
		if err := update.ValidateRelease(d.Custom); err != nil {
			return err
		}
		expected := "io.lumen.app/" + string(d.Custom.Channel) + "/windows-amd64/" + d.Custom.Version + "/runtime.zip"
		if d.Target != expected {
			return errors.New("target identity differs from release")
		}
		var custom json.RawMessage
		if custom, err = json.Marshal(d.Custom); err != nil {
			return err
		}
		digest, err := hex.DecodeString(d.SHA256)
		if err != nil {
			return err
		}
		if old, exists := targets.Signed.Targets[d.Target]; exists && (old.Length != d.Length || hex.EncodeToString(old.Hashes["sha256"]) != d.SHA256) {
			return errors.New("an immutable release target cannot change bytes")
		}
		targets.Signed.Targets[d.Target] = &metadata.TargetFiles{Length: d.Length, Hashes: metadata.Hashes{"sha256": digest}, Custom: &custom}
	}
	if *withdraw != "" {
		if _, ok := targets.Signed.Targets[*withdraw]; !ok {
			return errors.New("withdrawal target is not in authenticated targets")
		}
		delete(targets.Signed.Targets, *withdraw)
	}
	targetBytes := previousBytes
	if !*refresh {
		targetBytes, err = sign(targets, strings.Split(*targetKeys, ","))
	}
	if err != nil {
		return err
	}
	if err := root.VerifyDelegate(metadata.TARGETS, targets); err != nil {
		return err
	}
	snapshot := metadata.Snapshot(now.Add(7 * 24 * time.Hour))
	snapshot.Signed.Version = *metaVersion
	snapshot.Signed.Meta["targets.json"] = info(targets.Signed.Version, targetBytes)
	snapshotBytes, err := sign(snapshot, []string{*snapshotKey})
	if err != nil {
		return err
	}
	if err := root.VerifyDelegate(metadata.SNAPSHOT, snapshot); err != nil {
		return err
	}
	timestamp := metadata.Timestamp(now.Add(24 * time.Hour))
	timestamp.Signed.Version = *metaVersion
	timestamp.Signed.Meta["snapshot.json"] = info(*metaVersion, snapshotBytes)
	timestampBytes, err := sign(timestamp, []string{*timestampKey})
	if err != nil {
		return err
	}
	if err := root.VerifyDelegate(metadata.TIMESTAMP, timestamp); err != nil {
		return err
	}
	// Create refuses an existing feed. A reviewed publisher may upload immutable
	// payloads first and timestamp last; this tool performs no network operation.
	if err := os.Mkdir(*out, 0o700); err != nil {
		return err
	}
	for name, bytes := range map[string][]byte{"root.json": rootBytes, strconv.FormatInt(root.Signed.Version, 10) + ".root.json": rootBytes, "timestamp.json": timestampBytes, "snapshot.json": snapshotBytes, "targets.json": targetBytes} {
		if err := os.WriteFile(filepath.Join(*out, name), bytes, 0o600); err != nil {
			return err
		}
	}
	if root.Signed.ConsistentSnapshot {
		for name, bytes := range map[string][]byte{strconv.FormatInt(*metaVersion, 10) + ".snapshot.json": snapshotBytes, strconv.FormatInt(targets.Signed.Version, 10) + ".targets.json": targetBytes} {
			if err := os.WriteFile(filepath.Join(*out, name), bytes, 0o600); err != nil {
				return err
			}
		}
	}
	if *refresh || *withdraw != "" {
		return nil
	}
	remote := d.Target
	if root.Signed.ConsistentSnapshot {
		remote = filepath.ToSlash(filepath.Join(filepath.Dir(remote), d.SHA256+"."+filepath.Base(remote)))
	}
	destination := filepath.Join(*out, "targets", filepath.FromSlash(remote))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	return copyPayload(context.Background(), *payload, destination)
}

func info(version int64, data []byte) *metadata.MetaFiles {
	h := sha256.Sum256(data)
	return &metadata.MetaFiles{Version: version, Length: int64(len(data)), Hashes: metadata.Hashes{"sha256": h[:]}}
}

func sign[T metadata.Roles](m *metadata.Metadata[T], keys []string) ([]byte, error) {
	for _, key := range keys {
		data, err := os.ReadFile(key)
		if err != nil {
			return nil, err
		}
		private, err := cryptoutils.UnmarshalPEMToPrivateKey(data, nil)
		if err != nil {
			return nil, err
		}
		signer, err := signature.LoadSigner(private, crypto.SHA256)
		if err != nil {
			return nil, err
		}
		if _, err := m.Sign(signer); err != nil {
			return nil, err
		}
	}
	return m.ToBytes(false)
}

func copyPayload(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Sync()
}

package update

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	tuf "github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

const targetPath = "io.lumen.app/stable/windows-amd64/0.12.0/runtime.zip"

// fixtureFeed has no transport: every request is answered from test-owned bytes.
type fixtureFeed struct {
	files    map[string][]byte
	requests []string
}

func (f *fixtureFeed) DownloadFile(address string, limit int64, _ time.Duration) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	name := strings.TrimPrefix(u.Path, "/metadata/")
	f.requests = append(f.requests, name)
	data, ok := f.files[name]
	if !ok {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: 404, URL: address}
	}
	if int64(len(data)) > limit {
		return nil, &metadata.ErrDownloadLengthMismatch{Msg: "fixture exceeds the requested bound"}
	}
	return bytes.Clone(data), nil
}

type repository struct {
	root      *metadata.Metadata[metadata.RootType]
	timestamp *metadata.Metadata[metadata.TimestampType]
	snapshot  *metadata.Metadata[metadata.SnapshotType]
	targets   *metadata.Metadata[metadata.TargetsType]
	signers   map[string]signature.Signer
	payload   []byte
	feed      *fixtureFeed
}

func newRepository(t *testing.T) *repository {
	t.Helper()
	expires := time.Now().UTC().Add(24 * time.Hour)
	r := &repository{
		root: metadata.Root(expires), timestamp: metadata.Timestamp(expires),
		snapshot: metadata.Snapshot(expires), targets: metadata.Targets(expires),
		signers: make(map[string]signature.Signer), payload: []byte("whole-runtime fixture"),
		feed: &fixtureFeed{files: make(map[string][]byte)},
	}
	r.root.Signed.ConsistentSnapshot = false
	for _, role := range []string{metadata.ROOT, metadata.TIMESTAMP, metadata.SNAPSHOT, metadata.TARGETS} {
		public, private, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		key, err := metadata.KeyFromPublicKey(public)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.root.Signed.AddKey(key, role); err != nil {
			t.Fatal(err)
		}
		signer, err := signature.LoadSigner(private, crypto.Hash(0))
		if err != nil {
			t.Fatal(err)
		}
		r.signers[role] = signer
	}
	info, err := metadata.TargetFile().FromBytes(targetPath, r.payload, "sha256")
	if err != nil {
		t.Fatal(err)
	}
	custom := json.RawMessage(`{"product":"io.lumen.app","channel":"beta","platform":"windows","arch":"amd64","version":"0.12.0","schema":{"readMin":31,"readMax":31,"write":31}}`)
	info.Custom = &custom
	r.targets.Signed.Targets[targetPath] = info
	r.publish(t)
	return r
}

func signedBytes[T metadata.Roles](t *testing.T, value *metadata.Metadata[T], signer signature.Signer) []byte {
	t.Helper()
	value.ClearSignatures()
	if _, err := value.Sign(signer); err != nil {
		t.Fatal(err)
	}
	data, err := value.ToBytes(false)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func metaInfo(version int64, data []byte) *metadata.MetaFiles {
	digest := sha256.Sum256(data)
	return &metadata.MetaFiles{Version: version, Length: int64(len(data)), Hashes: metadata.Hashes{"sha256": digest[:]}}
}

func (r *repository) publish(t *testing.T) {
	t.Helper()
	r.feed.files["root.json"] = signedBytes(t, r.root, r.signers[metadata.ROOT])
	targets := signedBytes(t, r.targets, r.signers[metadata.TARGETS])
	r.snapshot.Signed.Meta["targets.json"] = metaInfo(r.targets.Signed.Version, targets)
	snapshot := signedBytes(t, r.snapshot, r.signers[metadata.SNAPSHOT])
	r.timestamp.Signed.Meta["snapshot.json"] = metaInfo(r.snapshot.Signed.Version, snapshot)
	r.feed.files["timestamp.json"] = signedBytes(t, r.timestamp, r.signers[metadata.TIMESTAMP])
	r.feed.files["snapshot.json"] = snapshot
	r.feed.files["targets.json"] = targets
	r.feed.files["targets/"+targetPath] = bytes.Clone(r.payload)
}

func client(t *testing.T, r *repository, cache string) *tuf.Updater {
	t.Helper()
	cfg, err := config.New("https://fixture.invalid/metadata/", r.feed.files["root.json"])
	if err != nil {
		t.Fatal(err)
	}
	cfg.Fetcher = r.feed
	cfg.LocalMetadataDir = filepath.Join(cache, "metadata")
	cfg.LocalTargetsDir = filepath.Join(cache, "targets")
	cfg.TimestampMaxLength = 16 * 1024
	cfg.SnapshotMaxLength = 128 * 1024
	cfg.TargetsMaxLength = 128 * 1024
	c, err := tuf.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDiscoveryAuthenticatesMetadataWithoutDownloadingTheRuntime(t *testing.T) {
	r := newRepository(t)
	c := client(t, r, t.TempDir())
	if err := c.Refresh(); err != nil {
		t.Fatal(err)
	}
	info, err := c.GetTargetInfo(targetPath)
	if err != nil || info.Length != int64(len(r.payload)) {
		t.Fatalf("target: %v, %v", info, err)
	}
	for _, request := range r.feed.requests {
		if strings.HasPrefix(request, "targets/") {
			t.Fatalf("discovery fetched runtime: %s", request)
		}
	}
	if _, err := c.GetTargetInfo("another-product/beta/windows-amd64/runtime.zip"); err == nil {
		t.Fatal("a different product's target was found")
	}
}

func TestRequestedPayloadMustMatchItsAuthenticatedLengthAndHash(t *testing.T) {
	for _, change := range []string{"unchanged", "same-length alteration", "truncation"} {
		t.Run(change, func(t *testing.T) {
			r := newRepository(t)
			c := client(t, r, t.TempDir())
			info, err := c.GetTargetInfo(targetPath)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "same-length alteration":
				r.feed.files["targets/"+targetPath][0] ^= 1
			case "truncation":
				r.feed.files["targets/"+targetPath] = r.payload[:len(r.payload)-1]
			}
			_, _, err = c.DownloadTarget(info, "", "")
			if (err == nil) != (change == "unchanged") {
				t.Fatalf("download result: %v", err)
			}
		})
	}
}

func TestInvalidMetadataFailsBeforeAnyRuntimeRequest(t *testing.T) {
	for _, change := range []string{"unsigned timestamp", "wrong timestamp signer", "expired timestamp", "expired snapshot", "expired targets", "expired root", "altered release identity", "missing snapshot", "oversized timestamp", "malformed timestamp"} {
		t.Run(change, func(t *testing.T) {
			r := newRepository(t)
			switch change {
			case "unsigned timestamp":
				r.timestamp.ClearSignatures()
				data, err := r.timestamp.ToBytes(false)
				if err != nil {
					t.Fatal(err)
				}
				r.feed.files["timestamp.json"] = data
			case "wrong timestamp signer":
				r.feed.files["timestamp.json"] = signedBytes(t, r.timestamp, r.signers[metadata.TARGETS])
			case "expired timestamp":
				r.timestamp.Signed.Expires = time.Now().UTC().Add(-time.Hour)
				r.publish(t)
			case "expired snapshot":
				r.snapshot.Signed.Expires = time.Now().UTC().Add(-time.Hour)
				r.publish(t)
			case "expired targets":
				r.targets.Signed.Expires = time.Now().UTC().Add(-time.Hour)
				r.publish(t)
			case "expired root":
				r.root.Signed.Expires = time.Now().UTC().Add(-time.Hour)
				r.publish(t)
			case "altered release identity":
				r.feed.files["targets.json"] = bytes.ReplaceAll(r.feed.files["targets.json"], []byte(`"arch":"amd64"`), []byte(`"arch":"arm64"`))
				// Even holders of the online timestamp/snapshot keys cannot alter
				// release identity without the authorised targets signature.
				r.snapshot.Signed.Meta["targets.json"] = metaInfo(r.targets.Signed.Version, r.feed.files["targets.json"])
				r.feed.files["snapshot.json"] = signedBytes(t, r.snapshot, r.signers[metadata.SNAPSHOT])
				r.timestamp.Signed.Meta["snapshot.json"] = metaInfo(r.snapshot.Signed.Version, r.feed.files["snapshot.json"])
				r.feed.files["timestamp.json"] = signedBytes(t, r.timestamp, r.signers[metadata.TIMESTAMP])
			case "missing snapshot":
				delete(r.feed.files, "snapshot.json")
			case "oversized timestamp":
				r.feed.files["timestamp.json"] = bytes.Repeat([]byte(" "), 16*1024+1)
			case "malformed timestamp":
				r.feed.files["timestamp.json"] = []byte("{")
			}
			c := client(t, r, t.TempDir())
			if err := c.Refresh(); err == nil {
				t.Fatal("invalid metadata was accepted")
			}
			for _, request := range r.feed.requests {
				if strings.HasPrefix(request, "targets/") {
					t.Fatalf("invalid feed caused runtime fetch: %s", request)
				}
			}
		})
	}
}

func TestRootRotationRequiresBothTheOldAndNewAuthority(t *testing.T) {
	for _, authorised := range []bool{false, true} {
		t.Run(map[bool]string{false: "self-signed substitution", true: "authorised rotation"}[authorised], func(t *testing.T) {
			r := newRepository(t)
			rotated, err := metadata.Root().FromBytes(r.feed.files["root.json"])
			if err != nil {
				t.Fatal(err)
			}
			public, private, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			key, err := metadata.KeyFromPublicKey(public)
			if err != nil {
				t.Fatal(err)
			}
			rotated.Signed.Version = 2
			rotated.Signed.Roles[metadata.ROOT].KeyIDs = nil
			if err := rotated.Signed.AddKey(key, metadata.ROOT); err != nil {
				t.Fatal(err)
			}
			signer, err := signature.LoadSigner(private, crypto.Hash(0))
			if err != nil {
				t.Fatal(err)
			}
			rotated.ClearSignatures()
			if _, err := rotated.Sign(signer); err != nil {
				t.Fatal(err)
			}
			if authorised {
				if _, err := rotated.Sign(r.signers[metadata.ROOT]); err != nil {
					t.Fatal(err)
				}
			}
			data, err := rotated.ToBytes(false)
			if err != nil {
				t.Fatal(err)
			}
			r.feed.files["2.root.json"] = data
			err = client(t, r, t.TempDir()).Refresh()
			if (err == nil) != authorised {
				t.Fatalf("rotation: %v", err)
			}
		})
	}
}

func TestPersistedMetadataRejectsAnOlderTimestampAfterRestart(t *testing.T) {
	r := newRepository(t)
	oldTimestamp := bytes.Clone(r.feed.files["timestamp.json"])
	r.timestamp.Signed.Version = 2
	r.publish(t)
	cache := t.TempDir()
	if err := client(t, r, cache).Refresh(); err != nil {
		t.Fatal(err)
	}
	r.feed.files["timestamp.json"] = oldTimestamp
	err := client(t, r, cache).Refresh()
	var versionError *metadata.ErrBadVersionNumber
	if !errors.As(err, &versionError) {
		t.Fatalf("expected replay refusal, got %v", err)
	}
}

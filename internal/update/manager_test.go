package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"

	"lumen/internal/core"
)

func releaseFixture(t *testing.T) (release Release, payload []byte) {
	t.Helper()
	r := Release{Product: product, Channel: core.UpdateStable, Platform: "windows", Arch: "amd64", Version: "0.12.0", MinUpdater: 1, MinWindows: 10240, ReadMin: 31, ReadMax: 31, WriteSchema: 31}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, data := range map[string]string{"lumen-app.exe": "fixture application", "THIRD_PARTY_NOTICES.txt": "fixture licence", "models/model.bin": "fixture model"} {
		digest := sha256.Sum256([]byte(data))
		r.Files = append(r.Files, File{Path: name, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])})
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return r, buffer.Bytes()
}

func TestGenerationStagingRejectsUnexpectedAndChangedFiles(t *testing.T) {
	r, data := releaseFixture(t)
	root := t.TempDir()
	archive := filepath.Join(root, "runtime.zip")
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "generation")
	if err := Stage(t.Context(), archive, destination, r); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "extra.dll"), []byte("untrusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(VerifyGeneration(t.Context(), destination, r), ErrInvalid) {
		t.Fatal("unexpected file accepted")
	}
	if err := os.Remove(filepath.Join(destination, "extra.dll")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "lumen-app.exe"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(VerifyGeneration(t.Context(), destination, r), ErrInvalid) {
		t.Fatal("altered application accepted")
	}
}

func TestInventoryRefusesWindowsAliasesTraversalAndFileParents(t *testing.T) {
	for _, name := range []string{"../outside", "/absolute", "C:/absolute", "models\\file", "models/file:stream", "CON", "aux.txt", "models/NUL", "COM1.jpg", "LPT9", "file.", "folder /file", "file\nname"} {
		t.Run(name, func(t *testing.T) {
			if safePath(name) {
				t.Fatalf("unsafe path accepted: %q", name)
			}
		})
	}
	r, _ := releaseFixture(t)
	r.Files = append(r.Files, File{Path: "LUMEN-APP.exe", Size: 0, SHA256: r.Files[0].SHA256})
	if _, err := r.validate(); !errors.Is(err, ErrInvalid) {
		t.Fatal("case alias accepted")
	}
}

func TestTransportRejectsUnsafeOriginsAndRedirects(t *testing.T) {
	for _, origin := range []string{"http://release.invalid", "https://user:password@release.invalid", "https://release.invalid/path", "https://release.invalid?token=secret"} {
		if _, err := NewTransport([]string{origin}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsafe origin %q: %v", origin, err)
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://another.invalid/runtime.zip", http.StatusFound)
	}))
	defer server.Close()
	transport, err := NewTransport([]string{server.URL})
	if err != nil {
		t.Fatal(err)
	}
	transport.client.Transport = server.Client().Transport
	res, err := transport.get(t.Context(), server.URL)
	if res != nil {
		_ = res.Body.Close()
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unapproved redirect: %v", err)
	}
}

func TestProductionFeedDiscoversWithoutRuntimeRequestsAndRejectsReplay(t *testing.T) {
	r := newRepository(t)
	release, payload := releaseFixture(t)
	custom, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	r.targets.Signed.Targets[targetPath].Custom = (*json.RawMessage)(&custom)
	r.payload = payload
	r.publish(t)
	oldTimestamp := bytes.Clone(r.feed.files["timestamp.json"])
	r.timestamp.Signed.Version = 2
	r.publish(t)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		name := request.URL.Path[1:]
		if request.Method != http.MethodGet || request.URL.RawQuery != "" || request.Header.Get("User-Agent") != "LumenUpdater/1" {
			t.Error("request contains unexpected context")
		}
		if strings.Contains(name, "runtime.zip") {
			t.Error("discovery downloaded runtime")
		}
		data, ok := r.feed.files[name]
		if !ok {
			http.NotFound(w, request)
			return
		}
		if _, err := w.Write(data); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	transport, err := NewTransport([]string{server.URL})
	if err != nil {
		t.Fatal(err)
	}
	transport.client.Transport = server.Client().Transport
	dir := t.TempDir()
	f := &Feed{Root: r.feed.files["root.json"], MetadataURL: server.URL, TargetsURL: server.URL + "/targets", StateDir: dir, Transport: transport}
	c, err := f.Check(t.Context(), core.UpdateStable, "0.11.0", 31)
	if err != nil || c == nil || c.Release.Version != "0.12.0" {
		t.Fatalf("discovery: %+v %v", c, err)
	}
	if requests.Load() == 0 {
		t.Fatal("feed was not exercised")
	}
	r.feed.files["timestamp.json"] = oldTimestamp
	f = &Feed{Root: f.Root, MetadataURL: server.URL, TargetsURL: server.URL + "/targets", StateDir: dir, Transport: transport}
	if _, err := f.Check(t.Context(), core.UpdateStable, "0.11.0", 31); err == nil {
		t.Fatal("persisted timestamp replay accepted")
	}
}

type blockedSource struct {
	entered chan struct{}
	calls   atomic.Int32
}

func (s *blockedSource) Check(ctx context.Context, _ core.UpdateChannel, _ string, _ int) (*Candidate, error) {
	s.calls.Add(1)
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func managerFixture(t *testing.T) *Manager {
	t.Helper()
	ctx, stop := context.WithCancel(t.Context())
	root := t.TempDir()
	dir := t.TempDir()
	lease, err := AcquireLease(t.Context(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{ctx: ctx, stop: stop, owner: lease, dir: dir, install: Installation{Format: 1, ID: "fixture", Root: root}, schema: 31, verify: VerifyPublisher, state: journal{Format: 1, Status: core.UpdateStatus{Configured: true, Managed: true, InstalledVersion: "0.11.0", Phase: core.UpdateIdle, Policy: core.UpdatePolicy{Channel: core.UpdateStable}}}}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return m
}

func TestDiscoveryIsSingleFlightAndCancelDoesNotDownload(t *testing.T) {
	m := managerFixture(t)
	s := &blockedSource{entered: make(chan struct{}, 1)}
	m.source = s
	done := make(chan error, 1)
	go func() { done <- m.Check(t.Context()) }()
	<-s.entered
	if !errors.Is(m.Check(t.Context()), ErrBusy) {
		t.Fatal("overlapping discovery admitted")
	}
	if err := m.Cancel(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if m.Status().Phase != core.UpdateCancelled || s.calls.Load() != 1 {
		t.Fatal("cancelled discovery changed state incorrectly")
	}
}

func TestOfflinePolicyDoesNotDiscoverOnStartup(t *testing.T) {
	m := managerFixture(t)
	s := &blockedSource{entered: make(chan struct{}, 1)}
	m.source = s
	m.Start()
	select {
	case <-s.entered:
		t.Fatal("disabled discovery contacted feed")
	case <-time.After(1200 * time.Millisecond):
	}
}

func TestLeaseAllowsLibrariesTogetherAndBlocksActivation(t *testing.T) {
	root := t.TempDir()
	a, err := AcquireLease(t.Context(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	b, err := AcquireLease(t.Context(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := AcquireLease(ctx, root, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("activation admitted while libraries open: %v", err)
	}
}

func TestPolicySavesStayOrderedAndPortableSettingsCannotEnableUpdates(t *testing.T) {
	m := managerFixture(t)
	var wg sync.WaitGroup
	for _, channel := range []core.UpdateChannel{core.UpdateStable, core.UpdatePreview} {
		wg.Go(func() {
			if err := m.SetPolicy(t.Context(), core.UpdatePolicy{Channel: channel}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var saved journal
	if err := readJSON(filepath.Join(m.dir, "journal.json"), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Status.Policy != m.Status().Policy {
		t.Fatal("policy persistence drift")
	}
	if saved.Status.Policy.Automatic {
		t.Fatal("automatic discovery became enabled")
	}
}

func TestRecoveryRetentionKeepsYoungGenerationsAndUnrelatedFiles(t *testing.T) {
	m := managerFixture(t)
	release, data := releaseFixture(t)
	root := filepath.Join(m.install.Root, "runtime")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(m.dir, "runtime.zip")
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	old := release
	old.Version = "0.11.0"
	if err := Stage(t.Context(), archive, filepath.Join(root, "0.11.0"), old); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "0.11.0", "unrelated.txt")
	if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(t.Context(), filepath.Join(m.install.Root, ".lumen-active.json"), Active{Format: 1, Generation: "0.12.0", Version: "0.12.0", Release: release}); err != nil {
		t.Fatal(err)
	}
	oldToken := "00000000000000000000000000000001"
	youngToken := "00000000000000000000000000000002"
	aged := Activation{Format: 1, Install: m.install, Token: oldToken, Previous: Active{Generation: "0.11.0", Release: old}, Phase: core.UpdateComplete, CompletedAt: time.Now().Add(-8 * 24 * time.Hour).Unix(), Backup: filepath.Join(m.dir, "library-"+oldToken+".db")}
	young := aged
	young.Token = youngToken
	young.CompletedAt = time.Now().Add(-time.Hour).Unix()
	young.Backup = filepath.Join(m.dir, "library-"+youngToken+".db")
	for _, a := range []Activation{aged, young} {
		if err := os.WriteFile(a.Backup, []byte("fixture backup"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeJSON(t.Context(), filepath.Join(m.dir, "recoveries", oldToken+".json"), aged); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(t.Context(), filepath.Join(m.dir, "activation.json"), young); err != nil {
		t.Fatal(err)
	}
	if err := m.PruneRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "0.11.0", "lumen-app.exe")); err != nil {
		t.Fatal("young recovery runtime removed", err)
	}
	if _, err := os.Stat(aged.Backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("aged backup retained", err)
	}
	young.CompletedAt = time.Now().Add(-8 * 24 * time.Hour).Unix()
	if err := writeJSON(t.Context(), filepath.Join(m.dir, "activation.json"), young); err != nil {
		t.Fatal(err)
	}
	if err := m.PruneRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("unrelated file removed", err)
	}
	if _, err := os.Stat(filepath.Join(root, "0.11.0", "lumen-app.exe")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("aged runtime retained", err)
	}
}

func TestAcceptedRootSurvivesRestartAndRefusesTheRevokedAuthority(t *testing.T) {
	r := newRepository(t)
	seed := bytes.Clone(r.feed.files["root.json"])
	rotated, err := metadata.Root().FromBytes(seed)
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
	for _, authority := range []signature.Signer{r.signers[metadata.ROOT], signer} {
		if _, err := rotated.Sign(authority); err != nil {
			t.Fatal(err)
		}
	}
	r.feed.files["2.root.json"], err = rotated.ToBytes(false)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		data, ok := r.feed.files[request.URL.Path[1:]]
		if !ok {
			http.NotFound(w, request)
			return
		}
		if _, err := w.Write(data); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	transport, err := NewTransport([]string{server.URL})
	if err != nil {
		t.Fatal(err)
	}
	transport.client.Transport = server.Client().Transport
	dir := t.TempDir()
	feed := &Feed{Root: seed, MetadataURL: server.URL, TargetsURL: server.URL + "/targets", StateDir: dir, Transport: transport}
	if _, err := feed.Check(t.Context(), core.UpdateStable, "0.11.0", 31); err != nil {
		t.Fatal(err)
	}
	var saved trustState
	if err := readJSON(filepath.Join(dir, "trust.json"), &saved); err != nil {
		t.Fatal(err)
	}
	accepted, err := metadata.Root().FromBytes(saved.Root)
	if err != nil || accepted.Signed.Version != 2 {
		t.Fatal("rotated root not durable", err)
	}
	rotated.Signed.Version = 3
	rotated.ClearSignatures()
	if _, err := rotated.Sign(r.signers[metadata.ROOT]); err != nil {
		t.Fatal(err)
	}
	r.feed.files["3.root.json"], err = rotated.ToBytes(false)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Feed{Root: seed, MetadataURL: server.URL, TargetsURL: server.URL + "/targets", StateDir: dir, Transport: transport}
	if _, err := restarted.Check(t.Context(), core.UpdateStable, "0.11.0", 31); err == nil {
		t.Fatal("revoked original root authority accepted after restart")
	}
}

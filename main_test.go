package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/diag"
	"lumen/internal/store"
	"lumen/services"
)

// The log starts over once it passes its size, while running too, keeping
// one old log.
func TestLogRotatesWhileRunning(t *testing.T) {
	dir := t.TempDir()
	name, old := filepath.Join(dir, "lumen.log"), filepath.Join(dir, "lumen.old.log")
	log, err := openLog(name, old, 100)
	if err != nil {
		t.Fatal(err)
	}
	line := bytes.Repeat([]byte("x"), 60)
	for range 3 {
		if _, err := log.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.f.Close(); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]int{name: 60, old: 60} {
		fi, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Size() != int64(want) {
			t.Errorf("%s: %d bytes, want %d", filepath.Base(file), fi.Size(), want)
		}
	}
}

type heldCacheSweep struct {
	started, release chan struct{}
	tempSwept        bool
}

func (c *heldCacheSweep) SweepTemp() error { c.tempSwept = true; return nil }

func (c *heldCacheSweep) SweepFrames(context.Context) error {
	close(c.started)
	<-c.release
	return nil
}

type stoppedIndexer struct {
	ctx       context.Context
	cache     *heldCacheSweep
	tempSwept bool
}

func (i *stoppedIndexer) Start(ctx context.Context) {
	i.ctx, i.tempSwept = ctx, i.cache.tempSwept
}
func (*stoppedIndexer) Stop() {}

func TestBackgroundShutdownWaitsForFrameCleanup(t *testing.T) {
	c := &heldCacheSweep{started: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(c.release) }) })
	i := &stoppedIndexer{cache: c}
	b := &background{ix: i, cache: c}
	if err := b.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	if !i.tempSwept {
		t.Fatal("indexing started before temporary cache cleanup")
	}
	select {
	case <-c.started:
	case <-time.After(5 * time.Second):
		t.Fatal("frame cleanup did not start")
	}
	done := make(chan struct{})
	go func() { _ = b.ServiceShutdown(); close(done) }()
	select {
	case <-i.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel background work")
	}
	select {
	case <-done:
		t.Fatal("shutdown returned while frame cleanup was still running")
	case <-time.After(20 * time.Millisecond):
	}
	release.Do(func() { close(c.release) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish after frame cleanup")
	}
}

func TestLogTruncatesWhenRotationFails(t *testing.T) {
	for _, startup := range []bool{true, false} {
		t.Run(fmt.Sprint("startup=", startup), func(t *testing.T) {
			dir := t.TempDir()
			name, old := filepath.Join(dir, "lumen.log"), filepath.Join(dir, "lumen.old.log")
			// A non-empty directory makes renaming fail on every platform.
			if err := os.Mkdir(old, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(old, "held"), []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			const limit = 1024
			if startup {
				if err := os.WriteFile(name, bytes.Repeat([]byte("x"), limit+1), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			log, err := openLog(name, old, limit)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = log.f.Close() })
			line := bytes.Repeat([]byte("y"), 800)
			if !startup {
				if _, err := log.Write(bytes.Repeat([]byte("x"), 800)); err != nil {
					t.Fatal(err)
				}
				if _, err := log.Write(line); err != nil {
					t.Fatal(err)
				}
			}
			for range 3 {
				data, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte("xxx")) || bytes.Count(data, []byte("level=WARN")) != 1 ||
					!bytes.Contains(data, []byte("truncated active log")) {
					t.Fatalf("failed rotation did not truncate and warn once: %q", data)
				}
				if int64(len(data)) != log.size || len(data) > limit+len(line) {
					t.Fatalf("log grew after failed rotation: %d bytes, tracked %d", len(data), log.size)
				}
				if _, err := log.Write(line); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// A scratch library (LUMEN_DATA_DIR) runs beside the user's Lumen: one
// single-instance ID per library, the same whatever the path's case.
func TestInstanceIDPerLibrary(t *testing.T) {
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	defaultDir := filepath.Join(base, "Lumen")
	t.Setenv("LUMEN_DATA_DIR", "")
	if got := instanceID(core.PathsIn(defaultDir)); got != "io.lumen.app" {
		t.Errorf("default library: %q", got)
	}
	t.Setenv("LUMEN_DATA_DIR", `D:\scratch`)
	a, b := instanceID(core.PathsIn(`D:\scratch`)), instanceID(core.PathsIn(`d:\SCRATCH`))
	if a == "io.lumen.app" || a != b {
		t.Errorf("scratch library IDs %q and %q", a, b)
	}
	if got := webviewDataPath(core.PathsIn(`D:\scratch`)); got != `D:\scratch\webview` {
		t.Errorf("scratch WebView data in %q", got)
	}
	t.Setenv("LUMEN_DATA_DIR", "")
	if got := webviewDataPath(core.PathsIn(defaultDir)); got != "" {
		t.Errorf("the user's WebView data moved to %q", got)
	}
	sep := string(filepath.Separator)
	variants := []string{defaultDir, defaultDir + sep + ".." + sep + "Lumen" + sep}
	if runtime.GOOS == "windows" {
		variants = append(variants, strings.ToUpper(defaultDir))
	}
	for _, dir := range variants {
		t.Setenv("LUMEN_DATA_DIR", dir)
		paths, err := core.DefaultPaths()
		if err != nil {
			t.Fatal(err)
		}
		if got := instanceID(paths); got != "io.lumen.app" {
			t.Errorf("explicit default library %q: %q", dir, got)
		}
		if got := webviewDataPath(paths); got != "" {
			t.Errorf("explicit default library %q moved WebView data to %q", dir, got)
		}
	}
}

// Through Wails' own bindings, a service method's error reaches the UI with
// its code: Wails gives bound methods their service's marshaler, not the
// application's, so service() must name it (a wrong PIN, here).
func TestServiceErrorsCarryTheirCodes(t *testing.T) {
	application.New(application.Options{}) // Bindings.Add logs through the application
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := application.NewBindings(nil, nil)
	if err := b.Add(service(services.NewLibraryService(st, nil, nil, nil), application.ServiceOptions{})); err != nil {
		t.Fatal(err)
	}
	m := b.Get(&application.CallOptions{MethodName: "lumen/services.LibraryService.UnlockBlocks"})
	if m == nil {
		t.Fatal("UnlockBlocks is not bound")
	}
	_, err = m.Call(t.Context(), []json.RawMessage{json.RawMessage(`"000000"`)})
	var cerr *application.CallError
	if !errors.As(err, &cerr) {
		t.Fatalf("error %T: %v", err, err)
	}
	cause, err := json.Marshal(cerr.Cause)
	if err != nil {
		t.Fatal(err)
	}
	if string(cause) != `{"code":"invalid_pin"}` {
		t.Errorf("cause = %s, want the invalid_pin code", cause)
	}

	// The system service is registered the same way.
	lib := services.NewLibraryService(st, nil, nil, nil)
	sys := services.NewSystemService(lib, diag.New(t.TempDir(), "test"), core.PathsIn(t.TempDir()), "test")
	if err := b.Add(service(sys, application.ServiceOptions{})); err != nil {
		t.Fatal(err)
	}
	m = b.Get(&application.CallOptions{MethodName: "lumen/services.SystemService.OpenLink"})
	if m == nil {
		t.Fatal("OpenLink is not bound")
	}
	_, err = m.Call(t.Context(), []json.RawMessage{json.RawMessage(`"nowhere"`)})
	if !errors.As(err, &cerr) {
		t.Fatalf("error %T: %v", err, err)
	}
	if cause, _ := json.Marshal(cerr.Cause); string(cause) != `{"code":"not_found"}` {
		t.Errorf("cause = %s, want the not_found code", cause)
	}
}

func TestARecoveredPanicIsKeptAsAReport(t *testing.T) {
	reports := diag.New(t.TempDir(), "test")
	recordPanics(reports)(&application.PanicDetails{
		Error: errors.New("boom"), StackTrace: "short", FullStackTrace: "goroutine 7 [running]", Time: time.Now(),
	})
	names, err := reports.Files()
	if err != nil || len(names) != 1 || !strings.HasPrefix(names[0], "panic-") {
		t.Fatalf("reports = %v (%v), want one panic report", names, err)
	}
	b, err := os.ReadFile(filepath.Join(reports.Dir(), names[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "panic: boom") || !strings.Contains(string(b), "goroutine 7 [running]") {
		t.Fatalf("report = %s", b)
	}
}

// readRepoFile reads a file of the repository by its slash path.
func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Lumen's version is set once, in build/config.yml. The files Wails generates
// from it (wails3 task common:update:build-assets) give it to Lumen.exe and
// the installer, so they must not drift from it.
func TestVersionsAgree(t *testing.T) {
	m := regexp.MustCompile(`(?m)^info:\n(?:[ \t]+.*\n)*?[ \t]+version: "([^"]+)"`).FindStringSubmatch(readRepoFile(t, "build/config.yml"))
	if m == nil {
		t.Fatal("build/config.yml has no info.version")
	}
	want := m[1]
	var info struct {
		Fixed struct {
			FileVersion string `json:"file_version"`
		} `json:"fixed"`
		Info map[string]struct{ ProductVersion string } `json:"info"`
	}
	if err := json.Unmarshal([]byte(readRepoFile(t, "build/windows/info.json")), &info); err != nil {
		t.Fatal(err)
	}
	// Wails writes the US English block: Explorer hides the version details
	// of a language-neutral ("0000") one.
	found := map[string]string{
		"info.json file_version":   info.Fixed.FileVersion,
		"info.json ProductVersion": info.Info["0409"].ProductVersion,
	}
	for name, re := range map[string]string{
		"build/windows/nsis/wails_tools.nsh": `!define INFO_PRODUCTVERSION "([^"]+)"`,
		"build/windows/wails.exe.manifest":   `<assemblyIdentity [^>]*version="([^"]+)"`,
	} {
		if m := regexp.MustCompile(re).FindStringSubmatch(readRepoFile(t, name)); m != nil {
			found[name] = m[1]
		} else {
			found[name] = "(none)"
		}
	}
	for name, got := range found {
		if got != want {
			t.Errorf("%s = %q, build/config.yml says %q: run wails3 task common:update:build-assets", name, got, want)
		}
	}
}

// The copyright is set once, in build/config.yml, and the LICENSE's Required
// Notice gives the same text. Regenerating the build assets also resets the
// Linux package's licence to Wails' template default, so both are checked.
func TestLicenceAndCopyrightAgree(t *testing.T) {
	m := regexp.MustCompile(`(?m)^[ \t]+copyright: "([^"]+)"`).FindStringSubmatch(readRepoFile(t, "build/config.yml"))
	if m == nil {
		t.Fatal("build/config.yml has no info.copyright")
	}
	want := m[1]
	if !strings.Contains(readRepoFile(t, "LICENSE"), "\nRequired Notice: "+want+" (") {
		t.Errorf("LICENSE has no Required Notice reading %q", want)
	}
	for name, text := range map[string]string{
		"build/windows/info.json":            `"LegalCopyright": "` + want + `"`,
		"build/windows/nsis/wails_tools.nsh": `!define INFO_COPYRIGHT "` + want + `"`,
		"build/linux/nfpm/nfpm.yaml":         `license: "PolyForm-Noncommercial-1.0.0"`,
	} {
		if !strings.Contains(readRepoFile(t, name), text) {
			t.Errorf("%s lacks %s: run wails3 task common:update:build-assets, then restore the nfpm licence", name, text)
		}
	}
}

// The installer's Add/Remove Programs entry links the releases page the
// About section shows.
func TestInstallerNamesTheReleasesPage(t *testing.T) {
	if !strings.Contains(readRepoFile(t, "build/windows/nsis/project.nsi"), `!define LUMEN_RELEASES "`+services.ReleasesURL+`"`) {
		t.Fatalf("project.nsi's LUMEN_RELEASES is not services.ReleasesURL (%s)", services.ReleasesURL)
	}
}

// Client-side routes are answered with the page; files and the development
// server's own modules are asked for as they are.
func TestSPAServesRoutesAsThePageAndLeavesFilesAndDevModulesAlone(t *testing.T) {
	var asked string
	h := spa(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { asked = r.URL.Path }))
	for path, want := range map[string]string{
		"/":                                   "/",
		"/things":                             "/",
		"/settings/blocked":                   "/",
		"/people/3/faces":                     "/",
		"/assets/index-abc.js":                "/assets/index-abc.js",
		"/lumen.png":                          "/lumen.png",
		"/@vite/client":                       "/@vite/client",
		"/@id/__x00__virtual:nuxt-ui-plugins": "/@id/__x00__virtual:nuxt-ui-plugins",
		"/@fs/D:/Downloads/Lumen/frontend/x":  "/@fs/D:/Downloads/Lumen/frontend/x",
	} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))
		if asked != want {
			t.Errorf("spa asked the files for %q on a request for %q, want %q", asked, path, want)
		}
	}
}

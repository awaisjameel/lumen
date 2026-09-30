package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
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

// A scratch library (LUMEN_DATA_DIR) runs beside the user's Lumen: one
// single-instance ID per library, the same whatever the path's case.
func TestInstanceIDPerLibrary(t *testing.T) {
	t.Setenv("LUMEN_DATA_DIR", "")
	if got := instanceID(core.PathsIn(`C:\Users\a\AppData\Local\Lumen`)); got != "io.lumen.app" {
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
	if got := webviewDataPath(core.PathsIn(`C:\Users\a\AppData\Local\Lumen`)); got != "" {
		t.Errorf("the user's WebView data moved to %q", got)
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
	if err := b.Add(service(services.NewLibraryService(st, nil), application.ServiceOptions{})); err != nil {
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
}

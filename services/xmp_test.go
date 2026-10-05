package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
	"lumen/internal/index"
	"lumen/internal/store"
)

func TestXMPIsOffByDefaultAndTheSettingsFollowTheToggle(t *testing.T) {
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := NewLibraryService(st, index.New(st, paths, nil, index.Models{}, quietEvents{}), nil, nil)

	if s, err := svc.Settings(ctx); err != nil || s.XMP {
		t.Fatalf("settings = %+v, %v; want XMP off by default", s, err)
	}
	_, err = svc.ExportXMPNow(ctx)
	if !errors.Is(err, index.ErrXMPOff) {
		t.Fatalf("Export now while off = %v, want ErrXMPOff", err)
	}
	if got := MarshalError(err); string(got) != `{"code":"xmp_off"}` {
		t.Fatalf("the UI sees %s, want the xmp_off code", got)
	}
	if err := svc.SetXMP(ctx, true); err != nil {
		t.Fatal(err)
	}
	if s, err := svc.Settings(ctx); err != nil || !s.XMP {
		t.Fatalf("settings = %+v, %v; want XMP on", s, err)
	}
	if err := svc.SetXMP(ctx, false); err != nil {
		t.Fatal(err)
	}
	if s, err := svc.Settings(ctx); err != nil || s.XMP {
		t.Fatalf("settings = %+v, %v; want XMP off again", s, err)
	}
}

func TestExportXMPNowReportsWhatItWroteLeftAndSkipped(t *testing.T) {
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dir := t.TempDir()
	src, err := st.AddSource(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	var items []store.IngestItem
	for _, name := range []string{"a", "b", "c"} {
		p := filepath.Join(dir, name+".jpg")
		if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		items = append(items, store.IngestItem{SourceID: src.ID, Path: p, Size: 1, MTime: 1, Hash: []byte(name),
			Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile})
	}
	if _, err := st.Ingest(ctx, items); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if err := st.SetFavourite(ctx, j.AssetID, true); err != nil {
			t.Fatal(err)
		}
	}
	// Someone else's sidecar sits beside c.
	foreign := filepath.Join(dir, "c.jpg.xmp")
	if err := os.WriteFile(foreign, []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := NewLibraryService(st, index.New(st, paths, nil, index.Models{}, quietEvents{}), nil, nil)
	if err := svc.SetXMP(ctx, true); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ExportXMPNow(ctx)
	if err != nil || res != (core.XMPExport{Written: 2, Skipped: 1}) {
		t.Fatalf("first export = %+v, %v; want two written and one skipped", res, err)
	}
	res, err = svc.ExportXMPNow(ctx)
	if err != nil || res != (core.XMPExport{Unchanged: 2, Skipped: 1}) {
		t.Fatalf("second export = %+v, %v; want two unchanged and one skipped", res, err)
	}
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "theirs" {
		t.Fatalf("the foreign sidecar = %q, %v", b, err)
	}
}

func TestDeleteXMPTurnsItOffAndRecyclesWhatLumenWrote(t *testing.T) {
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dir := t.TempDir()
	src, err := st.AddSource(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	photo := filepath.Join(dir, "a.jpg")
	if err := os.WriteFile(photo, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: photo, Size: 1, MTime: 1, Hash: []byte("a"),
		Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile}}); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetFavourite(ctx, jobs[0].AssetID, true); err != nil {
		t.Fatal(err)
	}
	rec := &fakeRecycler{}
	svc := NewLibraryService(st, index.New(st, paths, nil, index.Models{}, quietEvents{}), nil, nil)
	svc.recycle = rec.recycle
	if err := svc.SetXMP(ctx, true); err != nil {
		t.Fatal(err)
	}
	if res, err := svc.ExportXMPNow(ctx); err != nil || res.Written != 1 {
		t.Fatalf("export = %+v, %v; want one written", res, err)
	}

	res, err := svc.DeleteXMP(ctx)
	if err != nil || res != (core.XMPDelete{Deleted: 1}) {
		t.Fatalf("delete = %+v, %v; want one deleted", res, err)
	}
	if rec.called() != 1 {
		t.Fatalf("the recycler was called %d times, want once", rec.called())
	}
	if s, err := svc.Settings(ctx); err != nil || s.XMP {
		t.Fatalf("settings = %+v, %v; want XMP off after deleting", s, err)
	}
	if res, err := svc.DeleteXMP(ctx); err != nil || res != (core.XMPDelete{}) {
		t.Fatalf("second delete = %+v, %v; want nothing left to delete", res, err)
	}
}

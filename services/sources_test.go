package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/index"
	"lumen/internal/store"
)

func TestSourceRecheckReturnsImmediatelyAndPublishesCachedAvailability(t *testing.T) {
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	check(t, err)
	defer st.Close()
	root := filepath.Join(t.TempDir(), "photos")
	check(t, os.Mkdir(root, 0o750))
	_, err = st.AddSource(t.Context(), root)
	check(t, err)
	ix := index.New(st, paths, nil, index.Models{}, quietEvents{})
	s := NewLibraryService(st, ix, nil, nil)
	srcs, err := s.Sources(t.Context())
	check(t, err)
	if len(srcs) != 1 || srcs[0].Availability != core.SourceChecking {
		t.Fatalf("unchecked sources: %+v", srcs)
	}
	ix.Start(t.Context())
	defer ix.Stop()
	wait := func(want core.SourceAvailability) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			srcs, err := s.Sources(t.Context())
			check(t, err)
			if srcs[0].Availability == want {
				return
			}
		}
		t.Fatalf("source did not become %s", want)
	}
	wait(core.SourceAvailable)
	check(t, os.Remove(root))
	check(t, s.RecheckSources(t.Context()))
	wait(core.SourceUnavailable)
	check(t, os.Mkdir(root, 0o750))
	check(t, s.RecheckSources(t.Context()))
	wait(core.SourceAvailable)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.RecheckSources(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recheck: %v", err)
	}
}

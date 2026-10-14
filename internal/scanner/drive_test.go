package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
	"lumen/internal/store"
)

func TestHashWorkersUseOneReaderOnASeekingDrive(t *testing.T) {
	for _, tc := range []struct {
		name           string
		seeks          bool
		err            error
		override, want int
	}{
		{name: "spinning", seeks: true, want: 1},
		{name: "solid state", want: min(8, core.DefaultCPUThreads())},
		{name: "unknown", err: errors.New("unknown"), want: 4},
		{name: "untrusted answer", seeks: true, err: errors.New("failed"), want: 4},
		{name: "explicit override", seeks: true, override: 8, want: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			s := Scanner{HashWorkers: tc.override, seekPenalty: func(ctx context.Context, root string) (bool, error) {
				calls++
				if ctx != t.Context() || root != "resolved root" {
					t.Fatal("probe did not receive the scan root and context")
				}
				return tc.seeks, tc.err
			}}
			if got := s.hashWorkers(t.Context(), "resolved root"); got != tc.want {
				t.Fatalf("workers=%d, want %d", got, tc.want)
			}
			if tc.override > 0 && calls != 0 {
				t.Fatal("explicit worker choice probed the drive")
			}
		})
	}
}

func TestScanProbesItsFolderBeforeHashing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "one.jpg"), []byte("photo"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	src, err := st.AddSource(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s := Scanner{Store: st, seekPenalty: func(ctx context.Context, path string) (bool, error) {
		calls++
		if ctx.Err() != nil || path != root {
			t.Fatalf("probe path=%q context=%v", path, ctx.Err())
		}
		return true, nil
	}}
	res, err := s.Scan(t.Context(), src, "", Options{})
	if err != nil || res.Changed != 1 || calls != 1 {
		t.Fatalf("scan=%+v probe calls=%d err=%v", res, calls, err)
	}
}

func TestSolidStateHashingHonoursTheCPUThreadBudget(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	s := Scanner{Store: st, seekPenalty: func(context.Context, string) (bool, error) { return false, nil }}
	for _, n := range []int{1, 2, 8, core.MaxCPUThreads()} {
		stored, err := st.SetCPUThreads(t.Context(), n)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.hashWorkers(t.Context(), "root"); got != min(8, stored) {
			t.Fatalf("budget=%d workers=%d", stored, got)
		}
	}
}

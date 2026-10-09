package compress

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/scanner"
	"lumen/internal/store"
)

func compressionFixture(t *testing.T, mode core.CompressionMode) (coordinator *Coordinator, runID int64, sourcePath string) {
	t.Helper()
	path, codec := codecFixture(t, ".png", true)
	if mode == core.CompressReversible {
		path, codec = codecFixture(t, ".jpg", false)
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
	source, err := st.AddSource(t.Context(), filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := scanner.Fingerprint(t.Context(), path, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Ingest(t.Context(), []store.IngestItem{{SourceID: source.ID, Path: path, Size: info.Size(), MTime: info.ModTime().UnixNano(), Hash: hash, Kind: core.KindPhoto, Ext: filepath.Ext(path), TakenAt: time.Now().Unix(), DateFrom: core.DateFromFile}})
	if err != nil {
		t.Fatal(err)
	}
	c := &Coordinator{Store: st, Root: filepath.Join(t.TempDir(), "candidates"), Codec: codec}
	o := core.DefaultCompressionOptions()
	o.Mode = mode
	run, err := c.Plan(t.Context(), []core.CompressionInput{{AssetID: 1}}, "", o)
	if err != nil {
		t.Fatal(err)
	}
	return c, run, path
}

func firstJob(t *testing.T, c *Coordinator, run int64) core.CompressionJob {
	t.Helper()
	jobs, err := c.Store.CompressionJobs(t.Context(), run)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs: %+v %v", jobs, err)
	}
	return jobs[0]
}

func TestCompressionReviewsBeforePublishingAndRetainsAnnotationsAndExternalSidecars(t *testing.T) {
	c, run, source := compressionFixture(t, core.CompressCompatible)
	if err := c.Store.SetFavourite(t.Context(), 1, true); err != nil {
		t.Fatal(err)
	}
	sidecar := []byte("external XMP must stay external")
	if err := os.WriteFile(source+".xmp", sidecar, 0o600); err != nil {
		t.Fatal(err)
	}
	original, _, err := fileops.FileDigest(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Analyse(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	j := firstJob(t, c, run)
	if j.State != core.CompressionReady || j.OutputSize >= j.Size {
		t.Fatalf("candidate: %+v", j)
	}
	if _, err := os.Stat(j.To); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("analysis published a file")
	}
	if err := c.Commit(t.Context(), run, []int64{j.ID}); err != nil {
		t.Fatal(err)
	}
	j = firstJob(t, c, run)
	if j.State != core.CompressionDone || j.OutputAssetID == 0 || j.OutputAssetID == 1 {
		t.Fatalf("related output: %+v", j)
	}
	detail, err := c.Store.AssetDetail(t.Context(), j.OutputAssetID, false)
	if err != nil || !detail.Favourite {
		t.Fatalf("annotation lost: %+v %v", detail, err)
	}
	for _, path := range []string{source + ".xmp", j.To + ".xmp"} {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, sidecar) {
			t.Fatalf("sidecar changed: %s", path)
		}
	}
	after, _, err := fileops.FileDigest(t.Context(), source)
	if err != nil || after != original {
		t.Fatal("source changed")
	}
}

func TestCompressionRefusesChangedSourcesAndDestinationRaces(t *testing.T) {
	for _, changeSource := range []bool{true, false} {
		t.Run(map[bool]string{true: "source changes", false: "destination is taken"}[changeSource], func(t *testing.T) {
			c, run, source := compressionFixture(t, core.CompressCompatible)
			if err := c.Analyse(t.Context(), run); err != nil {
				t.Fatal(err)
			}
			j := firstJob(t, c, run)
			path := j.To
			if changeSource {
				path = source
			}
			if err := os.WriteFile(path, []byte("keep this later work"), 0o600); err != nil {
				t.Fatal(err)
			}
			_ = c.Commit(t.Context(), run, []int64{j.ID})
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "keep this later work" {
				t.Fatal("later work overwritten")
			}
			if firstJob(t, c, run).State == core.CompressionDone {
				t.Fatal("changed source or destination accepted")
			}
		})
	}
}

func TestCompressionCancellationAndBlockingNeverPublishCandidates(t *testing.T) {
	c, run, _ := compressionFixture(t, core.CompressCompatible)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Analyse(ctx, run); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if err := c.Analyse(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	j := firstJob(t, c, run)
	_, err := c.Store.SetBlock(t.Context(), core.BlockAsset, 1, "", core.BlockPhotos)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Commit(t.Context(), run, []int64{j.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.To); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("blocked asset published")
	}
	if _, err := c.Store.VisibleCompressionJob(t.Context(), run, j.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("blocked history disclosed: %v", err)
	}
}

func TestReadyCandidatesSurviveRestartButInterruptedEncodingRequiresAnotherReview(t *testing.T) {
	c, run, _ := compressionFixture(t, core.CompressCompatible)
	if err := c.Analyse(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	j := firstJob(t, c, run)
	if err := c.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if firstJob(t, c, run).State != core.CompressionReady {
		t.Fatal("ready candidate lost on restart")
	}
	j.State = core.CompressionVerifying
	if err := c.Store.SetCompressionJob(t.Context(), j); err != nil {
		t.Fatal(err)
	}
	if err := c.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if firstJob(t, c, run).State != core.CompressionPlanned {
		t.Fatal("interrupted verification trusted")
	}
	if _, err := os.Stat(j.Temp); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted candidate retained")
	}
}

func TestReversibleJPEGRestorationIsByteExactInsideAndOutsideTheLibrary(t *testing.T) {
	for _, outside := range []bool{false, true} {
		t.Run(map[bool]string{false: "inside", true: "outside"}[outside], func(t *testing.T) {
			c, initial, source := compressionFixture(t, core.CompressReversible)
			original, size, err := fileops.FileDigest(t.Context(), source)
			if err != nil {
				t.Fatal(err)
			}
			run := initial
			if outside {
				o := core.DefaultCompressionOptions()
				o.Mode = core.CompressReversible
				run, err = c.Plan(t.Context(), []core.CompressionInput{{AssetID: 1}}, t.TempDir(), o)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Analyse(t.Context(), run); err != nil {
				t.Fatal(err)
			}
			j := firstJob(t, c, run)
			if j.State != core.CompressionReady {
				t.Fatalf("reconstruction candidate: %+v", j)
			}
			if err := c.Commit(t.Context(), run, []int64{j.ID}); err != nil {
				t.Fatal(err)
			}
			restore, err := c.RestorePlan(t.Context(), run, j.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Analyse(t.Context(), restore); err != nil {
				t.Fatal(err)
			}
			r := firstJob(t, c, restore)
			if r.State != core.CompressionReady {
				t.Fatalf("restoration candidate: %+v", r)
			}
			if err := c.Commit(t.Context(), restore, []int64{r.ID}); err != nil {
				t.Fatal(err)
			}
			actual, n, err := fileops.FileDigest(t.Context(), r.To)
			if err != nil || actual != original || n != size {
				t.Fatalf("JPEG round trip: %s %d %v", actual, n, err)
			}
		})
	}
}

func TestRestartSettlesPublishedBytesOnceAndPreservesAnUnrelatedDestination(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "published", false: "unrelated"}[valid], func(t *testing.T) {
			c, run, _ := compressionFixture(t, core.CompressCompatible)
			if err := c.Analyse(t.Context(), run); err != nil {
				t.Fatal(err)
			}
			j := firstJob(t, c, run)
			data, err := os.ReadFile(j.Temp)
			if err != nil {
				t.Fatal(err)
			}
			if !valid {
				data = []byte("keep the user's later file")
			}
			if err := os.WriteFile(j.To, data, 0o600); err != nil {
				t.Fatal(err)
			}
			j.State = core.CompressionCommitting
			if err := c.Store.SetCompressionJob(t.Context(), j); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := c.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			got := firstJob(t, c, run)
			if valid && (got.State != core.CompressionDone || got.OutputAssetID == 0) || !valid && got.State != core.CompressionFailed {
				t.Fatalf("recovered: %+v", got)
			}
			actual, err := os.ReadFile(j.To)
			if err != nil || !bytes.Equal(actual, data) {
				t.Fatal("recovery modified the destination")
			}
		})
	}
}

// Waiting for the indexer's politeness can take hours. A block applied
// meanwhile stops that one file, with its reason, and never the whole run.
func TestABlockAppliedWhileAFileWaitsItsTurnStopsOnlyThatFile(t *testing.T) {
	c, run, _ := compressionFixture(t, core.CompressCompatible)
	c.Acquire = func(ctx context.Context, _ int) (context.Context, func(), int, error) {
		if _, err := c.Store.SetBlock(t.Context(), core.BlockAsset, 1, "", core.BlockPhotos); err != nil {
			t.Error(err)
		}
		<-ctx.Done()
		return ctx, nil, 0, ctx.Err()
	}
	if err := c.Analyse(t.Context(), run); err != nil {
		t.Fatalf("the run stopped with its blocked file: %v", err)
	}
	if j := firstJob(t, c, run); j.State != core.CompressionFailed || j.Reason != "not_visible" || j.Temp != "" {
		t.Fatalf("blocked file: %+v", j)
	}
}

func TestRecoverySettlesEveryRowEvenWhenOneCannotBeSettled(t *testing.T) {
	c, stuck, _ := compressionFixture(t, core.CompressCompatible)
	later, err := c.Plan(t.Context(), []core.CompressionInput{{AssetID: 1}}, "", core.DefaultCompressionOptions())
	if err != nil {
		t.Fatal(err)
	}
	// A candidate path outside the owned folder is never removed: that row stays in doubt.
	foreign := firstJob(t, c, stuck)
	foreign.State, foreign.Temp = core.CompressionEncoding, filepath.Join(t.TempDir(), "elsewhere", "candidate")
	if err := c.Store.SetCompressionJob(t.Context(), foreign); err != nil {
		t.Fatal(err)
	}
	interrupted := firstJob(t, c, later)
	interrupted.State = core.CompressionVerifying
	if err := c.Store.SetCompressionJob(t.Context(), interrupted); err != nil {
		t.Fatal(err)
	}
	if err := c.Recover(t.Context()); !errors.Is(err, ErrPlan) {
		t.Fatalf("recovery: %v, want the row it could not settle", err)
	}
	if j := firstJob(t, c, stuck); j.State != core.CompressionEncoding {
		t.Fatalf("the row in doubt was settled: %+v", j)
	}
	if j := firstJob(t, c, later); j.State != core.CompressionPlanned || j.Reason != "interrupted" {
		t.Fatalf("a later row was left unsettled: %+v", j)
	}
}

// A review never saved or cancelled keeps its candidate file. Once history no
// longer reaches its run, nothing in the interface could discard it.
func TestRunsOlderThanHistoryReachesAreRemovedWithTheirCandidates(t *testing.T) {
	c, old, _ := compressionFixture(t, core.CompressCompatible)
	if err := c.Analyse(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	candidate := firstJob(t, c, old)
	if candidate.State != core.CompressionReady {
		t.Fatalf("candidate: %+v", candidate)
	}
	for range core.CompressionHistoryRuns - 1 {
		if _, err := c.Store.SaveCompressionPlan(t.Context(), core.DefaultCompressionOptions(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(candidate.Temp); err != nil {
		t.Fatalf("a candidate history still reaches was removed: %v", err)
	}
	if _, err := c.Store.SaveCompressionPlan(t.Context(), core.DefaultCompressionOptions(), nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(candidate.Temp)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an unreachable candidate was kept: %v", err)
	}
	if _, err := c.Store.CompressionRun(t.Context(), old); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an unreachable run was kept: %v", err)
	}
}

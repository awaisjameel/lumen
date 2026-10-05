package fileops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/core"
)

func exportFixture(t *testing.T) (library, core.ExportJob, *ExportRunner) {
	t.Helper()
	l := newLibrary(t)
	id := l.add("a.jpg", "original bytes", day(2025, time.June, 1))
	info, err := os.Stat(l.path("a.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	job := core.ExportJob{AssetID: id, From: l.path("a.jpg"), To: filepath.Join(t.TempDir(), "copy.jpg"), Size: info.Size(), MTime: info.ModTime().UnixNano(), Options: core.ExportOptions{Format: "original", Frame: -1}}
	batch, err := l.st.SaveExportPlan(l.ctx, []core.ExportJob{job})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := l.st.ExportJobs(l.ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	return l, jobs[0], &ExportRunner{Store: l.st}
}

func TestExportPreservesOriginalAndRefusesDestinationReplacement(t *testing.T) {
	l, job, runner := exportFixture(t)
	if err := runner.Run(l.ctx, job.Batch); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{job.From, job.To} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "original bytes" {
			t.Fatalf("copy at %s: %q %v", path, data, err)
		}
	}
	jobs, err := l.st.ExportJobs(l.ctx, job.Batch)
	if err != nil || jobs[0].State != "done" {
		t.Fatalf("outcome: %+v %v", jobs, err)
	}
	l2, second, other := exportFixture(t)
	l2.write(second.To, "keep this")
	if err := other.Run(l2.ctx, second.Batch); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(second.To)
	if err != nil || string(data) != "keep this" {
		t.Fatal("destination overwritten")
	}
	jobs, err = l2.st.ExportJobs(l2.ctx, second.Batch)
	if err != nil || jobs[0].State != "failed" {
		t.Fatalf("collision outcome: %+v %v", jobs, err)
	}
}

func TestExportCancellationAndPrivacyRevocationDoNotPublish(t *testing.T) {
	l, job, runner := exportFixture(t)
	ctx, cancel := context.WithCancel(l.ctx)
	cancel()
	if err := runner.Run(ctx, job.Batch); err == nil {
		t.Fatal("cancelled lookup succeeded")
	}
	if _, err := l.st.SetBlock(l.ctx, core.BlockAsset, job.AssetID, "", core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(l.ctx, job.Batch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(job.To); !os.IsNotExist(err) {
		t.Fatal("covered export was published")
	}
	history, err := l.st.VisibleExportJobs(l.ctx, 0)
	if err != nil || len(history) != 0 {
		t.Fatalf("covered path leaked to history: %+v %v", history, err)
	}
}

func TestExportRecoveryVerifiesPublishedBytesAndRemovesOwnTemporaryFile(t *testing.T) {
	l, job, runner := exportFixture(t)
	l.write(job.To, "published")
	tmp := filepath.Join(filepath.Dir(job.To), ".lumen-export-interrupted")
	l.write(tmp, "partial")
	digest := sha256.Sum256([]byte("published"))
	if err := l.st.SetExportJob(l.ctx, job.ID, "ready", "", tmp, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	if err := runner.Recover(l.ctx); err != nil {
		t.Fatal(err)
	}
	jobs, err := l.st.ExportJobs(l.ctx, job.Batch)
	if err != nil || jobs[0].State != "done" {
		t.Fatalf("recovery: %+v %v", jobs, err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("partial export remains")
	}
	if _, err := os.Stat(job.From); err != nil {
		t.Fatal("recovery altered original")
	}
}

func TestExportRevalidatesAfterRenderingAndCleansTemporaryCopies(t *testing.T) {
	for _, reason := range []string{"cancel", "source", "privacy"} {
		t.Run(reason, func(t *testing.T) {
			l, original, runner := exportFixture(t)
			original.Options.Format = "png"
			batch, err := l.st.SaveExportPlan(l.ctx, []core.ExportJob{original})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(l.ctx)
			defer cancel()
			runner.Render = func(context.Context, core.ExportJob) ([]byte, error) {
				switch reason {
				case "cancel":
					cancel()
				case "source":
					l.write(original.From, "changed source bytes")
				case "privacy":
					if _, err := l.st.SetBlock(l.ctx, core.BlockAsset, original.AssetID, "", core.BlockPhotos); err != nil {
						t.Fatal(err)
					}
				}
				return []byte("rendered output"), nil
			}
			if err := runner.Run(ctx, batch); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(original.To); !os.IsNotExist(err) {
				t.Fatal("invalidated export was published")
			}
			entries, err := os.ReadDir(filepath.Dir(original.To))
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary copies remain: %v %v", entries, err)
			}
			jobs, err := l.st.ExportJobs(l.ctx, batch)
			want := "failed"
			if reason == "cancel" {
				want = "cancelled"
			}
			if err != nil || len(jobs) != 1 || jobs[0].State != want {
				t.Fatalf("outcome: %+v %v", jobs, err)
			}
		})
	}
}

func TestFreeNameSkipsNamesOnDiskAndNamesAlreadyPlanned(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	reserved := map[string]bool{core.Fold(filepath.Join(dir, "A (2).JPG")): true}
	got, err := FreeName(dir, "a.jpg", reserved)
	if err != nil || got != filepath.Join(dir, "a (3).jpg") {
		t.Fatalf("FreeName = %q, %v", got, err)
	}
	if got, err := FreeName(dir, "b.jpg", reserved); err != nil || got != filepath.Join(dir, "b.jpg") {
		t.Fatalf("a free name was changed: %q, %v", got, err)
	}
}

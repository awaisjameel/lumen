//go:build windows

package fileops

import (
	"context"
	"errors"
	"os"
	"testing"

	"lumen/internal/core"
	"lumen/internal/media"
)

func TestFailedTemporaryCleanupKeepsTheOwnedCopyInTheRecoveryJournal(t *testing.T) {
	l, job, runner := exportFixture(t)
	job.Options.Format = core.ExportPNG
	batch, err := l.st.SaveExportPlan(l.ctx, []core.ExportJob{job})
	if err != nil {
		t.Fatal(err)
	}
	var held *os.File
	defer func() {
		if held != nil {
			_ = held.Close()
		}
	}()
	runner.Render = func(ctx context.Context, current core.ExportJob) ([]byte, error) {
		jobs, err := l.st.ExportJobs(ctx, current.Batch)
		if err != nil {
			return nil, err
		}
		// Windows refuses deletion while an ordinary file handle remains open.
		held, err = os.Open(jobs[0].Temp)
		if err != nil {
			return nil, err
		}
		return nil, media.ErrUnsupported
	}
	var pending *pendingExportError
	if err := runner.Run(l.ctx, batch); !errors.As(err, &pending) {
		t.Fatalf("temporary cleanup failure was hidden: %v", err)
	}
	jobs, err := l.st.ExportJobs(l.ctx, batch)
	if err != nil || len(jobs) != 1 || jobs[0].State != core.ExportWriting || jobs[0].Temp == "" {
		t.Fatalf("recovery lost the owned path: %+v, %v", jobs, err)
	}
	temp := jobs[0].Temp
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	held = nil
	if err := runner.Recover(l.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery left the temporary copy: %v", err)
	}
	jobs, err = l.st.ExportJobs(l.ctx, batch)
	if err != nil || jobs[0].State != core.ExportFailed || jobs[0].Temp != "" {
		t.Fatalf("recovered outcome: %+v, %v", jobs, err)
	}
}

package compress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
)

// ErrPlan rejects invalid options, duplicate sources or a non-reviewed selection.
var ErrPlan = errors.New("invalid compression plan")

// Coordinator owns candidate lifetime and recovery, while store owns its journal.
type Coordinator struct {
	Store   *store.Store
	Root    string
	Codec   Codec
	Acquire func(context.Context, int) (context.Context, func(), int, error)
	// Admit takes the existing file-operation owner briefly for publication.
	Admit func(context.Context) (context.Context, func(), error)
}

// Plan resolves exactly the requested copies; absent paths choose one readable copy.
func (c *Coordinator) Plan(ctx context.Context, inputs []core.CompressionInput, directory string, options core.CompressionOptions) (int64, error) {
	if len(inputs) == 0 || len(inputs) > core.CompressionMaxInputs || options != options.Normalised() || directory != "" && !filepath.IsAbs(directory) {
		return 0, ErrPlan
	}
	jobs := make([]core.CompressionJob, 0, len(inputs))
	reserved := make(map[string]bool)
	for _, input := range inputs {
		f, err := c.Store.AssetFile(ctx, input.AssetID, false)
		if err != nil {
			return 0, err
		}
		if len(f.Paths) == 0 {
			return 0, store.ErrNotFound
		}
		path := input.Path
		if path == "" {
			for _, copyPath := range f.Paths {
				if info, err := os.Lstat(copyPath); err == nil && info.Mode().IsRegular() {
					path = copyPath
					break
				}
			}
		}
		if path == "" {
			path = f.Paths[0]
		}
		if !slices.Contains(f.Paths, path) || reserved[core.Fold(path)] {
			return 0, ErrPlan
		}
		reserved[core.Fold(path)] = true
		j := core.CompressionJob{AssetID: input.AssetID, From: path, Copies: len(f.Paths), State: core.CompressionPlanned}
		dir := directory
		if dir == "" {
			dir = filepath.Dir(path)
		}
		ext := filepath.Ext(path)
		if options.Mode != core.CompressCompatible {
			ext = ".jxl"
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + "-compressed" + ext
		j.To, err = c.freeName(ctx, dir, name, reserved)
		if err != nil {
			return 0, err
		}
		reserved[core.Fold(j.To)] = true
		if err := c.Store.ExportAllowed(ctx, input.AssetID, j.To); err != nil {
			return 0, err
		}
		eligible := f.Kind == core.KindPhoto && core.CompressionSupported(core.FamilyOf(path), options.Mode)
		info, err := os.Lstat(path)
		switch {
		case !eligible:
			j.State, j.Reason = core.CompressionSkipped, "unsupported"
		case err != nil:
			j.State, j.Reason = core.CompressionSkipped, "missing"
		case !info.Mode().IsRegular() || info.Size() > maxInputBytes:
			j.State, j.Reason = core.CompressionSkipped, "limits"
		default:
			j.Size, j.MTime = info.Size(), info.ModTime().UnixNano()
		}
		jobs = append(jobs, j)
	}
	return c.Store.SaveCompressionPlan(ctx, options, jobs)
}

// Analyse prepares candidates one file at a time. It never commits a result.
func (c *Coordinator) Analyse(ctx context.Context, runID int64) error {
	run, err := c.Store.CompressionRun(ctx, runID)
	if err != nil {
		return err
	}
	jobs, err := c.Store.CompressionJobs(ctx, runID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if job.State != core.CompressionPlanned && job.State != core.CompressionFailed && job.State != core.CompressionCancelled {
			continue
		}
		if err := c.analyseOne(ctx, job, run.Options); err != nil {
			return err
		}
	}
	current, err := c.Store.CompressionJobs(ctx, runID)
	if err != nil {
		return err
	}
	state := core.CompressionDone
	if slices.ContainsFunc(current, func(j core.CompressionJob) bool { return j.State == core.CompressionReady }) {
		state = core.CompressionReady
	}
	return c.Store.SetCompressionRun(ctx, runID, state)
}

func (c *Coordinator) analyseOne(ctx context.Context, j core.CompressionJob, options core.CompressionOptions) error {
	if err := c.discard(ctx, j.Temp); err != nil {
		return err
	}
	j.Temp, j.PublishTemp, j.SidecarDigest, j.SidecarSize = "", "", "", 0
	runCtx, cancel := context.WithCancel(ctx)
	monitorCtx := runCtx
	defer cancel()
	// Ordinary compression never reads through BlockGate. A newly applied
	// block cancels the current worker and removes its uncommitted result.
	var covered atomic.Bool
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		timer := time.NewTicker(200 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-timer.C:
				if err := c.Store.ExportAllowed(monitorCtx, j.AssetID, j.To); errors.Is(err, store.ErrNotFound) {
					covered.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-monitorDone }()
	err := c.encodeInTurn(runCtx, &j, options)
	if err == nil {
		return nil
	}
	if cleanupErr := c.discard(context.WithoutCancel(ctx), j.Temp); cleanupErr != nil {
		return errors.Join(err, cleanupErr)
	}
	j.Temp, j.PublishTemp = "", ""
	j.State, j.Reason = core.CompressionFailed, reason(err)
	if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrPreservation) {
		j.State = core.CompressionSkipped
	}
	switch {
	case covered.Load():
		// The run goes on to its other files; this one says why it stopped.
		j.State, j.Reason = core.CompressionFailed, reason(store.ErrNotFound)
	case runCtx.Err() != nil:
		j.State, j.Reason = core.CompressionPlanned, "interrupted"
	}
	if saveErr := c.Store.SetCompressionJob(context.WithoutCancel(ctx), j); saveErr != nil {
		return saveErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// encodeInTurn waits for the shared CPU budget and the indexer's politeness,
// which can take as long as a film played full screen, then encodes with the
// threads it was granted.
func (c *Coordinator) encodeInTurn(ctx context.Context, j *core.CompressionJob, options core.CompressionOptions) error {
	if c.Acquire != nil {
		workCtx, release, threads, err := c.Acquire(ctx, options.Threads)
		if err != nil {
			return err
		}
		defer release()
		ctx, options.Threads = workCtx, threads
	}
	return c.encodeOne(ctx, j, options)
}

func (c *Coordinator) encodeOne(ctx context.Context, j *core.CompressionJob, options core.CompressionOptions) error {
	if err := c.Store.ExportAllowed(ctx, j.AssetID, j.To); err != nil {
		return err
	}
	if err := os.MkdirAll(c.Root, 0o750); err != nil {
		return err
	}
	if err := fileops.CopySpace(ctx, c.Root, j.Size*8); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(c.Root, fmt.Sprintf("run-%d-job-%d-", j.RunID, j.ID))
	if err != nil {
		return err
	}
	// A checkpoint owns the directory before a snapshot or encoder writes in it.
	j.Temp, j.State, j.Reason = filepath.Join(dir, "candidate"), core.CompressionEncoding, ""
	if err := c.Store.SetCompressionJob(ctx, *j); err != nil {
		return err
	}
	ext := ".png"
	if core.FamilyOf(j.From) == core.FamilyJPEG {
		ext = ".jpg"
	}
	if options.Mode == core.CompressRestore {
		ext = ".jxl"
	}
	source := filepath.Join(dir, "source"+ext)
	release, err := fileops.LeaseCopyFolders(ctx, filepath.Dir(j.From))
	if err != nil {
		return err
	}
	defer release()
	in, err := core.OpenStable(ctx, j.From)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != j.Size || info.ModTime().UnixNano() != j.MTime {
		return fileops.ErrSourceChanged
	}
	if err := snapshot(ctx, in, source); err != nil {
		return err
	}
	j.Digest, _, err = fileops.FileDigest(ctx, source)
	if err != nil {
		return err
	}
	if side, err := core.OpenStable(ctx, j.From+fileops.SidecarExt); err == nil {
		defer func() { _ = side.Close() }()
		info, err := side.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return ErrUnsupported
		}
		path := filepath.Join(dir, "sidecar.xmp")
		if err := snapshot(ctx, side, path); err != nil {
			return err
		}
		j.SidecarDigest, j.SidecarSize, err = fileops.FileDigest(ctx, path)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	notify := func() error { j.State = core.CompressionVerifying; return c.Store.SetCompressionJob(ctx, *j) }
	var path, verification, tool string
	if options.Mode == core.CompressRestore {
		path, verification, tool, err = c.Codec.Restore(ctx, source, dir, j.RestoreDigest, j.RestoreSize, notify)
	} else {
		path, verification, tool, err = c.Codec.Encode(ctx, source, dir, options, notify)
	}
	if err != nil {
		return err
	}
	j.Temp, j.Verification, j.Tool = path, verification, tool
	j.OutputDigest, j.OutputSize, err = fileops.FileDigest(ctx, path)
	if err != nil {
		return err
	}
	if options.Mode != core.CompressRestore && (j.OutputSize >= j.Size || (j.Size-j.OutputSize)*100 < int64(options.MinSavings)*j.Size) {
		j.State, j.Reason = core.CompressionSkipped, "no_gain"
		if err := c.discard(ctx, j.Temp); err != nil {
			return err
		}
		j.Temp = ""
	} else {
		j.State = core.CompressionReady
	}
	if err := c.Store.ExportAllowed(ctx, j.AssetID, j.To); err != nil {
		return err
	}
	return c.Store.SetCompressionJob(ctx, *j)
}

// RestorePlan prepares a new JPEG copy from one published, indexed reversible
// output. Its digest is checked against the complete original, never re-encoded.
func (c *Coordinator) RestorePlan(ctx context.Context, runID, jobID int64, directory string) (int64, error) {
	run, err := c.Store.CompressionRun(ctx, runID)
	if err != nil {
		return 0, err
	}
	if run.Options.Mode != core.CompressReversible {
		return 0, ErrPlan
	}
	old, err := c.Store.VisibleCompressionJob(ctx, runID, jobID)
	if err != nil {
		return 0, err
	}
	if old.State != core.CompressionDone {
		return 0, store.ErrNotFound
	}
	assetID := old.OutputAssetID
	if assetID == 0 {
		assetID = old.AssetID
	}
	if _, err := c.Store.AssetFile(ctx, assetID, false); err != nil {
		return 0, err
	}
	digest, size, err := fileops.FileDigest(ctx, old.To)
	if err != nil {
		return 0, err
	}
	if digest != old.OutputDigest || size != old.OutputSize {
		return 0, fileops.ErrSourceChanged
	}
	if directory == "" {
		directory = filepath.Dir(old.To)
	}
	if !filepath.IsAbs(directory) {
		return 0, ErrPlan
	}
	to, err := c.freeName(ctx, directory, strings.TrimSuffix(filepath.Base(old.From), filepath.Ext(old.From))+"-restored"+filepath.Ext(old.From), make(map[string]bool))
	if err != nil {
		return 0, err
	}
	if err := c.Store.ExportAllowed(ctx, assetID, to); err != nil {
		return 0, err
	}
	info, err := os.Stat(old.To)
	if err != nil {
		return 0, err
	}
	options := core.DefaultCompressionOptions()
	options.Mode = core.CompressRestore
	return c.Store.SaveCompressionPlan(ctx, options, []core.CompressionJob{{AssetID: assetID, From: old.To, To: to, Copies: 1, Size: size, MTime: info.ModTime().UnixNano(), State: core.CompressionPlanned, RestoreDigest: old.Digest, RestoreSize: old.Size}})
}

func (c *Coordinator) freeName(ctx context.Context, dir, name string, reserved map[string]bool) (string, error) {
	// Missing indexed copies still own their path; a new result must not
	// reconnect unrelated content to their annotations on the next scan.
	for range 10000 {
		path, err := fileops.FreeName(ctx, dir, name, reserved)
		if err != nil {
			return "", err
		}
		known, err := c.Store.KnownDestinations(ctx, []string{path})
		if err != nil {
			return "", err
		}
		if !known[core.Fold(path)] {
			return path, nil
		}
		reserved[core.Fold(path)] = true
	}
	return "", fileops.ErrExists
}

func snapshot(ctx context.Context, in io.Reader, path string) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	buf := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := in.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return out.Sync()
		}
		if err != nil {
			return err
		}
	}
}

// Commit publishes only selected ready rows. Source changes require a new review.
func (c *Coordinator) Commit(ctx context.Context, runID int64, ids []int64) error {
	jobs, err := c.Store.CompressionJobs(ctx, runID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return ErrPlan
	}
	seen := make(map[int64]bool)
	ready := make(map[int64]bool, len(jobs))
	for _, job := range jobs {
		if job.State == core.CompressionReady {
			ready[job.ID] = true
		}
	}
	for _, id := range ids {
		if seen[id] || !ready[id] {
			return ErrPlan
		}
		seen[id] = true
	}
	publisher := fileops.CompressionPublisher{Store: c.Store}
	for _, j := range jobs {
		if !seen[j.ID] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		err := c.publish(ctx, &publisher, j)
		if err != nil {
			fresh, readErr := c.Store.CompressionJob(context.WithoutCancel(ctx), j.ID)
			if readErr != nil {
				return readErr
			}
			if fresh.State == core.CompressionCommitting {
				if settleErr := c.recoverPublication(context.WithoutCancel(ctx), fresh, reason(err)); settleErr != nil {
					return errors.Join(err, settleErr)
				}
				if err := c.cleanDone(context.WithoutCancel(ctx), runID, j.ID); err != nil {
					return err
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
			j.State, j.Reason = core.CompressionFailed, reason(err)
			if err := c.Store.SetCompressionJob(context.WithoutCancel(ctx), j); err != nil {
				return err
			}
		} else if err := c.cleanDone(context.WithoutCancel(ctx), runID, j.ID); err != nil {
			return err
		}
	}
	return c.Store.SetCompressionRun(context.WithoutCancel(ctx), runID, core.CompressionDone)
}

func (c *Coordinator) cleanDone(ctx context.Context, runID, jobID int64) error {
	j, err := c.Store.CompressionJob(ctx, jobID)
	if err != nil {
		return err
	}
	if j.RunID != runID {
		return ErrPlan
	}
	if j.State == core.CompressionDone && j.Temp != "" {
		if err := c.discard(ctx, j.Temp); err != nil {
			return err
		}
		j.Temp = ""
		return c.Store.SetCompressionJob(ctx, j)
	}
	return nil
}

func (c *Coordinator) publish(ctx context.Context, publisher *fileops.CompressionPublisher, j core.CompressionJob) error {
	if c.Admit != nil {
		workCtx, release, err := c.Admit(ctx)
		if err != nil {
			return err
		}
		defer release()
		ctx = workCtx
	}
	return publisher.Commit(ctx, j)
}

// Recover makes interrupted encoding retryable and settles committed bytes.
// Ready candidates remain ready, requiring the user's review after restart.
// One row that cannot be settled now (its drive is away, say) does not hold
// up the others: they are all tried, and what failed is returned together.
func (c *Coordinator) Recover(ctx context.Context) error {
	jobs, err := c.Store.CompressionRecoveryJobs(ctx)
	if err != nil {
		return err
	}
	var failed []error
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.recoverOne(ctx, j); err != nil {
			failed = append(failed, fmt.Errorf("compression job %d: %w", j.ID, err))
		}
	}
	if err := c.prune(ctx); err != nil {
		failed = append(failed, fmt.Errorf("prune compression history: %w", err))
	}
	return errors.Join(failed...)
}

func (c *Coordinator) recoverOne(ctx context.Context, j core.CompressionJob) error {
	switch j.State {
	case core.CompressionEncoding, core.CompressionVerifying:
		if err := c.discard(ctx, j.Temp); err != nil {
			return err
		}
		j.Temp, j.State, j.Reason = "", core.CompressionPlanned, "interrupted"
		if err := c.Store.SetCompressionJob(ctx, j); err != nil {
			return err
		}
	case core.CompressionCommitting:
		if err := c.recoverPublication(ctx, j, "interrupted"); err != nil {
			return err
		}
		if err := c.cleanDone(ctx, j.RunID, j.ID); err != nil {
			return err
		}
	default: // done, with its candidate still to remove
		return c.cleanDone(ctx, j.RunID, j.ID)
	}
	return c.Store.SetCompressionRun(ctx, j.RunID, core.CompressionPaused)
}

// prune discards the candidates of runs older than history reaches, then
// their rows: otherwise reviews never saved or cancelled would keep their
// files for good, with no screen left to discard them from.
func (c *Coordinator) prune(ctx context.Context) error {
	candidates, err := c.Store.ExpiredCompressionCandidates(ctx)
	if err != nil {
		return err
	}
	for _, path := range candidates {
		if err := c.discard(ctx, path); err != nil {
			return err
		}
	}
	return c.Store.PruneCompressionRuns(ctx)
}

func (c *Coordinator) recoverPublication(ctx context.Context, j core.CompressionJob, failure string) error {
	p := fileops.CompressionPublisher{Store: c.Store}
	if err := p.Settle(ctx, j); err == nil {
		return nil
	} else if !errors.Is(err, fileops.ErrSourceChanged) && !errors.Is(err, fileops.ErrExists) && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if j.PublishTemp != "" && filepath.Dir(j.PublishTemp) == filepath.Dir(j.To) && strings.HasPrefix(filepath.Base(j.PublishTemp), ".lumen-compress-") {
		if err := os.Remove(j.PublishTemp); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	j.State, j.Reason, j.PublishTemp = core.CompressionFailed, failure, ""
	return c.Store.SetCompressionJob(ctx, j)
}

func (c *Coordinator) discard(ctx context.Context, path string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if filepath.Dir(dir) != filepath.Clean(c.Root) || !strings.HasPrefix(filepath.Base(dir), "run-") {
		return ErrPlan
	}
	release, err := fileops.LeaseCopyFolders(ctx, c.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	return os.RemoveAll(dir)
}

// Cancel discards only uncommitted owned files and records cancellation.
func (c *Coordinator) Cancel(ctx context.Context, runID int64) error {
	jobs, err := c.Store.CompressionJobs(ctx, runID)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.State == core.CompressionDone || j.State == core.CompressionSkipped || j.State == core.CompressionCommitting {
			continue
		}
		if err := c.discard(ctx, j.Temp); err != nil {
			return err
		}
		j.Temp, j.State, j.Reason = "", core.CompressionCancelled, ""
		if err := c.Store.SetCompressionJob(ctx, j); err != nil {
			return err
		}
	}
	return c.Store.SetCompressionRun(ctx, runID, core.CompressionCancelled)
}

func reason(err error) string {
	switch {
	case errors.Is(err, ErrUnsupported):
		return "unsupported"
	case errors.Is(err, ErrPreservation):
		return "preservation"
	case errors.Is(err, fileops.ErrSourceChanged):
		return "changed"
	case errors.Is(err, fileops.ErrExists):
		return "taken"
	case errors.Is(err, fileops.ErrDestinationFolder):
		return "links"
	case errors.Is(err, fileops.ErrCopySpace):
		return "space"
	case errors.Is(err, store.ErrNotFound):
		return "not_visible"
	case errors.Is(err, os.ErrNotExist):
		return "missing"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "codec"
	}
}

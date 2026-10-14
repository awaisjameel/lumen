package scanner

import (
	"context"
	"log/slog"

	"lumen/internal/core"
)

// hashWorkers keeps explicit overrides for callers measuring scan throughput.
// Unknown drives retain the existing concurrency rather than guessing their type.
func (s *Scanner) hashWorkers(ctx context.Context, root string) int {
	if s.HashWorkers > 0 {
		return s.HashWorkers
	}
	probe := s.seekPenalty
	if probe == nil {
		probe = driveSeekPenalty
	}
	seeks, err := probe(ctx, root)
	if err != nil {
		if ctx.Err() == nil {
			slog.Debug("query drive seek penalty", "path", root, "err", err)
		}
		return 4
	}
	if seeks {
		return 1
	}
	threads := core.DefaultCPUThreads()
	if s.Store != nil {
		n, err := s.Store.CPUThreads(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Debug("read fingerprint thread budget", "err", err)
			}
			return 4
		}
		threads = n
	}
	// Eight readers improved cold NVMe scans; higher counts varied under contention.
	return min(8, max(1, threads), core.MaxCPUThreads())
}

package index

import (
	"context"

	"lumen/internal/core"
)

// AcquireCompression shares the stages' CPU budget and politeness holds with
// foreground compression. No scan is held while a candidate is encoded.
func (ix *Indexer) AcquireCompression(ctx context.Context, threads int) (workCtx context.Context, release func(), granted int, err error) {
	if !ix.waitTurn(ctx, core.StageThumbnail, nil) {
		return ctx, nil, 0, ctx.Err()
	}
	work, release, err := ix.cpu.acquire(ctx, threads)
	if err != nil {
		return ctx, nil, 0, err
	}
	held, ok := work.Value(holdingKey{}).(int)
	if !ok {
		release()
		return ctx, nil, 0, context.Canceled
	}
	return work, release, held, nil
}

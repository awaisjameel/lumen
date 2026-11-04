package index

import (
	"context"
	"sync"
)

// passState serialises a background pass and its explicit callers, with one
// coalesced wake-up. OCR scheduling and XMP export each own a separate state.
// The zero value is ready, including in indexers used without Start.
type passState struct {
	once sync.Once
	turn chan struct{}
	wake chan struct{}
}

func (p *passState) init() {
	p.once.Do(func() {
		p.turn = make(chan struct{}, 1)
		p.wake = make(chan struct{}, 1)
	})
}

func (p *passState) wakeCh() chan struct{} {
	p.init()
	return p.wake
}

// acquire waits for the pass or its caller's cancellation. A pass releases
// its turn before waiting for work or for a pause to lift, so explicit work
// never waits behind a sleeping background owner.
func (p *passState) acquire(ctx context.Context) error {
	p.init()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case p.turn <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *passState) release() { <-p.turn }

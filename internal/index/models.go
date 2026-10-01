package index

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// model is an AI model the indexer loads when its stage has work and frees
// once the stage has had none for idle: models hold hundreds of MB, and
// loading one takes a second or two, so short gaps keep it.
type model[T any] struct {
	name  string
	idle  time.Duration
	open  func() (T, error)
	close func(T)
	// report is told about every load (err set if it failed) and every
	// unload (loaded false), so the progress can show the AI's state.
	report func(v T, loaded bool, err error)

	once   sync.Once
	use    chan struct{} // one batch or release at a time; made by once
	mu     sync.Mutex    // guards v, loaded and used
	v      T
	loaded bool
	used   time.Time
}

func (m *model[T]) lock(ctx context.Context) error {
	m.once.Do(func() { m.use = make(chan struct{}, 1) })
	select {
	case m.use <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-m.use
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *model[T]) acquire(ctx context.Context) (value T, release func(), err error) {
	if err := m.lock(ctx); err != nil {
		return value, func() {}, err
	}
	value, err = m.get()
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		<-m.use
		return value, func() {}, err
	}
	return value, func() {
		m.mu.Lock()
		m.used = time.Now()
		m.mu.Unlock()
		<-m.use
	}, nil
}

// get returns the model, loading it if needed.
func (m *model[T]) get() (T, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.used = time.Now()
	if m.loaded {
		return m.v, nil
	}
	t := time.Now()
	v, err := m.open()
	m.report(v, err == nil, err)
	if err != nil {
		return v, fmt.Errorf("load %s: %w", m.name, err)
	}
	slog.Info("model loaded", "model", m.name, "took", time.Since(t).Round(time.Millisecond))
	m.v, m.loaded = v, true
	return v, nil
}

// releaseIdle frees the model if it has had no work for a while. Stages
// call it whenever their queue is empty.
func (m *model[T]) releaseIdle() {
	_ = m.lock(context.Background()) // waits for a batch in flight; never fails
	defer func() { <-m.use }()
	m.mu.Lock()
	idle := time.Since(m.used) >= m.idle
	m.mu.Unlock()
	if idle {
		m.releaseLocked()
	}
}

// release frees the model now: its stage is idle, held (paused, on
// battery) or stopping, or the model failed and is loaded afresh next time.
func (m *model[T]) release() {
	_ = m.lock(context.Background()) // waits for a batch in flight; never fails
	defer func() { <-m.use }()
	m.releaseLocked()
}

func (m *model[T]) releaseLocked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return
	}
	m.close(m.v)
	var zero T
	m.v, m.loaded = zero, false
	m.report(zero, false, nil)
	slog.Info("model unloaded", "model", m.name)
}

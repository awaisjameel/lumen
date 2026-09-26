package index

import (
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

	mu     sync.Mutex
	v      T
	loaded bool
	used   time.Time
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
		return v, err
	}
	slog.Info(m.name+" loaded", "took", time.Since(t).Round(time.Millisecond))
	m.v, m.loaded = v, true
	return v, nil
}

// releaseIdle frees the model if it has had no work for a while. Stages
// call it whenever their queue is empty.
func (m *model[T]) releaseIdle() {
	m.mu.Lock()
	idle := time.Since(m.used) >= m.idle
	m.mu.Unlock()
	if idle {
		m.release()
	}
}

// release frees the model now.
func (m *model[T]) release() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return
	}
	m.close(m.v)
	var zero T
	m.v, m.loaded = zero, false
	m.report(zero, false, nil)
	slog.Info(m.name + " unloaded (no work)")
}

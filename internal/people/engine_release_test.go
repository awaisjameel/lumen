package people

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/ai"
	"lumen/internal/store"
)

func TestTheEngineGivesItsKeptStateBackOnceTheLibraryRests(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	held := func(idle time.Duration) bool {
		e := NewEngine(st, ai.FaceModel{Version: "test"})
		e.idle = idle
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			e.Run(ctx, func() {})
		}()
		// The first run follows the quiet period; the rest follows the run.
		time.Sleep(quiet + 3*time.Second)
		cancel()
		<-done // the state belongs to Run's goroutine until it has returned
		return e.state != nil
	}
	if !held(time.Hour) {
		t.Fatal("the engine kept no state after a run, so this test would prove nothing")
	}
	if held(time.Millisecond) {
		t.Fatal("the kept state outlived the idle time")
	}
}

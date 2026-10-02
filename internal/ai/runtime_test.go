package ai

import (
	"testing"
	"time"
)

func TestGPURestsAfterEachRun(t *testing.T) {
	var g gpuGate
	const work = 60 * time.Millisecond
	rest := time.Duration(gpuRest * float64(work))

	g.run(func() { time.Sleep(work) })
	// Resting: a face run goes to the CPU instead.
	if g.try(func() {}) {
		t.Fatal("the GPU ran again straight after a run")
	}
	// The image model waits out the rest.
	start := time.Now()
	g.run(func() {})
	if waited := time.Since(start); waited < rest*3/4 {
		t.Fatalf("the next run waited %v, want about %v", waited, rest)
	}
	// A run that took no time leaves no rest.
	if !g.try(func() {}) {
		t.Fatal("the GPU stayed resting after a run that took no time")
	}
}

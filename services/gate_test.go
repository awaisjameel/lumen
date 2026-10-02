package services

import (
	"context"
	"testing"
	"time"
)

func TestLockingTheGateEndsEveryRequestHeldOpenByIt(t *testing.T) {
	g := &BlockGate{}
	g.open(time.Now())
	a, releaseA, okA := g.Hold(t.Context())
	b, releaseB, okB := g.Hold(t.Context())
	defer releaseA()
	defer releaseB()
	if !okA || !okB || a.Err() != nil || b.Err() != nil {
		t.Fatalf("holds on an open gate: %v %v, errors %v %v", okA, okB, a.Err(), b.Err())
	}
	g.close()
	if a.Err() == nil || b.Err() == nil {
		t.Fatal("a request survived the lock")
	}
	if g.Unlocked() {
		t.Fatal("the gate is still open after close")
	}
}

func TestAHoldOnALockedGateIsRefused(t *testing.T) {
	g := &BlockGate{}
	ctx, release, ok := g.Hold(t.Context())
	defer release()
	if ok {
		t.Fatal("a locked gate was held open")
	}
	if ctx != t.Context() {
		t.Fatal("a refused hold changed the context")
	}
}

func TestAHoldOnAnIdleGateLocksItInsteadOfRefreshingIt(t *testing.T) {
	now := time.Now()
	g := &BlockGate{clock: func() time.Time { return now }}
	g.open(now)
	now = now.Add(blockUnlockIdle + time.Second)
	if _, release, ok := g.Hold(t.Context()); ok {
		release()
		t.Fatal("an idle gate was held open")
	}
	if g.Unlocked() {
		t.Fatal("the idle gate stayed open")
	}
}

func TestAResponseStillBeingSentKeepsTheGateFromIdling(t *testing.T) {
	now := time.Now()
	g := &BlockGate{clock: func() time.Time { return now }}
	g.open(now)
	_, release, _ := g.Hold(t.Context())
	now = now.Add(3 * blockUnlockIdle) // a long video, nothing else asked of the gate
	if !g.Unlocked() {
		t.Fatal("the gate idled out under a response still being sent")
	}
	now = now.Add(blockUnlockIdle - time.Second)
	release()
	now = now.Add(blockUnlockIdle - time.Second)
	if !g.Unlocked() {
		t.Fatal("the end of the response did not count as a use")
	}
	now = now.Add(blockUnlockIdle + time.Second)
	if g.Unlocked() {
		t.Fatal("the gate never idled out once nothing was held")
	}
}

func TestReleasingAHoldAfterTheLockIsHarmless(t *testing.T) {
	g := &BlockGate{}
	g.open(time.Now())
	_, release, _ := g.Hold(context.Background())
	g.close()
	release()
	release()
	if g.Unlocked() {
		t.Fatal("releasing reopened the gate")
	}
}

package gitleaks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zricethezav/gitleaks/v8/detect"
)

// blockingCheckoutCtx is a minimal context.Context whose Done() closes reached
// the second time it is observed. Scan checks ctx.Done() once in its upfront
// guard and once when it enters the blocking detector checkout select, so the
// second observation is a deterministic, sleep-free signal that the caller has
// committed to waiting on a detector — the exact state this test must reach
// before it cancels. Coupling to the call count keeps the test honest: if the
// checkout ever stopped observing ctx, reached would never close and the test
// would fail on its timeout rather than silently passing. It implements the
// interface directly (rather than wrapping a context) to avoid holding a
// context.Context in a struct field.
type blockingCheckoutCtx struct {
	mu       sync.Mutex
	calls    int
	canceled bool
	reached  chan struct{}
	done     chan struct{}
}

func (c *blockingCheckoutCtx) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *blockingCheckoutCtx) Value(any) any { return nil }

func (c *blockingCheckoutCtx) Done() <-chan struct{} {
	c.mu.Lock()
	c.calls++
	if c.calls == 2 {
		close(c.reached)
	}
	c.mu.Unlock()
	return c.done
}

func (c *blockingCheckoutCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.canceled {
		return context.Canceled
	}
	return nil
}

func (c *blockingCheckoutCtx) cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.canceled {
		c.canceled = true
		close(c.done)
	}
}

// TestScanCancelWhileWaiting verifies that Scan returns promptly when its
// context is canceled while it is blocked waiting for a detector — i.e. when
// concurrency is saturated and every slot is checked out. Before the checkout
// was made context-aware, a parked caller would stay blocked here indefinitely
// (until an unrelated Scan returned a detector), ignoring cancellation.
func TestScanCancelWhileWaiting(t *testing.T) {
	t.Parallel()

	s := NewScanner()

	// Drain every slot so the warm set is empty; the next checkout must block.
	held := make([]*detect.Detector, 0, cap(s.detectors))
	for range cap(s.detectors) {
		held = append(held, <-s.detectors)
	}

	ctx := &blockingCheckoutCtx{reached: make(chan struct{}), done: make(chan struct{})}

	done := make(chan error, 1)
	go func() {
		_, err := s.Scan(ctx, "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7REALKEY")
		done <- err
	}()

	// Wait until Scan has parked on the blocking checkout, then cancel. With
	// every slot drained it cannot proceed except via cancellation, so this
	// exercises the context-aware receive rather than the upfront guard.
	select {
	case <-ctx.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("Scan never reached the blocking detector checkout")
	}
	ctx.cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Scan did not return after its context was canceled while waiting for a detector")
	}

	// Cancellation must not have consumed a slot; the semaphore is intact.
	for _, d := range held {
		s.detectors <- d
	}
}

package identitychaining

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type observerFunc func(context.Context, Observation) error

func (f observerFunc) ObserveAttempt(ctx context.Context, o Observation) error {
	return f(ctx, o)
}

func TestObserverRegistration(t *testing.T) {
	var c Chainer
	var calls int
	want := Observation{Resource: "https://resource.example", StartedAt: time.Now()}
	observe := func() { c.observe(context.Background(), slog.Default(), want) }
	observe() // A zero-value chainer has no observer.
	c.SetObserver(observerFunc(func(_ context.Context, got Observation) error {
		calls++
		if got != want {
			t.Errorf("observation = %+v, want %+v", got, want)
		}
		return nil
	}))
	observe()
	c.SetObserver(nil)
	observe()
	if calls != 1 {
		t.Fatalf("observer calls = %d, want 1", calls)
	}
}

func TestObserverCanReplaceItself(t *testing.T) {
	var c Chainer
	var calls atomic.Int64
	replacement := observerFunc(func(context.Context, Observation) error {
		calls.Add(1)
		return nil
	})
	c.SetObserver(observerFunc(func(context.Context, Observation) error {
		c.SetObserver(replacement) // Must not hold the observer lock during invocation.
		return nil
	}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.observe(context.Background(), slog.Default(), Observation{})
		c.observe(context.Background(), slog.Default(), Observation{})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("observer callback blocked while replacing itself")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("replacement calls = %d, want 1", got)
	}
}

func TestObserverConcurrentSetAndObserve(t *testing.T) {
	var c Chainer
	var calls atomic.Int64
	observer := observerFunc(func(context.Context, Observation) error {
		calls.Add(1)
		return nil
	})
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			<-start
			for range 1000 {
				if worker%2 == 0 {
					c.SetObserver(observer)
					c.SetObserver(nil)
				} else {
					c.observe(context.Background(), slog.Default(), Observation{})
				}
			}
		})
	}
	close(start)
	wg.Wait()
	c.SetObserver(observer)
	c.observe(context.Background(), slog.Default(), Observation{})
	if calls.Load() == 0 {
		t.Fatal("observer was never called")
	}
}

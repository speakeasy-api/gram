package sc

import (
	"context"
	"iter"
	"sync"
	"time"
)

// inbox is an unbounded queue of events on their way to one reader. Putting
// never blocks, so the goroutine processing a machine's events can hand one
// over while the reader is busy.
type inbox struct {
	mu    sync.Mutex
	queue []any

	// wake holds a token whenever the queue may be non-empty.
	wake chan struct{}
}

func newInbox() *inbox {
	return &inbox{mu: sync.Mutex{}, queue: nil, wake: make(chan struct{}, 1)}
}

func (b *inbox) put(ev any) {
	b.mu.Lock()
	b.queue = append(b.queue, ev)
	b.mu.Unlock()

	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// events yields the queued events in order, waiting for more, until ctx
// ends.
func (b *inbox) events(ctx context.Context) iter.Seq[any] {
	return func(yield func(any) bool) {
		for {
			b.mu.Lock()
			queued := b.queue
			b.queue = nil
			b.mu.Unlock()

			for _, ev := range queued {
				if !yield(ev) {
					return
				}
			}

			select {
			case <-b.wake:
			case <-ctx.Done():
				return
			}
		}
	}
}

// serviceInbox is the context key under which a driver gives a service run
// its inbox, and driverClock the one under which it gives the clock.
type (
	serviceInbox struct{}
	driverClock  struct{}
)

// serviceContext returns ctx carrying what a driver gives each service run:
// the inbox its chart can send to, and the driver's clock.
func serviceContext(ctx context.Context, box *inbox, now func() time.Time) context.Context {
	return context.WithValue(context.WithValue(ctx, serviceInbox{}, box), driverClock{}, now)
}

// Inbox returns the events the chart sends to the running service with
// [Transition.SendTo], in order, waiting for more until ctx ends. A service
// ranges over it, usually on a goroutine of its own beside its work:
//
//	for ev := range sc.Inbox(ctx) { ... }
//
// ctx is the context the service was given. Under a driver that cannot
// deliver events to a service, the sequence is empty.
func Inbox(ctx context.Context) iter.Seq[any] {
	box, ok := ctx.Value(serviceInbox{}).(*inbox)
	if !ok {
		return func(func(any) bool) {}
	}

	return box.events(ctx)
}

// clockFrom returns the clock of the driver running the service that was
// given ctx, or the wall clock.
func clockFrom(ctx context.Context) func() time.Time {
	if now, ok := ctx.Value(driverClock{}).(func() time.Time); ok && now != nil {
		return now
	}

	return time.Now
}

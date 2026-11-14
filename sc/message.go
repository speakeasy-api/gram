package sc

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"
)

// MaxInbox is how many events sent to a service may wait unread. A service
// that falls further behind is stopped and fails with [ErrInboxFull].
const MaxInbox = 1024

// ErrInboxFull is the error of a [Failed] event for a service that was
// stopped because [MaxInbox] events sent to it were waiting unread.
var ErrInboxFull = errors.New("service fell too far behind the events sent to it")

// inbox is a queue of events on their way to one reader. Putting never
// blocks, so the goroutine processing a machine's events can hand one over
// while the reader is busy; instead a put fails once limit events wait. A
// limit of zero is none, for a queue whose producer is bounded elsewhere.
type inbox struct {
	mu    sync.Mutex
	queue []any
	limit int

	// wake holds a token whenever the queue may be non-empty.
	wake chan struct{}
}

func newInbox(limit int) *inbox {
	return &inbox{mu: sync.Mutex{}, queue: nil, limit: limit, wake: make(chan struct{}, 1)}
}

// put queues ev, and reports false when the queue is full.
func (b *inbox) put(ev any) bool {
	b.mu.Lock()
	if b.limit > 0 && len(b.queue) >= b.limit {
		b.mu.Unlock()
		return false
	}
	b.queue = append(b.queue, ev)
	b.mu.Unlock()

	select {
	case b.wake <- struct{}{}:
	default:
	}

	return true
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
				// What was sent by the step that ended the reader is
				// still handed over, so a service hears the last thing
				// it was told before it stops.
				b.mu.Lock()
				queued := b.queue
				b.queue = nil
				b.mu.Unlock()

				for _, ev := range queued {
					if !yield(ev) {
						return
					}
				}

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
func serviceContext(ctx context.Context, box *inbox, now func() time.Time, manual bool) context.Context {
	ctx = context.WithValue(ctx, serviceInbox{}, box)
	ctx = context.WithValue(ctx, driverClock{}, now)

	return context.WithValue(ctx, manualClock{}, manual)
}

// manualClock is the context key under which a driver says whether its
// timers are driven by the caller, and advance is what it puts in a
// service's inbox when the caller moves time on.
type (
	manualClock struct{}
	advance     struct{}
)

// listen hands an emitted event to a driver's listener. A listener that
// panics fails like an action: the step it belongs to stands.
func listen[C any](ctx context.Context, fn func(context.Context, any), e Effect[C]) (err error) {
	if fn == nil {
		return nil
	}

	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%w: listener for emit %q in state %q: %v", ErrPanicked, e.Name, e.State, p)
		}
	}()

	fn(ctx, e.Event)

	return nil
}

// clockFrom returns the clock of the driver running the service that was
// given ctx, or the wall clock.
func clockFrom(ctx context.Context) func() time.Time {
	if now, ok := ctx.Value(driverClock{}).(func() time.Time); ok && now != nil {
		return now
	}

	return time.Now
}

// savedProgress is the context key under which a service run finds the
// progress it starts from.
type savedProgress struct{}

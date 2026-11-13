package sc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// savedProgress is the context key under which a service run finds the
// progress it starts from, and progressRecorder the one under which it finds
// how to record more.
type (
	savedProgress    struct{}
	progressRecorder struct{}
)

// ErrNoCheckpoint is returned by [Checkpoint] when the context is not one a
// service was given.
var ErrNoCheckpoint = errors.New("context does not belong to a service run")

// Checkpoint records v, encoded as JSON, as the progress of the service run
// that was given ctx: a cursor, the pages fetched so far, the step it
// reached. The progress is kept in the snapshot, so when the instance is
// stored and the service is started again after a restart, the new run
// reads it with [Resumed] and carries on instead of starting over. Each
// call replaces the last and counts as a step of the instance.
//
// Call it from the service function itself, on the goroutine the function
// was called on. A run started by [Invocation.Retry] after a failure starts
// from the failed run's progress.
func Checkpoint(ctx context.Context, v any) error {
	record, ok := ctx.Value(progressRecorder{}).(func(json.RawMessage))
	if !ok {
		return ErrNoCheckpoint
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	record(data)

	return nil
}

// Resumed decodes into v the progress an earlier run of the service
// recorded with [Checkpoint], and reports whether there was any. ctx is the
// context the service was given.
func Resumed(ctx context.Context, v any) (bool, error) {
	data, _ := ctx.Value(savedProgress{}).(json.RawMessage)
	if len(data) == 0 {
		return false, nil
	}

	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("unmarshal checkpoint: %w", err)
	}

	return true, nil
}

// recording wraps yield for a service function: the returned context lets
// the function record progress through it, and the returned yield stops
// calling the driver once the driver has stopped listening.
func recording(ctx context.Context, id string, yield func(any) bool) (context.Context, func(any)) {
	stopped := false
	emit := func(ev any) {
		if !stopped && !yield(ev) {
			stopped = true
		}
	}

	record := func(data json.RawMessage) { emit(Checkpointed{ID: id, Data: data}) }

	return context.WithValue(ctx, progressRecorder{}, record), emit
}

// runID is the context key under which a service run finds its ID.
type runID struct{}

// RunID returns the ID of the service run that was given ctx, or "" for any
// other context. The ID is unique within its instance and stays the same
// when a driver runs the same effect again after a crash, which makes it,
// together with the instance's own identity, a ready idempotency key for
// the call the service makes. A run started by entering the state again, or
// by [Invocation.Retry], has a new ID.
func RunID(ctx context.Context) string {
	id, _ := ctx.Value(runID{}).(string)
	return id
}

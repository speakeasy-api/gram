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
// deliver events to a service, the sequence is empty. A service that lets
// [MaxInbox] events wait unread is stopped and fails with [ErrInboxFull].
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

// trigger is the context key under which a service run finds the event it
// was started for.
type trigger struct{}

// Trigger returns the event that was being processed when the state of the
// service given ctx was entered: the event of the transition into it, so a
// service can act on what it carries without the chart copying it into the
// context first. It is nil when the state was entered as the instance
// started or by a timer, and for an event of a type no On in the chart
// names. The event is kept with the run, so a run started again after a
// restart, or by [Invocation.Retry], sees the same one.
func Trigger(ctx context.Context) any {
	return ctx.Value(trigger{})
}

package sc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
)

// Run is one run of a service, handed to the service function beside its
// context. It is how a service reaches what the driver keeps for it: its ID,
// the progress it recorded on an earlier run, and the resources of the
// states around it. The context is left for cancellation, deadlines and
// tracing.
//
// A Run is only valid for the call it was given to, and must be used from
// the goroutine the service function was called on.
type Run struct {
	id string

	// saved is the progress an earlier run recorded, and record how to
	// record more; nil outside a driver.
	saved  json.RawMessage
	record func(json.RawMessage)

	// lookup finds the value of a held resource by its key; nil under a
	// driver that holds none.
	lookup func(key string) (any, bool)
}

// ErrNoCheckpoint is returned by [Run.Checkpoint] for a Run that no driver
// handed out, such as a zero Run in a test.
var ErrNoCheckpoint = errors.New("run does not belong to a driver")

// ID returns the run's ID. It is unique within its instance and stays the
// same when a driver runs the same effect again after a crash, which makes
// it, together with the instance's own identity, a ready idempotency key for
// the call the service makes. A run started by entering the state again, or
// by [Invocation.Retry], has a new ID.
func (r *Run) ID() string { return r.id }

// Key returns the key a [Transition.Spawn] gave the run, and reports whether
// it is a spawned run at all.
func (r *Run) Key() (string, bool) { return SpawnKey(r.id) }

// Checkpoint records v, encoded as JSON, as the run's progress: a cursor,
// the pages fetched so far, the step it reached. The progress is kept in the
// snapshot, so when the instance is stored and the service is started again
// after a restart, the new run reads it with [Run.Resumed] and carries on
// instead of starting over. Each call replaces the last and counts as a step
// of the instance. A run started by [Invocation.Retry] after a failure starts
// from the failed run's progress.
func (r *Run) Checkpoint(v any) error {
	if r == nil || r.record == nil {
		return ErrNoCheckpoint
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	r.record(data)

	return nil
}

// Resumed decodes into v the progress an earlier run of the service recorded
// with [Run.Checkpoint], and reports whether there was any.
func (r *Run) Resumed(v any) (bool, error) {
	if r == nil || len(r.saved) == 0 {
		return false, nil
	}

	if err := json.Unmarshal(r.saved, v); err != nil {
		return false, fmt.Errorf("unmarshal checkpoint: %w", err)
	}

	return true, nil
}

// Held returns the resource's value for the service or acquisition that was
// given run. It reports false when the resource is not held: before it is
// acquired, after its state is left, or under a driver that holds no
// resources.
func (r *Resource[S, C, R]) Held(run *Run) (R, bool) {
	var zero R

	if run == nil || run.lookup == nil {
		return zero, false
	}

	return typedValue[R](run.lookup(r.inv.resource.key))
}

// From returns the resource's value from the context an action was given,
// as [Resource.Held] does for a service. Actions have no [Run], so this is
// how they reach a resource of their state.
func (r *Resource[S, C, R]) From(ctx context.Context) (R, bool) {
	var zero R

	lookup, ok := ctx.Value(heldResources{}).(func(key string) (any, bool))
	if !ok {
		return zero, false
	}

	return typedValue[R](lookup(r.inv.resource.key))
}

func typedValue[R any](value any, ok bool) (R, bool) {
	typed, isR := value.(R)

	return typed, ok && isR
}

// heldResources is the context key under which a driver exposes the
// resources it holds.
type heldResources struct{}

// startRun builds the Run for a service function under a driver, from what
// the driver put in ctx, and wraps yield so that the function's checkpoints
// and results reach the driver until the driver stops listening.
func startRun(ctx context.Context, id string, yield func(any) bool) (*Run, func(any)) {
	stopped := false
	emit := func(ev any) {
		if !stopped && !yield(ev) {
			stopped = true
		}
	}

	saved, _ := ctx.Value(savedProgress{}).(json.RawMessage)
	lookup, _ := ctx.Value(heldResources{}).(func(key string) (any, bool))

	run := &Run{
		id:     id,
		saved:  saved,
		record: func(data json.RawMessage) { emit(Checkpointed{ID: id, Data: data}) },
		lookup: lookup,
	}

	return run, emit
}

// Port names a service that receives messages of type M. It is what the
// handle returned by [StateBuilder.Receive], [StateBuilder.ReceiveStream],
// [StateBuilder.InvokeMachine] and [StateBuilder.SpawnMachine] carries, and
// what [Transition.SendTo] and the other sends take.
type Port[M any] struct {
	name string
}

func (p Port[M]) port() Port[M] { return p }

// Mailbox is a service that receives messages of type M: a handle a
// receiving service's declaration returned. Sends take a Mailbox, so the
// message they build must be of the type the service reads.
type Mailbox[M any] interface {
	port() Port[M]
}

// Receiving is the handle of a service that receives messages of type M. It
// routes the service's outcome, as an [Invocation] does, and is the
// [Mailbox] sends address.
type Receiving[S comparable, C, M, R any] struct {
	*Invocation[S, C, R]
	Port[M]
}

// messages returns what the chart sends the run, as messages of type M, in
// order, waiting for more until ctx ends.
func messages[M any](ctx context.Context) iter.Seq[M] {
	box, ok := ctx.Value(serviceInbox{}).(*inbox)
	if !ok {
		return func(func(M) bool) {}
	}

	return func(yield func(M) bool) {
		for ev := range box.events(ctx) {
			// What the driver tells a child machine about time, and
			// anything not meant for this inbox, is not the service's
			// to see.
			msg, ok := ev.(M)
			if !ok {
				continue
			}

			if !yield(msg) {
				return
			}
		}
	}
}

// triggerOf returns the event the run's state was entered with, as an Ev.
func triggerOf[Ev any](ctx context.Context) Ev {
	ev, _ := ctx.Value(trigger{}).(Ev)
	return ev
}

// trigger is the context key under which a service run finds the event its
// state was entered with.
type trigger struct{}

// adaptService adapts a service function to the form the core runs.
func adaptService[C, R any](fn func(context.Context, *Run, C) (R, error)) func(context.Context, C, string) iter.Seq[any] {
	return func(ctx context.Context, c C, id string) iter.Seq[any] {
		return func(yield func(any) bool) {
			run, emit := startRun(ctx, id, yield)

			res, err := fn(ctx, run, c)
			if err != nil {
				emit(Failed{ID: id, Err: err})
				return
			}

			emit(Done[R]{ID: id, Result: res})
		}
	}
}

// adaptStream adapts a stream function to the form the core runs.
func adaptStream[C any](fn func(context.Context, *Run, C) iter.Seq2[any, error]) func(context.Context, C, string) iter.Seq[any] {
	return func(ctx context.Context, c C, id string) iter.Seq[any] {
		return func(yield func(any) bool) {
			stopped := false
			run, emit := startRun(ctx, id, func(ev any) bool {
				stopped = !yield(ev)
				return !stopped
			})

			for ev, err := range fn(ctx, run, c) {
				if err != nil {
					emit(Failed{ID: id, Err: err})
					return
				}

				if emit(Emitted{ID: id, Event: ev}); stopped {
					return
				}
			}

			emit(Done[struct{}]{ID: id, Result: struct{}{}})
		}
	}
}

// adaptAcquire adapts an acquire function to the form the core runs.
func adaptAcquire[C, R any](fn func(context.Context, *Run, C) (R, error)) func(context.Context, C, string) iter.Seq[any] {
	return func(ctx context.Context, c C, id string) iter.Seq[any] {
		return func(yield func(any) bool) {
			run, emit := startRun(ctx, id, yield)

			value, err := fn(ctx, run, c)
			if err != nil {
				emit(Failed{ID: id, Err: err})
				return
			}

			emit(Acquired[R]{ID: id, Value: value})
		}
	}
}

// adaptRelease adapts a release function to the form the core runs.
func adaptRelease[S comparable, R any](fn func(context.Context, R, Leave[S])) func(context.Context, any, Leave[S]) {
	return func(ctx context.Context, value any, why Leave[S]) {
		if typed, ok := value.(R); ok {
			fn(ctx, typed, why)
		}
	}
}

// addInvocation declares a service of state s and returns its routing
// handle.
func (s *StateBuilder[S, C]) addInvocation(inv *invocation[S, C]) *invocation[S, C] {
	inv.state = s.n
	s.n.invocations = append(s.n.invocations, inv)

	return inv
}

// newInvocation is a service named name reporting Done[R], run by run.
func newInvocation[S comparable, C, R any](name string, run func(context.Context, C, string) iter.Seq[any]) *invocation[S, C] {
	return &invocation[S, C]{
		state:       nil,
		name:        name,
		doneType:    reflect.TypeFor[Done[R]](),
		run:         run,
		resource:    nil,
		retry:       nil,
		spawned:     false,
		inboxType:   nil,
		triggerType: nil,
	}
}

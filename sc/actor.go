package sc

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrActorStopped is returned by [Actor.Send] once [Actor.Run] has
	// returned.
	ErrActorStopped = errors.New("actor has stopped")

	// ErrActorStarted is returned by [Actor.Run] when it is called a
	// second time.
	ErrActorStarted = errors.New("actor was already run")
)

// Actor runs one machine instance in memory. It processes one event at a
// time, runs actions on the goroutine that called [Actor.Run], runs each
// service on its own goroutine, and keeps timers with the time package.
//
// Nothing an Actor holds survives the process. Persist [Actor.Snapshot] and
// start again with [Machine.ActorFrom] to continue after a restart.
type Actor[S comparable, C any] struct {
	m *Machine[S, C]

	// mailbox carries events to the Run loop.
	mailbox chan any

	// stopped is closed when Run returns.
	stopped chan struct{}

	started atomic.Bool

	// initial holds the effects Run carries out before its first event.
	initial []Effect[C]

	mu sync.Mutex

	// snap is the current snapshot, guarded by mu.
	snap Snapshot[S, C]

	// changed is closed and replaced whenever snap is replaced, guarded
	// by mu.
	changed chan struct{}
}

// Actor returns an actor for a new instance with context c. It does nothing
// until [Actor.Run] is called.
func (m *Machine[S, C]) Actor(c C) *Actor[S, C] {
	snap, effects := m.Start(c)
	return m.newActor(snap, effects)
}

// ActorFrom returns an actor that continues from snap. Its pending timers
// restart with their full delay and its unfinished services run again.
func (m *Machine[S, C]) ActorFrom(snap Snapshot[S, C]) *Actor[S, C] {
	return m.newActor(snap, m.Resume(snap))
}

func (m *Machine[S, C]) newActor(snap Snapshot[S, C], effects []Effect[C]) *Actor[S, C] {
	return &Actor[S, C]{
		m:       m,
		mailbox: make(chan any),
		stopped: make(chan struct{}),
		started: atomic.Bool{},
		initial: effects,
		mu:      sync.Mutex{},
		snap:    snap,
		changed: make(chan struct{}),
	}
}

// Run processes events until the machine is done, in which case it returns
// nil, or ctx is cancelled, in which case it returns the context's error. It
// stops every running service and timer before returning. An actor runs
// once.
func (a *Actor[S, C]) Run(ctx context.Context) error {
	if !a.started.CompareAndSwap(false, true) {
		return ErrActorStarted
	}
	defer close(a.stopped)

	ctx, cancel := context.WithCancel(ctx)

	var services sync.WaitGroup
	defer services.Wait()
	defer cancel()

	// deliver sends an event from a service or timer to the loop, giving
	// up when the actor stops.
	deliver := func(ev any) {
		select {
		case a.mailbox <- ev:
		case <-ctx.Done():
		}
	}

	stops := map[string]context.CancelFunc{}
	timers := map[string]*time.Timer{}
	defer func() {
		for _, t := range timers {
			t.Stop()
		}
	}()

	apply := func(effects []Effect[C]) error {
		for _, e := range effects {
			switch e.Kind {
			case EffectAction:
				if err := a.m.Exec(ctx, e, deliver); err != nil {
					return err
				}
			case EffectStartInvocation:
				serviceCtx, stop := context.WithCancel(ctx)
				stops[e.ID] = stop

				services.Go(func() {
					if err := a.m.Exec(serviceCtx, e, deliver); err != nil {
						deliver(Failed{ID: e.ID, Err: err})
					}
				})
			case EffectStopInvocation:
				if stop, ok := stops[e.ID]; ok {
					stop()
					delete(stops, e.ID)
				}
			case EffectStartTimer:
				timers[e.ID] = time.AfterFunc(e.Delay, func() { deliver(TimerFired{ID: e.ID}) })
			case EffectCancelTimer:
				if t, ok := timers[e.ID]; ok {
					t.Stop()
					delete(timers, e.ID)
				}
			}
		}

		return nil
	}

	if err := apply(a.initial); err != nil {
		return err
	}

	for {
		if a.Snapshot().Done() {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("run actor: %w", context.Cause(ctx))
		case ev := <-a.mailbox:
			next, effects := a.m.Transition(a.Snapshot(), ev)
			a.publish(next)

			if err := apply(effects); err != nil {
				return err
			}
		}
	}
}

func (a *Actor[S, C]) publish(next Snapshot[S, C]) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if next.seq == a.snap.seq {
		return
	}

	a.snap = next
	close(a.changed)
	a.changed = make(chan struct{})
}

// Send delivers an event to the actor and returns once the actor has taken
// it. It blocks while the actor is busy or not yet running, which gives the
// sender backpressure; ctx bounds the wait.
func (a *Actor[S, C]) Send(ctx context.Context, ev any) error {
	select {
	case a.mailbox <- ev:
		return nil
	case <-a.stopped:
		return ErrActorStopped
	case <-ctx.Done():
		return fmt.Errorf("send to actor: %w", context.Cause(ctx))
	}
}

// Snapshot returns the actor's current snapshot.
func (a *Actor[S, C]) Snapshot() Snapshot[S, C] {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.snap
}

// Snapshots yields the current snapshot and then each later one. A consumer
// slower than the actor sees only the latest snapshot when it comes back for
// the next, so it never holds the actor up. The sequence ends when the
// consumer breaks, when ctx is cancelled, or after the last snapshot of an
// actor that stopped.
func (a *Actor[S, C]) Snapshots(ctx context.Context) iter.Seq[Snapshot[S, C]] {
	return func(yield func(Snapshot[S, C]) bool) {
		first := true
		var seen uint64

		for {
			a.mu.Lock()
			snap, changed := a.snap, a.changed
			a.mu.Unlock()

			if first || snap.seq != seen {
				first, seen = false, snap.seq
				if !yield(snap) {
					return
				}

				continue
			}

			select {
			case <-changed:
			case <-ctx.Done():
				return
			case <-a.stopped:
				// Run publishes its last snapshot before it
				// returns, so one more look is enough.
				if a.Snapshot().seq == seen {
					return
				}
			}
		}
	}
}

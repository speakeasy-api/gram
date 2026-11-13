package sc

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
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
	mailbox chan envelope[S, C]

	// hooks are the functions the actor reports to; nil for none.
	hooks *Hooks[S, C]

	// services, when set, is acquired by each service run before it starts.
	services *semaphore.Weighted

	// now is the actor's clock.
	now func() time.Time

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

	// due maps each scheduled timer's ID to when it fires, guarded by mu.
	due map[string]time.Time
}

// envelope is an event on its way to the Run loop, with the context it was
// sent with.
type envelope[S comparable, C any] struct {
	ctx context.Context //nolint:containedctx // carries the sender's trace context across the mailbox to the hooks.
	ev  any

	// reply, when set, receives the step once its effects have been
	// started.
	reply chan<- Step[S, C]
}

// Actor returns an actor for a new instance with context c, started at the
// actor's clock. It does nothing until [Actor.Run] is called.
func (m *Machine[S, C]) Actor(c C, opts ...RunOption) *Actor[S, C] {
	o := runSettings(opts)
	snap, effects := m.Start(c, o.now())

	return m.newActor(snap, effects, o)
}

// ActorFrom returns an actor that continues from snap. Its pending timers
// are scheduled for the times they are due, and its unfinished services run
// again.
func (m *Machine[S, C]) ActorFrom(snap Snapshot[S, C], opts ...RunOption) *Actor[S, C] {
	return m.newActor(snap, m.Resume(snap), runSettings(opts))
}

func (m *Machine[S, C]) newActor(snap Snapshot[S, C], effects []Effect[C], o runOptions) *Actor[S, C] {
	hooks := hooksFor[S, C](o, m.id)

	return &Actor[S, C]{
		m:        m,
		mailbox:  make(chan envelope[S, C]),
		hooks:    hooks,
		services: o.services,
		now:      o.now,
		stopped:  make(chan struct{}),
		started:  atomic.Bool{},
		initial:  effects,
		mu:       sync.Mutex{},
		snap:     snap,
		changed:  make(chan struct{}),
		due:      map[string]time.Time{},
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
	deliver := func(evCtx context.Context, ev any) {
		select {
		case a.mailbox <- envelope[S, C]{ctx: context.WithoutCancel(evCtx), ev: ev, reply: nil}:
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
				if err := a.hooks.Exec(ctx, a.m, e, a.now(), deliver); err != nil {
					return err
				}
			case EffectStartInvocation:
				serviceCtx, stop := context.WithCancel(ctx)
				stops[e.ID] = stop

				services.Go(func() {
					if a.services != nil {
						if err := a.services.Acquire(serviceCtx, 1); err != nil {
							return
						}
						defer a.services.Release(1)
					}

					if err := a.hooks.Exec(serviceCtx, a.m, e, a.now(), deliver); err != nil {
						deliver(serviceCtx, Failed{ID: e.ID, Err: err})
					}
				})
			case EffectStopInvocation:
				if stop, ok := stops[e.ID]; ok {
					stop()
					delete(stops, e.ID)
				}
			case EffectStartTimer:
				now := a.now()
				wait := e.Wait(now, rand.Float64)
				a.setDue(e.ID, now.Add(wait))
				timers[e.ID] = time.AfterFunc(wait, func() {
					a.setDue(e.ID, time.Time{})
					deliver(ctx, TimerFired{ID: e.ID})
				})
			case EffectCancelTimer:
				if t, ok := timers[e.ID]; ok {
					t.Stop()
					delete(timers, e.ID)
				}
				a.setDue(e.ID, time.Time{})
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
		case env := <-a.mailbox:
			from := a.Snapshot()
			at := a.now()
			next, effects, _ := a.m.transition(from, env.ev, at)
			step := Step[S, C]{At: at, From: from, Event: env.ev, To: next, Effects: effects}
			a.hooks.Observe(env.ctx, step)

			// The new snapshot is published once its effects have been
			// carried out, so a reader never sees a state whose actions
			// have not run.
			err := apply(effects)
			a.publish(next)
			if env.reply != nil {
				env.reply <- step
			}
			if err != nil {
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
// sender backpressure; ctx bounds the wait. The actor's hooks see the event
// with ctx's values, such as the sender's span.
func (a *Actor[S, C]) Send(ctx context.Context, ev any) error {
	select {
	case a.mailbox <- envelope[S, C]{ctx: context.WithoutCancel(ctx), ev: ev, reply: nil}:
		return nil
	case <-a.stopped:
		return ErrActorStopped
	case <-ctx.Done():
		return fmt.Errorf("send to actor: %w", context.Cause(ctx))
	}
}

// setDue records when a timer fires; the zero time forgets it.
func (a *Actor[S, C]) setDue(id string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if at.IsZero() {
		delete(a.due, id)
		return
	}
	a.due[id] = at
}

// Timers returns the current snapshot's pending timers, soonest first, with
// Due set to when the actor scheduled each to fire, jitter included. A timer
// the actor has not scheduled yet, because Run has not reached it, keeps
// the Due of its snapshot entry.
func (a *Actor[S, C]) Timers() []PendingTimer[S] {
	a.mu.Lock()
	defer a.mu.Unlock()

	timers := a.snap.Timers()
	for i := range timers {
		if at, ok := a.due[timers[i].ID]; ok {
			timers[i].Due = at
		}
	}

	slices.SortStableFunc(timers, func(x, y PendingTimer[S]) int { return x.Due.Compare(y.Due) })

	return timers
}

// Advance asks the actor to fire the timers due by its clock now, without
// any other event. It returns once the actor has taken the request.
func (a *Actor[S, C]) Advance(ctx context.Context) error {
	return a.Send(ctx, nil)
}

// Call sends ev and waits until the actor has processed it: the transition
// taken, its actions run, its services and timers started. The returned step
// tells whether the event was accepted: it changed the instance when
// step.To.Seq() differs from step.From.Seq(). ctx bounds both waits.
func (a *Actor[S, C]) Call(ctx context.Context, ev any) (Step[S, C], error) {
	reply := make(chan Step[S, C], 1)

	select {
	case a.mailbox <- envelope[S, C]{ctx: context.WithoutCancel(ctx), ev: ev, reply: reply}:
	case <-a.stopped:
		return Step[S, C]{}, ErrActorStopped
	case <-ctx.Done():
		return Step[S, C]{}, fmt.Errorf("call actor: %w", context.Cause(ctx))
	}

	select {
	case step := <-reply:
		return step, nil
	case <-ctx.Done():
		return Step[S, C]{}, fmt.Errorf("call actor: %w", context.Cause(ctx))
	}
}

// Await blocks until cond holds for the actor's current snapshot, and
// returns that snapshot. It checks the snapshot first, then after every
// change. It fails with [ErrActorStopped] if the actor stops without cond
// holding, and with the context's error when ctx ends first.
func (a *Actor[S, C]) Await(ctx context.Context, cond func(Snapshot[S, C]) bool) (Snapshot[S, C], error) {
	for {
		a.mu.Lock()
		snap, changed := a.snap, a.changed
		a.mu.Unlock()

		if cond(snap) {
			return snap, nil
		}

		select {
		case <-changed:
		case <-a.stopped:
			if final := a.Snapshot(); cond(final) {
				return final, nil
			}

			return a.Snapshot(), ErrActorStopped
		case <-ctx.Done():
			return snap, fmt.Errorf("await actor: %w", context.Cause(ctx))
		}
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

package sc

import (
	"context"
	"fmt"
	"iter"
	"math/rand/v2"
	"reflect"
	"time"
)

// build accumulates the chart while the definition function runs.
type build[S comparable, C any] struct {
	// root is the implicit state containing every top-level state.
	root *node[S, C]

	// nodes indexes every declared state by its identifier.
	nodes map[S]*node[S, C]

	// ordered lists every declared state in document order.
	ordered []*node[S, C]

	// errs collects declaration errors for New to report together.
	errs []error

	// actions are the chart actions declared with Action.
	actions map[string]chartAction[S, C]
}

func (b *build[S, C]) errorf(format string, args ...any) {
	b.errs = append(b.errs, fmt.Errorf(format, args...))
}

// Builder declares the child states of one state. The Builder passed to the
// function given to [New] declares the top-level states.
type Builder[S comparable, C any] struct {
	b      *build[S, C]
	parent *node[S, C]
}

// Action declares an action named once for the whole chart, which any
// transition runs with [Transition.Run] and any state with
// [StateBuilder.OnEntry] and [StateBuilder.OnExit]. fn receives the move it
// runs in: the state that runs it and the active states before and after,
// so one action can do what depends on where the chart came from and where
// it went, such as recording a status and then notifying when it crossed a
// boundary. A chart action's name must not be used by a state's own
// actions. Like every action, its effect may be carried out more than once.
func (b *Builder[S, C]) Action(name string, fn func(ctx context.Context, c C, mv Move[S])) {
	if name == "" {
		b.b.errorf("a chart action has no name")
		return
	}
	if _, ok := b.b.actions[name]; ok {
		b.b.errorf("chart action %q is declared twice", name)
		return
	}

	b.b.actions[name] = fn
}

// RequiresAction declares a chart action like [Builder.Action], by name
// only. Its implementation is given later with [Machine.Bind]. Running it
// while still unbound fails with [ErrNotBound].
func (b *Builder[S, C]) RequiresAction(name string) {
	b.Action(name, nil)
}

// Initial names the child state that is entered when the enclosing state is
// entered without a more specific target. Every state with children needs
// one, except parallel states, which enter all of their children.
func (b *Builder[S, C]) Initial(s S) {
	if b.parent.kind == kindParallel {
		b.b.errorf("state %q: a parallel state has no initial state", b.parent.name)
		return
	}

	if b.parent.initialID != nil {
		b.b.errorf("state %q: initial state declared twice", b.parent.name)
		return
	}

	b.parent.initialID = &s
}

// State declares a state. Passing a function makes it a compound state whose
// children are declared inside that function. Calling State again with the
// same identifier under the same parent returns the existing state, so its
// behaviour can be declared across several statements.
func (b *Builder[S, C]) State(s S, children ...func(*Builder[S, C])) *StateBuilder[S, C] {
	n := b.declare(s, kindState)
	for _, fn := range children {
		fn(&Builder[S, C]{b: b.b, parent: n})
	}

	return &StateBuilder[S, C]{b: b.b, n: n}
}

// Parallel declares a state whose children are all active at once. Each state
// declared directly inside regions is one region.
func (b *Builder[S, C]) Parallel(s S, regions func(*Builder[S, C])) *StateBuilder[S, C] {
	n := b.declare(s, kindParallel)
	regions(&Builder[S, C]{b: b.b, parent: n})

	return &StateBuilder[S, C]{b: b.b, n: n}
}

// Final declares a final state. Entering it completes the enclosing state,
// which fires that state's [StateBuilder.OnDone] transitions. Entering a
// top-level final state completes the machine.
func (b *Builder[S, C]) Final(s S) *FinalBuilder[S, C] {
	return &FinalBuilder[S, C]{n: b.declare(s, kindFinal)}
}

// History declares a history pseudo-state inside the enclosing state. A
// transition that targets it re-enters the states that were active when the
// enclosing state was last exited.
func (b *Builder[S, C]) History(s S, kind HistoryKind) *HistoryBuilder[S, C] {
	n := b.declare(s, kindHistory)
	n.historyKind = kind
	if kind != Shallow && kind != Deep {
		b.b.errorf("history state %q: unknown history kind %q", n.name, kind)
	}

	return &HistoryBuilder[S, C]{b: b.b, n: n}
}

func (b *Builder[S, C]) declare(s S, kind nodeKind) *node[S, C] {
	name := fmt.Sprint(s)

	if existing, ok := b.b.nodes[s]; ok {
		switch {
		case existing.parent != b.parent:
			b.b.errorf("state %q: declared under both %q and %q", name, existing.parent.name, b.parent.name)
		case existing.kind != kind:
			b.b.errorf("state %q: declared as both %s and %s", name, existing.kind, kind)
		case kind == kindHistory:
			b.b.errorf("history state %q: declared twice", name)
		}

		return existing
	}

	if b.parent.kind == kindFinal || b.parent.kind == kindHistory {
		b.b.errorf("state %q: %s state %q cannot have children", name, b.parent.kind, b.parent.name)
	}

	n := new(node[S, C])
	n.id = s
	n.name = name
	n.kind = kind
	n.parent = b.parent
	n.order = len(b.b.ordered) + 1
	b.parent.children = append(b.parent.children, n)
	b.b.nodes[s] = n
	b.b.ordered = append(b.b.ordered, n)

	return n
}

// FinalBuilder declares the behaviour of a final state.
type FinalBuilder[S comparable, C any] struct {
	n *node[S, C]
}

// Entry adds an action that runs when the final state is entered.
func (f *FinalBuilder[S, C]) Entry(name string, fn func(context.Context, C)) *FinalBuilder[S, C] {
	f.n.entry = append(f.n.entry, action[C]{name: name, fn: fn})
	return f
}

// HistoryBuilder declares the behaviour of a history state.
type HistoryBuilder[S comparable, C any] struct {
	b *build[S, C]
	n *node[S, C]
}

// Default names the state entered when the history state is targeted before
// its parent has ever been exited. Without a default the parent's initial
// state is used.
func (h *HistoryBuilder[S, C]) Default(target S) {
	if h.n.defaultID != nil {
		h.b.errorf("history state %q: default declared twice", h.n.name)
		return
	}

	h.n.defaultID = &target
}

// StateBuilder declares the behaviour of one state.
type StateBuilder[S comparable, C any] struct {
	b *build[S, C]
	n *node[S, C]
}

// On declares a transition taken when an event of type Ev arrives while the
// state is active. Transitions for the same event are tried in declaration
// order and the first whose guard holds is taken. A descendant state's
// transition takes priority over its ancestor's.
func (s *StateBuilder[S, C]) On[Ev any]() *Transition[S, C, Ev] {
	t := newTransition[S, C, Ev](s.b, s.n, triggerEvent)
	t.t.eventType = reflect.TypeFor[Ev]()

	return t
}

// Always declares an eventless transition. It is taken as soon as the state
// is active and its guard holds, before any further event is processed.
func (s *StateBuilder[S, C]) Always() *Transition[S, C, struct{}] {
	return newTransition[S, C, struct{}](s.b, s.n, triggerAlways)
}

// After declares a transition taken once the state has been active for d.
// Entering the state emits an [EffectStartTimer] effect and leaving it emits
// [EffectCancelTimer]; the driver delivers [TimerFired] when the timer is due.
func (s *StateBuilder[S, C]) After(d time.Duration) *Transition[S, C, struct{}] {
	if d < 0 {
		s.b.errorf("state %q: negative delay %s", s.n.name, d)
	}

	t := s.after()
	t.t.delay = d

	return t
}

// Delay declares a transition taken once the state has been active for the
// duration delay returns. The duration is computed from the context as it is
// when the state is entered, so a backoff can grow with a failure count. A
// negative duration fires at once. delay must be a pure function of the
// context, like a guard. To recompute the duration after the context
// changes, re-enter the state.
func (s *StateBuilder[S, C]) Delay(name string, delay func(C) time.Duration) *Transition[S, C, struct{}] {
	t := s.after()
	t.t.delayName = name
	t.t.delayFn = delay

	return t
}

// Deadline declares a transition taken at the time deadline returns, such as
// a retry time named by a server or a renewal time stored in the context.
// The time is computed from the context as it is when the state is entered,
// and a time already past fires at once. The core never reads a clock: the
// timer effect carries the time in [Effect.At], and the driver schedules it.
// deadline must be a pure function of the context.
func (s *StateBuilder[S, C]) Deadline(name string, deadline func(C) time.Time) *Transition[S, C, struct{}] {
	t := s.after()
	t.t.delayName = name
	t.t.deadlineFn = deadline

	return t
}

// after adds a delayed transition to the state.
func (s *StateBuilder[S, C]) after() *Transition[S, C, struct{}] {
	t := newTransition[S, C, struct{}](s.b, s.n, triggerAfter)
	t.t.afterIndex = s.n.afterCount
	s.n.afterCount++

	return t
}

// OnDone declares a transition taken when the state completes: when a
// compound state enters one of its final children, or when every region of a
// parallel state has.
func (s *StateBuilder[S, C]) OnDone() *Transition[S, C, struct{}] {
	return newTransition[S, C, struct{}](s.b, s.n, triggerStateDone)
}

// Within gives every service run in the state or any state inside it a
// shared time budget of d, starting when the state is entered. A service
// still running when the budget ends is cancelled through its context and
// fails with [context.DeadlineExceeded], which reaches the chart like any
// other failure; one started after the budget ended fails at once. When
// states inside each other both declare a budget, the earlier deadline
// applies. The deadline is part of the snapshot, so it survives a restart.
func (s *StateBuilder[S, C]) Within(d time.Duration) *StateBuilder[S, C] {
	if d <= 0 {
		s.b.errorf("state %q: budget %s is not positive", s.n.name, d)
	}

	s.n.budget = d

	return s
}

// Entry adds an action that runs when the state is entered. The name must be
// unique among the state's actions.
func (s *StateBuilder[S, C]) Entry(name string, fn func(context.Context, C)) *StateBuilder[S, C] {
	s.n.entry = append(s.n.entry, action[C]{name: name, fn: fn})
	return s
}

// Exit adds an action that runs when the state is exited. The name must be
// unique among the state's actions.
func (s *StateBuilder[S, C]) Exit(name string, fn func(context.Context, C)) *StateBuilder[S, C] {
	s.n.exit = append(s.n.exit, action[C]{name: name, fn: fn})
	return s
}

// OnEntry runs chart actions, declared with [Builder.Action], when the state
// is entered, after its own entry actions.
func (s *StateBuilder[S, C]) OnEntry(names ...string) *StateBuilder[S, C] {
	s.n.entryRuns = append(s.n.entryRuns, names...)
	for range names {
		s.n.entryConds = append(s.n.entryConds, nil)
	}

	return s
}

// OnEntryIf runs a chart action on entry, as OnEntry does, only when cond
// holds for the step's [Move]. Because the decision is made in the core, the
// action appears in the step's effects only when it runs, where exploration
// and step invariants can check it. cond must be a pure function of the move.
func (s *StateBuilder[S, C]) OnEntryIf(name string, cond func(Move[S]) bool) *StateBuilder[S, C] {
	s.n.entryRuns = append(s.n.entryRuns, name)
	s.n.entryConds = append(s.n.entryConds, cond)

	return s
}

// OnExit runs chart actions, declared with [Builder.Action], when the state
// is exited, after its own exit actions.
func (s *StateBuilder[S, C]) OnExit(names ...string) *StateBuilder[S, C] {
	s.n.exitRuns = append(s.n.exitRuns, names...)
	for range names {
		s.n.exitConds = append(s.n.exitConds, nil)
	}

	return s
}

// OnExitIf runs a chart action on exit, as OnExit does, only when cond holds
// for the step's [Move].
func (s *StateBuilder[S, C]) OnExitIf(name string, cond func(Move[S]) bool) *StateBuilder[S, C] {
	s.n.exitRuns = append(s.n.exitRuns, name)
	s.n.exitConds = append(s.n.exitConds, cond)

	return s
}

// Invoke declares a service that runs while the state is active. It starts
// when the state is entered and receives the context as it was at that
// moment. Its result arrives as a [Done] event and its error as a [Failed]
// event, routed with the returned [Invocation]. Leaving the state before the
// service finishes stops it and discards its result.
//
// Drivers may run a service more than once, so fn must be idempotent.
func (s *StateBuilder[S, C]) Invoke[R any](name string, fn func(context.Context, C) (R, error)) *Invocation[S, C, R] {
	inv := &invocation[S, C]{
		state:    s.n,
		name:     name,
		doneType: reflect.TypeFor[Done[R]](),
		run:      adaptService(fn),
		resource: nil,
	}
	s.n.invocations = append(s.n.invocations, inv)

	return &Invocation[S, C, R]{b: s.b, n: s.n, inv: inv}
}

// Requires declares a service like [StateBuilder.Invoke], by its name and
// result type only. Its implementation is given later with [Machine.Bind],
// so the chart can be declared, exported and explored without the
// dependencies the service needs. A service run while still unbound fails
// with [ErrNotBound].
func (s *StateBuilder[S, C]) Requires[R any](name string) *Invocation[S, C, R] {
	inv := &invocation[S, C]{
		state:    s.n,
		name:     name,
		doneType: reflect.TypeFor[Done[R]](),
		run:      nil,
		resource: nil,
	}
	s.n.invocations = append(s.n.invocations, inv)

	return &Invocation[S, C, R]{b: s.b, n: s.n, inv: inv}
}

// RequiresStream declares a service like [StateBuilder.Stream], by name
// only. Its implementation is given later with [Machine.Bind].
func (s *StateBuilder[S, C]) RequiresStream(name string) *Invocation[S, C, struct{}] {
	return s.Requires[struct{}](name)
}

// Holds declares a resource the state holds for as long as it is active: a
// connection, a listener, a running node. acquire runs when the state is
// entered, like a service, and its outcome is routed with the returned
// [Resource]: OnAcquired when it succeeds, OnError when it fails. release
// runs when the state is left, after the services inside it have stopped,
// and is told why.
//
// A state's resources are acquired one after another, in the order they
// were declared: each starts once the one before it is held, and its acquire
// function can read the earlier ones with [Resource.From]. If one fails, the
// later ones are not acquired. Services of the same state start with the
// first acquisition, so put work that needs the resource in a child state
// entered from OnAcquired.
//
// The value never enters the context or the snapshot. The driver keeps it,
// and services and actions of the state and the states inside it read it
// with [Resource.From]. Resources are released in the reverse of the order
// they were declared, and those of inner states before those of outer ones.
//
// If the state is left while acquire is still running, acquire is left to
// finish and its value is released at once. Resuming a persisted instance
// that held the resource acquires it again before the services inside it
// run; if that fails, the OnError transition decides what happens.
//
// release cannot fail, like an action. Only the [Instance] and [Actor]
// drivers hold resources; pgdriver rejects a chart that declares one.
func (s *StateBuilder[S, C]) Holds[R any](name string, acquire func(context.Context, C) (R, error), release func(ctx context.Context, value R, why Leave[S])) *Resource[S, C, R] {
	res := s.HoldsRequired[R](name)
	res.inv.run = adaptAcquire(acquire)
	res.inv.resource.release = adaptRelease(release)

	return res
}

// HoldsRequired declares a resource like [StateBuilder.Holds], by its name
// and type only. Its acquire and release functions are given later with
// [Machine.Bind].
func (s *StateBuilder[S, C]) HoldsRequired[R any](name string) *Resource[S, C, R] {
	inv := &invocation[S, C]{
		state:    s.n,
		name:     name,
		doneType: reflect.TypeFor[Acquired[R]](),
		run:      nil,
		resource: &resourceSpec[S]{key: s.n.name + "\x00" + name, release: nil},
	}
	s.n.invocations = append(s.n.invocations, inv)

	return &Resource[S, C, R]{b: s.b, n: s.n, inv: inv}
}

// Resource routes the outcome of acquiring a resource and gives access to
// its value.
type Resource[S comparable, C, R any] struct {
	b   *build[S, C]
	n   *node[S, C]
	inv *invocation[S, C]
}

// OnAcquired declares a transition taken when the resource has been
// acquired.
func (r *Resource[S, C, R]) OnAcquired() *Transition[S, C, Acquired[R]] {
	t := newTransition[S, C, Acquired[R]](r.b, r.n, triggerAcquired)
	t.t.eventType = reflect.TypeFor[Acquired[R]]()
	t.t.invocation = r.inv

	return t
}

// OnError declares a transition taken when the resource cannot be acquired,
// at first or again after a restart. Every resource needs at least one.
func (r *Resource[S, C, R]) OnError() *Transition[S, C, Failed] {
	t := newTransition[S, C, Failed](r.b, r.n, triggerInvokeError)
	t.t.eventType = reflect.TypeFor[Failed]()
	t.t.invocation = r.inv

	return t
}

// From returns the resource's value from the context a service or action
// was given. It reports false when the resource is not held: before it is
// acquired, after its state is left, or under a driver that holds no
// resources.
func (r *Resource[S, C, R]) From(ctx context.Context) (R, bool) {
	var zero R

	lookup, ok := ctx.Value(heldResources{}).(func(key string) (any, bool))
	if !ok {
		return zero, false
	}

	value, ok := lookup(r.inv.resource.key)
	if !ok {
		return zero, false
	}

	typed, ok := value.(R)

	return typed, ok
}

// heldResources is the context key under which a driver exposes the
// resources it holds.
type heldResources struct{}

// adaptAcquire adapts an acquire function to the form the core runs.
func adaptAcquire[C, R any](fn func(context.Context, C) (R, error)) func(context.Context, C, string) iter.Seq[any] {
	return func(ctx context.Context, c C, id string) iter.Seq[any] {
		return func(yield func(any) bool) {
			value, err := fn(ctx, c)
			if err != nil {
				yield(Failed{ID: id, Err: err})
				return
			}

			yield(Acquired[R]{ID: id, Value: value})
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

// adaptService adapts a service function to the form the core runs.
func adaptService[C, R any](fn func(context.Context, C) (R, error)) func(context.Context, C, string) iter.Seq[any] {
	return func(ctx context.Context, c C, id string) iter.Seq[any] {
		return func(yield func(any) bool) {
			res, err := fn(ctx, c)
			if err != nil {
				yield(Failed{ID: id, Err: err})
				return
			}

			yield(Done[R]{ID: id, Result: res})
		}
	}
}

// adaptStream adapts a stream function to the form the core runs.
func adaptStream[C any](fn func(context.Context, C) iter.Seq2[any, error]) func(context.Context, C, string) iter.Seq[any] {
	return func(ctx context.Context, c C, id string) iter.Seq[any] {
		return func(yield func(any) bool) {
			for ev, err := range fn(ctx, c) {
				if err != nil {
					yield(Failed{ID: id, Err: err})
					return
				}

				if !yield(Emitted{ID: id, Event: ev}) {
					return
				}
			}

			yield(Done[struct{}]{ID: id, Result: struct{}{}})
		}
	}
}

// Stream declares a service that sends events to the machine for as long as
// the state is active. Each value the sequence yields is delivered as an
// event. Yielding a non-nil error ends the service with a [Failed] event, and
// a sequence that ends normally produces a [Done] event. When the state is
// exited the driver stops ranging, so the function's deferred cleanup runs.
func (s *StateBuilder[S, C]) Stream(name string, fn func(context.Context, C) iter.Seq2[any, error]) *Invocation[S, C, struct{}] {
	inv := &invocation[S, C]{
		state:    s.n,
		name:     name,
		doneType: reflect.TypeFor[Done[struct{}]](),
		run:      adaptStream(fn),
		resource: nil,
	}
	s.n.invocations = append(s.n.invocations, inv)

	return &Invocation[S, C, struct{}]{b: s.b, n: s.n, inv: inv}
}

// Invocation routes the outcome of an invoked service.
type Invocation[S comparable, C, R any] struct {
	b   *build[S, C]
	n   *node[S, C]
	inv *invocation[S, C]
}

// OnDone declares a transition taken when the service succeeds.
func (i *Invocation[S, C, R]) OnDone() *Transition[S, C, Done[R]] {
	t := newTransition[S, C, Done[R]](i.b, i.n, triggerInvokeDone)
	t.t.eventType = reflect.TypeFor[Done[R]]()
	t.t.invocation = i.inv

	return t
}

// OnError declares a transition taken when the service fails. Every
// invocation needs at least one.
func (i *Invocation[S, C, R]) OnError() *Transition[S, C, Failed] {
	t := newTransition[S, C, Failed](i.b, i.n, triggerInvokeError)
	t.t.eventType = reflect.TypeFor[Failed]()
	t.t.invocation = i.inv

	return t
}

// Transition declares what one transition does. A transition without a
// target changes the context or runs actions and leaves the active states as
// they are.
type Transition[S comparable, C, Ev any] struct {
	b *build[S, C]
	t *transition[S, C]
}

// newTransition adds a transition with the given trigger to source.
func newTransition[S comparable, C, Ev any](b *build[S, C], source *node[S, C], trigger triggerKind) *Transition[S, C, Ev] {
	t := new(transition[S, C])
	t.source = source
	t.trigger = trigger
	source.transitions = append(source.transitions, t)

	return &Transition[S, C, Ev]{b: b, t: t}
}

// event recovers the typed event from the value the core passes around.
// Triggers without an event value pass struct{}, which is also their Ev.
func event[Ev any](v any) Ev {
	ev, _ := v.(Ev)
	return ev
}

// When restricts the transition to events and contexts for which g holds.
// A transition may have several guards, each with its own name; all must
// hold, and they are checked in the order they were declared.
func (t *Transition[S, C, Ev]) When(name string, g func(C, Ev) bool) *Transition[S, C, Ev] {
	t.t.guards = append(t.t.guards, guard[C]{
		name: name,
		fn:   func(c C, ev any, _ time.Time) bool { return g(c, event[Ev](ev)) },
	})

	return t
}

// WhenAt is [Transition.When] for a guard that needs the current time, such
// as whether a retry time has passed. now is the driver's time for an event,
// and a timer's due time while that timer fires. Like every guard, g must be
// a pure function of its arguments.
func (t *Transition[S, C, Ev]) WhenAt(name string, g func(c C, ev Ev, now time.Time) bool) *Transition[S, C, Ev] {
	t.t.guards = append(t.t.guards, guard[C]{
		name: name,
		fn:   func(c C, ev any, now time.Time) bool { return g(c, event[Ev](ev), now) },
	})

	return t
}

// WhenIn restricts the transition to times when every one of states is
// active. It lets one parallel region react to where another region is,
// which a guard cannot see. It combines with [Transition.When]: both must
// hold.
func (t *Transition[S, C, Ev]) WhenIn(states ...S) *Transition[S, C, Ev] {
	if len(states) == 0 {
		t.b.errorf("state %q: WhenIn names no states", t.t.source.name)
	}

	t.t.inIDs = append(t.t.inIDs, states...)

	return t
}

// Jitter makes the driver vary a delayed transition's wait at random by up
// to fraction of it in either direction, so that many instances started
// together do not fire together. It applies only to transitions declared
// with After, Delay, or Deadline, and fraction must be between 0 and 1. The
// core stays deterministic: the effect carries the fraction in
// [Effect.Jitter], and the driver draws the variation.
func (t *Transition[S, C, Ev]) Jitter(fraction float64) *Transition[S, C, Ev] {
	if t.t.trigger != triggerAfter {
		t.b.errorf("state %q: Jitter applies only to delayed transitions", t.t.source.name)
	}
	if fraction < 0 || fraction > 1 {
		t.b.errorf("state %q: jitter %v is not between 0 and 1", t.t.source.name, fraction)
	}

	t.t.jitter = fraction

	return t
}

// WhenNotIn restricts the transition to times when none of states is active.
// It combines with [Transition.When] and [Transition.WhenIn]: all must hold.
func (t *Transition[S, C, Ev]) WhenNotIn(states ...S) *Transition[S, C, Ev] {
	if len(states) == 0 {
		t.b.errorf("state %q: WhenNotIn names no states", t.t.source.name)
	}

	t.t.notInIDs = append(t.t.notInIDs, states...)

	return t
}

// Assign replaces the context with the value f returns. Assignments, actions,
// and raises run in the order they are declared, so a later step sees the
// context produced by an earlier one.
func (t *Transition[S, C, Ev]) Assign(name string, f func(C, Ev) C) *Transition[S, C, Ev] {
	t.t.steps = append(t.t.steps, step[C]{
		kind:   stepAssign,
		name:   name,
		assign: func(c C, ev any, _ time.Time, _ *rand.Rand) C { return f(c, event[Ev](ev)) },
		do:     nil,
		raise:  nil,
	})

	return t
}

// AssignAt is [Transition.Assign] for an assignment that needs the current
// time, such as recording when something happened or computing the next
// retry from it. now is the same time [Transition.WhenAt] sees.
func (t *Transition[S, C, Ev]) AssignAt(name string, f func(c C, ev Ev, now time.Time) C) *Transition[S, C, Ev] {
	t.t.steps = append(t.t.steps, step[C]{
		kind:   stepAssign,
		name:   name,
		assign: func(c C, ev any, now time.Time, _ *rand.Rand) C { return f(c, event[Ev](ev), now) },
		do:     nil,
		raise:  nil,
	})

	return t
}

// AssignRand is [Transition.AssignAt] for an assignment that also needs
// random numbers, such as spreading a refresh time so that instances started
// together do not all act together. rnd is a source the core derives from the
// instance's seed and how many events it has processed, so the assignment
// stays a pure function of its arguments: replaying the same events gives the
// same draws, and exploration stays deterministic. For a timer, prefer
// [Transition.Jitter].
func (t *Transition[S, C, Ev]) AssignRand(name string, f func(c C, ev Ev, now time.Time, rnd *rand.Rand) C) *Transition[S, C, Ev] {
	t.t.steps = append(t.t.steps, step[C]{
		kind:   stepAssign,
		name:   name,
		assign: func(c C, ev any, now time.Time, rnd *rand.Rand) C { return f(c, event[Ev](ev), now, rnd) },
		do:     nil,
		raise:  nil,
	})

	return t
}

// Do adds an action. The core does not run it: it returns an [EffectAction]
// carrying the context and event the action must see. The name must be
// unique among the source state's actions.
func (t *Transition[S, C, Ev]) Do(name string, f func(context.Context, C, Ev)) *Transition[S, C, Ev] {
	t.t.steps = append(t.t.steps, step[C]{
		kind:   stepDo,
		name:   name,
		assign: nil,
		do:     func(ctx context.Context, c C, ev any) { f(ctx, c, event[Ev](ev)) },
		raise:  nil,
	})

	return t
}

// Run runs chart actions, declared with [Builder.Action], as steps of the
// transition, in order with its other steps.
func (t *Transition[S, C, Ev]) Run(names ...string) *Transition[S, C, Ev] {
	for _, name := range names {
		t.t.steps = append(t.t.steps, step[C]{kind: stepRun, name: name, assign: nil, do: nil, raise: nil})
	}

	return t
}

// RunIf runs a chart action as a step of the transition, as Run does, only
// when cond holds for the step's [Move]. The decision is made in the core, so
// the action appears in the step's effects only when it runs. cond must be a
// pure function of the move.
func (t *Transition[S, C, Ev]) RunIf(name string, cond func(Move[S]) bool) *Transition[S, C, Ev] {
	if t.t.runConds == nil {
		t.t.runConds = map[int]func(Move[S]) bool{}
	}
	t.t.runConds[len(t.t.steps)] = cond
	t.t.steps = append(t.t.steps, step[C]{kind: stepRun, name: name, assign: nil, do: nil, raise: nil})

	return t
}

// Raise queues the event f returns for the machine itself. It is processed
// in the same call to [Machine.Transition], after the current transitions
// complete, which lets one parallel region signal another.
func (t *Transition[S, C, Ev]) Raise(name string, f func(C, Ev) any) *Transition[S, C, Ev] {
	t.t.steps = append(t.t.steps, step[C]{
		kind:   stepRaise,
		name:   name,
		assign: nil,
		do:     nil,
		raise:  func(c C, ev any) any { return f(c, event[Ev](ev)) },
	})

	return t
}

// To sets the state the transition enters. Targeting a history state
// re-enters what its parent last had active.
//
// When the target is the source state or lies inside it, the source is not
// exited and entered again unless [Transition.Reenter] is also called.
func (t *Transition[S, C, Ev]) To(target S) *Transition[S, C, Ev] {
	if t.t.targetID != nil {
		t.b.errorf("state %q: transition has more than one target", t.t.source.name)
		return t
	}

	t.t.targetID = &target

	return t
}

// Stay marks a transition that consumes its event and does nothing else:
// no target and no steps. It is how a chart says that an event is expected
// and deliberately dropped, such as the error of a service whose failure
// does not matter, which is otherwise a declaration error. A transition
// declared after it for the same trigger is never reached, unless Stay's
// transition has a guard that fails.
func (t *Transition[S, C, Ev]) Stay() *Transition[S, C, Ev] {
	t.t.stay = true
	return t
}

// Reenter makes the transition exit and re-enter its source state even when
// the target is the source or one of its descendants. Exit and entry actions
// run again, timers restart, and services are restarted.
func (t *Transition[S, C, Ev]) Reenter() *Transition[S, C, Ev] {
	t.t.reenter = true
	return t
}

package sc

import (
	"context"
	"fmt"
	"iter"
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

	t := newTransition[S, C, struct{}](s.b, s.n, triggerAfter)
	t.t.delay = d
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
		run: func(ctx context.Context, c C, id string) iter.Seq[any] {
			return func(yield func(any) bool) {
				res, err := fn(ctx, c)
				if err != nil {
					yield(Failed{ID: id, Err: err})
					return
				}

				yield(Done[R]{ID: id, Result: res})
			}
		},
	}
	s.n.invocations = append(s.n.invocations, inv)

	return &Invocation[S, C, R]{b: s.b, n: s.n, inv: inv}
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
		run: func(ctx context.Context, c C, id string) iter.Seq[any] {
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
		},
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

// When restricts the transition to events and contexts for which g holds. A
// transition takes at most one guard.
func (t *Transition[S, C, Ev]) When(name string, g func(C, Ev) bool) *Transition[S, C, Ev] {
	t.t.guards = append(t.t.guards, guard[C]{
		name: name,
		fn:   func(c C, ev any) bool { return g(c, event[Ev](ev)) },
	})

	return t
}

// Assign replaces the context with the value f returns. Assignments, actions,
// and raises run in the order they are declared, so a later step sees the
// context produced by an earlier one.
func (t *Transition[S, C, Ev]) Assign(name string, f func(C, Ev) C) *Transition[S, C, Ev] {
	t.t.steps = append(t.t.steps, step[C]{
		kind:   stepAssign,
		name:   name,
		assign: func(c C, ev any) C { return f(c, event[Ev](ev)) },
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

// Reenter makes the transition exit and re-enter its source state even when
// the target is the source or one of its descendants. Exit and entry actions
// run again, timers restart, and services are restarted.
func (t *Transition[S, C, Ev]) Reenter() *Transition[S, C, Ev] {
	t.t.reenter = true
	return t
}

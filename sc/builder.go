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

	// kinds records, for each service or resource name checked so far,
	// whether it names a resource.
	kinds map[string]bool

	// raises holds the names given to RaiseAfter, and cancels the names
	// CancelRaise refers to.
	raises  map[string]bool
	cancels []string

	// sends holds the services SendTo names, and emits is set once any
	// transition emits.
	sends []string
	emits bool

	// releases holds the resources Release names.
	releases []string

	// spawns holds the services Spawn and StopSpawned name.
	spawns []string

	// emitTypes holds the types of the events Emit steps return, and
	// outputTypes those of the completion events final states report.
	emitTypes   []reflect.Type
	outputTypes []reflect.Type
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

// Chart returns the builder for the chart itself, which is a state like
// any other: it can run entry and exit actions, invoke services and child
// machines, hold resources, carry timers and a budget, and take
// transitions. What it declares applies for as long as the instance lives.
// Its entry actions run when the instance starts and its exit actions when
// the machine finishes; its services run from start to finish; and its
// transitions are taken in any state that does not handle the event first.
//
// In snapshots, effects and hooks the chart goes by the machine's name.
func (b *Builder[S, C]) Chart() *StateBuilder[S, C] {
	return &StateBuilder[S, C]{b: b.b, n: b.b.root}
}

// Regions makes the top-level states regions that are all active at once,
// as [Builder.Parallel] does for the states inside it, so the chart needs no
// initial state. The machine finishes when every region has: each is in a
// final state of its own, or is itself a final state. Call it on the
// Builder given to [New].
func (b *Builder[S, C]) Regions() {
	switch {
	case !b.parent.root:
		b.b.errorf("state %q: Regions applies to the chart itself; use Parallel for a state", b.parent.name)
	case b.parent.initialID != nil:
		b.b.errorf("a chart of regions has no initial state")
	default:
		b.parent.kind = kindParallel
	}
}

// On declares a transition on the enclosing state, as [StateBuilder.On]
// does. On the Builder given to [New] that is the chart itself, so the
// transition is taken whatever state the chart is in, unless an active
// state handles the event first: a cancellation or a shutdown that applies
// everywhere.
func (b *Builder[S, C]) On[Ev any]() *Transition[S, C, Ev] {
	return (&StateBuilder[S, C]{b: b.b, n: b.parent}).On[Ev]()
}

// OnAny declares a transition on the enclosing state for any event its
// other transitions do not take, as [StateBuilder.OnAny] does.
func (b *Builder[S, C]) OnAny() *Transition[S, C, any] {
	return (&StateBuilder[S, C]{b: b.b, n: b.parent}).OnAny()
}

// Always declares an eventless transition on the enclosing state, as
// [StateBuilder.Always] does.
func (b *Builder[S, C]) Always() *Transition[S, C, struct{}] {
	return (&StateBuilder[S, C]{b: b.b, n: b.parent}).Always()
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
// top-level final state completes the machine. Declared directly inside a
// parallel state it is a region that is complete from the start.
func (b *Builder[S, C]) Final(s S) *FinalBuilder[S, C] {
	return &FinalBuilder[S, C]{b: b.b, n: b.declare(s, kindFinal)}
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
	b *build[S, C]
	n *node[S, C]
}

// Tag labels the final state, as [StateBuilder.Tag] does.
func (f *FinalBuilder[S, C]) Tag(tags ...string) *FinalBuilder[S, C] {
	f.n.tags = append(f.n.tags, tags...)
	return f
}

// AssignOnEntry replaces the context when the final state is entered, as
// [StateBuilder.AssignOnEntry] does, so the transitions taken on the
// enclosing state's completion see the result.
func (f *FinalBuilder[S, C]) AssignOnEntry(name string, fn func(C) C) *FinalBuilder[S, C] {
	f.n.entryAssigns = append(f.n.entryAssigns, stateAssign[C]{name: name, fn: fn})
	return f
}

// RaiseOnEntry queues an event for the machine itself when the final state
// is entered, as [StateBuilder.RaiseOnEntry] does.
func (f *FinalBuilder[S, C]) RaiseOnEntry(name string, fn func(C) any) *FinalBuilder[S, C] {
	f.n.entryRaises = append(f.n.entryRaises, stateRaise[C]{name: name, fn: fn})
	return f
}

// Output declares what entering the final state reports. The enclosing
// state's completion then carries it: a transition declared there with
// [StateBuilder.OnDoneWith] receives a [Done] whose Result is the value f
// returns for the context, as it is after the final state's own entry
// assignments, and whose ID is the final state's name. For a top-level
// final state it is the machine's output, read with [Machine.Output].
func (f *FinalBuilder[S, C]) Output[R any](fn func(C) R) *FinalBuilder[S, C] {
	if f.n.output != nil {
		f.b.errorf("final state %q: output declared twice", f.n.name)
	}

	name := f.n.name
	f.n.output = func(c C) any { return Done[R]{ID: name, Result: fn(c)} }
	f.n.outputValue = func(c C) any { return fn(c) }
	f.b.outputTypes = append(f.b.outputTypes, reflect.TypeFor[Done[R]]())

	return f
}

// EmitOnEntry hands the event f returns to whoever runs the machine when
// the final state is entered, as [StateBuilder.EmitOnEntry] does: a child
// machine's last word to its parent.
func (f *FinalBuilder[S, C]) EmitOnEntry[T any](name string, fn func(C) T) *FinalBuilder[S, C] {
	f.n.entryEmits = append(f.n.entryEmits, emitter(f.b, f.n, name, fn))
	return f
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
// state is used. A history state of a parallel state, or a deep one above
// one, can name a default in each of several regions with also.
func (h *HistoryBuilder[S, C]) Default(target S, also ...S) {
	if h.n.defaultID != nil {
		h.b.errorf("history state %q: default declared twice", h.n.name)
		return
	}

	h.n.defaultID = &target
	h.n.defaultAlso = also
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
//
// Ev must be a concrete type: an event matches the transition whose Ev is
// exactly its own type, so an interface type would match nothing and is a
// declaration error.
func (s *StateBuilder[S, C]) On[Ev any]() *Transition[S, C, Ev] {
	t := newTransition[S, C, Ev](s.b, s.n, triggerEvent)
	t.t.eventType = reflect.TypeFor[Ev]()

	if t.t.eventType.Kind() == reflect.Interface {
		s.b.errorf("state %q: event type %s is an interface; events are matched by their concrete type", s.n.name, t.t.eventType)
	}

	return t
}

// OnAny declares a transition taken when an event arrives that no other
// transition of the state takes: one of a type the state has no On for, or
// one whose On transitions all have failing guards. It is tried after the
// state's On transitions whatever order they were declared in, and a
// descendant state's transitions still come first. It suits a state that
// must answer everything it does not expect, such as by recording it or by
// leaving for an error state.
//
// Only events sent to the machine and those a stream service yields count.
// Timers, service results and state completions never match.
//
// An action on the transition receives the event as it arrived. A driver
// that persists effects, such as pgdriver, can only store an event whose
// type the chart names in an On somewhere.
func (s *StateBuilder[S, C]) OnAny() *Transition[S, C, any] {
	t := newTransition[S, C, any](s.b, s.n, triggerEvent)
	t.t.wildcard = true

	return t
}

// Tag labels the state. A snapshot reports the tags of its active states
// with [Snapshot.HasTag], which lets a caller ask what kind of state an
// instance is in, such as "busy" or "terminal", without naming every state
// of that kind.
func (s *StateBuilder[S, C]) Tag(tags ...string) *StateBuilder[S, C] {
	s.n.tags = append(s.n.tags, tags...)
	return s
}

// Always declares an eventless transition. It is taken as soon as the state
// is active and its guard holds, before any further event is processed. One
// that changes nothing, neither the active states nor the context, is taken
// once and then rests until something else changes, so an action on a
// transition that stays where it is runs once per event and not for ever.
// Eventless transitions that keep changing things without settling are a
// cycle, and panic.
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

// OnDoneWith declares a transition taken when the state completes, as
// [StateBuilder.OnDone] does, that receives the output of the final state
// reached: a [Done] whose Result is what that state declared with
// [FinalBuilder.Output] and whose ID is its name, so one transition can
// tell several final states apart. It receives the zero value when the
// final state declares no output, or one of another type, and for a
// parallel state, whose regions each end in a final state of their own.
func (s *StateBuilder[S, C]) OnDoneWith[R any]() *Transition[S, C, Done[R]] {
	return newTransition[S, C, Done[R]](s.b, s.n, triggerStateDone)
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

	if s.n.budget != 0 {
		s.b.errorf("state %q: budget declared twice", s.n.name)
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

// AssignOnEntry replaces the context with the value f returns whenever the
// state is entered, before its entry actions, which see the new context. It
// is the place to reset what belongs to one visit of the state, such as a
// count of attempts.
func (s *StateBuilder[S, C]) AssignOnEntry(name string, f func(C) C) *StateBuilder[S, C] {
	if name == "" {
		s.b.errorf("state %q: an assignment has no name", s.n.name)
	}
	s.n.entryAssigns = append(s.n.entryAssigns, stateAssign[C]{name: name, fn: f})

	return s
}

// AssignOnExit replaces the context whenever the state is exited, before
// its exit actions, as AssignOnEntry does on entry.
func (s *StateBuilder[S, C]) AssignOnExit(name string, f func(C) C) *StateBuilder[S, C] {
	if name == "" {
		s.b.errorf("state %q: an assignment has no name", s.n.name)
	}
	s.n.exitAssigns = append(s.n.exitAssigns, stateAssign[C]{name: name, fn: f})

	return s
}

// RaiseOnEntry queues the event f returns for the machine itself whenever
// the state is entered, as [Transition.Raise] does from a transition. It is
// processed in the same call to [Machine.Transition], once the step that
// entered the state is complete.
func (s *StateBuilder[S, C]) RaiseOnEntry(name string, f func(C) any) *StateBuilder[S, C] {
	if name == "" {
		s.b.errorf("state %q: a raise has no name", s.n.name)
	}
	s.n.entryRaises = append(s.n.entryRaises, stateRaise[C]{name: name, fn: f})

	return s
}

// RaiseOnExit queues the event f returns for the machine itself whenever the
// state is exited, as RaiseOnEntry does on entry.
func (s *StateBuilder[S, C]) RaiseOnExit(name string, f func(C) any) *StateBuilder[S, C] {
	if name == "" {
		s.b.errorf("state %q: a raise has no name", s.n.name)
	}
	s.n.exitRaises = append(s.n.exitRaises, stateRaise[C]{name: name, fn: f})

	return s
}

// SendOnEntry sends the event f returns to the service named service
// whenever the state is entered, as [Transition.SendTo] does from a
// transition. It reaches a service the state itself invokes, which is how a
// state greets its own child machine or gives a service its first
// instruction.
func (s *StateBuilder[S, C]) SendOnEntry(name, service string, f func(C) any) *StateBuilder[S, C] {
	s.b.sends = append(s.b.sends, service)
	s.n.entrySends = append(s.n.entrySends, stateSend[C]{name: name, service: service, fn: f})

	return s
}

// SendOnExit sends an event to a service whenever the state is exited, as
// SendOnEntry does on entry. A service the state itself invokes is told
// before it is stopped: the event is still in its [Inbox] after its context
// ends, so it hears the last thing it was sent.
func (s *StateBuilder[S, C]) SendOnExit(name, service string, f func(C) any) *StateBuilder[S, C] {
	s.b.sends = append(s.b.sends, service)
	s.n.exitSends = append(s.n.exitSends, stateSend[C]{name: name, service: service, fn: f})

	return s
}

// EmitOnEntry hands the event f returns to whoever runs the machine
// whenever the state is entered, as [Transition.Emit] does from a
// transition. T must be a concrete type.
func (s *StateBuilder[S, C]) EmitOnEntry[T any](name string, f func(C) T) *StateBuilder[S, C] {
	s.n.entryEmits = append(s.n.entryEmits, emitter(s.b, s.n, name, f))
	return s
}

// EmitOnExit hands an event to whoever runs the machine whenever the state
// is exited, as EmitOnEntry does on entry.
func (s *StateBuilder[S, C]) EmitOnExit[T any](name string, f func(C) T) *StateBuilder[S, C] {
	s.n.exitEmits = append(s.n.exitEmits, emitter(s.b, s.n, name, f))
	return s
}

// emitter records an event a state emits, and the type it emits.
func emitter[S comparable, C, T any](b *build[S, C], n *node[S, C], name string, f func(C) T) stateRaise[C] {
	emitted := reflect.TypeFor[T]()
	if emitted.Kind() == reflect.Interface {
		b.errorf("state %q: emit %q returns %s, an interface; emit a concrete type", n.name, name, emitted)
	} else {
		b.emitTypes = append(b.emitTypes, emitted)
	}
	b.emits = true

	return stateRaise[C]{name: name, fn: func(c C) any { return f(c) }}
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
		retry:    nil,
		spawned:  false,
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
		retry:    nil,
		spawned:  false,
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
// later ones are not acquired.
//
// Acquiring does not hold anything else up: the services of the state, and
// of the child states entered with it, start alongside the first
// acquisition and may find [Resource.From] reporting false. Put work that
// needs the resource in a child state entered from OnAcquired.
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
		retry:    nil,
		spawned:  false,
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
			ctx, emit := recording(ctx, id, yield)

			res, err := fn(ctx, c)
			if err != nil {
				emit(Failed{ID: id, Err: err})
				return
			}

			emit(Done[R]{ID: id, Result: res})
		}
	}
}

// adaptStream adapts a stream function to the form the core runs.
func adaptStream[C any](fn func(context.Context, C) iter.Seq2[any, error]) func(context.Context, C, string) iter.Seq[any] {
	return func(ctx context.Context, c C, id string) iter.Seq[any] {
		return func(yield func(any) bool) {
			stopped := false
			ctx, emit := recording(ctx, id, func(ev any) bool {
				stopped = !yield(ev)
				return !stopped
			})

			for ev, err := range fn(ctx, c) {
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
		retry:    nil,
		spawned:  false,
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

// Retry makes the driver run the service again when it fails, after the
// waits the policy gives, instead of failing it. The chart stays in the
// state meanwhile, and sees a [Failed] event, through OnError, only for a
// failure the policy does not cover: one its If rejects, the one after Max
// retries, or one whose retry could not start before a [StateBuilder.Within]
// budget around the state runs out. Leaving the state abandons the retry.
//
// The count of failures lives in the snapshot, not the context, so it
// survives a restart and resets each time the state is entered.
func (i *Invocation[S, C, R]) Retry(policy Retry) *Invocation[S, C, R] {
	switch {
	case i.inv.retry != nil:
		i.b.errorf("state %q: service %q has more than one retry policy", i.n.name, i.inv.name)
	case policy.Max != Forever && policy.Max <= 0:
		i.b.errorf("state %q: service %q retries %d times; use a positive number or Forever", i.n.name, i.inv.name, policy.Max)
	case policy.First < 0 || policy.Cap < 0:
		i.b.errorf("state %q: service %q has a negative retry wait", i.n.name, i.inv.name)
	case policy.Cap > 0 && policy.First == 0:
		i.b.errorf("state %q: service %q has a retry cap but no first wait to grow from", i.n.name, i.inv.name)
	case policy.Max == Forever && policy.First == 0:
		i.b.errorf("state %q: service %q retries forever without a wait", i.n.name, i.inv.name)
	case policy.Jitter < 0 || policy.Jitter > 1:
		i.b.errorf("state %q: service %q has retry jitter %v, which is not between 0 and 1", i.n.name, i.inv.name, policy.Jitter)
	}

	i.inv.retry = &policy

	return i
}

// Spawned makes the service one that runs any number of times at once
// while its state is active, each run under a key: one per upload, per
// host, per job. Nothing starts when the state is entered. A transition
// starts a run with [Transition.Spawn] and stops one with
// [Transition.StopSpawned]; leaving the state stops them all.
//
// The service's OnDone and OnError transitions are taken for every run, and
// tell the runs apart with [SpawnKey] on the event's ID. The service reads
// its own key with SpawnKey on [RunID].
func (i *Invocation[S, C, R]) Spawned() *Invocation[S, C, R] {
	i.inv.spawned = true
	return i
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
	t.sendTargets = map[int]string{}
	t.keys = map[int]func(C, any) string{}
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

// WhenEvent is [Transition.When] for a guard that needs the event being
// processed whatever its type. On an eventless transition, declared with
// Always, that is the event that started the step, or the raised event
// being processed, which a typed guard there cannot see; it is nil when the
// step began with a timer or with the instance starting. On other
// transitions it is the transition's own event. An action on an eventless
// transition carries the event too, in its effect, when the chart names the
// event's type in an On somewhere.
func (t *Transition[S, C, Ev]) WhenEvent(name string, g func(c C, ev any) bool) *Transition[S, C, Ev] {
	t.t.guards = append(t.t.guards, guard[C]{
		name: name,
		fn:   func(c C, ev any, _ time.Time) bool { return g(c, ev) },
	})

	return t
}

// WhenChildIn restricts the transition to times when the child machine that
// the service named service runs has every one of states active. It is how
// a parent decides on where its child is without the child having to report
// each move: the core reads the snapshot the child last handed up. The
// condition fails when the child is not running. states are the child's
// state identifiers.
func (t *Transition[S, C, Ev]) WhenChildIn(service string, states ...any) *Transition[S, C, Ev] {
	if len(states) == 0 {
		t.b.errorf("state %q: WhenChildIn names no states", t.t.source.name)
	}

	cond := childCond{service: service, states: nil}
	for _, state := range states {
		cond.states = append(cond.states, fmt.Sprint(state))
	}
	t.t.childConds = append(t.t.childConds, cond)

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
		random: false,
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
		random: false,
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
		random: true,
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
		random: false,
	})

	return t
}

// Run runs chart actions, declared with [Builder.Action], as steps of the
// transition, in order with its other steps.
func (t *Transition[S, C, Ev]) Run(names ...string) *Transition[S, C, Ev] {
	for _, name := range names {
		t.t.steps = append(t.t.steps, step[C]{kind: stepRun, name: name, assign: nil, do: nil, raise: nil, random: false})
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
	t.t.steps = append(t.t.steps, step[C]{kind: stepRun, name: name, assign: nil, do: nil, raise: nil, random: false})

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
		random: false,
	})

	return t
}

// Release gives back the resource named name now, although the state that
// holds it stays active: a secret that is only needed for the first step
// inside the state, a lock that later steps must not keep. Its release
// function runs as it would on leaving the state, [Resource.From] reports
// false from then on, and the resource is not acquired again until the
// state is entered again. It does nothing when the resource is not held; an
// acquisition still in flight is released when it arrives, without its
// OnAcquired being taken.
func (t *Transition[S, C, Ev]) Release(name string) *Transition[S, C, Ev] {
	t.b.releases = append(t.b.releases, name)
	t.t.steps = append(t.t.steps, step[C]{kind: stepRelease, name: name, assign: nil, do: nil, raise: nil, random: false})

	return t
}

// Spawn starts a run of the spawned service named service under the key
// that key returns, with the context as it is at this step. It does nothing
// when a run with that key is already going, or when the service's state is
// not active: a transition that enters the state cannot spawn in the same
// step.
func (t *Transition[S, C, Ev]) Spawn(name, service string, key func(C, Ev) string) *Transition[S, C, Ev] {
	return t.keyed(stepSpawn, name, service, key)
}

// StopSpawned stops the run of the spawned service named service that has
// the key that key returns, and drops what it reports from then on. It does
// nothing when there is none.
func (t *Transition[S, C, Ev]) StopSpawned(name, service string, key func(C, Ev) string) *Transition[S, C, Ev] {
	return t.keyed(stepStop, name, service, key)
}

// SendToKey is [Transition.SendTo] for one run of a spawned service: the
// one with the key that key returns.
func (t *Transition[S, C, Ev]) SendToKey(name, service string, key func(C, Ev) string, f func(C, Ev) any) *Transition[S, C, Ev] {
	t.SendTo(name, service, f)
	t.t.keys[len(t.t.steps)-1] = func(c C, ev any) string { return key(c, event[Ev](ev)) }

	return t
}

// keyed adds a step about one run of a spawned service.
func (t *Transition[S, C, Ev]) keyed(kind stepKind, name, service string, key func(C, Ev) string) *Transition[S, C, Ev] {
	t.t.sendTargets[len(t.t.steps)] = service
	t.t.keys[len(t.t.steps)] = func(c C, ev any) string { return key(c, event[Ev](ev)) }
	t.b.spawns = append(t.b.spawns, service)
	t.t.steps = append(t.t.steps, step[C]{kind: kind, name: name, assign: nil, do: nil, raise: nil, random: false})

	return t
}

// SendTo sends the event f returns to the running service named service,
// which reads it from [Inbox]. It is how a chart talks to work in progress:
// a child machine declared with [StateBuilder.InvokeMachine], or a service
// that takes instructions while it runs. The event goes to every service of
// that name that is running once the step has settled, which includes one
// the step itself starts, so a transition into a state can greet the service
// that state invokes. A service the step stops is told first: the event is
// still in its [Inbox] after its context ends. When no such service is
// running, because its state is not active or it has finished, the event
// is dropped.
//
// Only the [Instance] and [Actor] drivers deliver such events; pgdriver
// rejects a chart that sends them, because the service runs in another
// process than the one processing the event.
func (t *Transition[S, C, Ev]) SendTo(name, service string, f func(C, Ev) any) *Transition[S, C, Ev] {
	t.t.sendTargets[len(t.t.steps)] = service
	t.b.sends = append(t.b.sends, service)

	t.t.steps = append(t.t.steps, step[C]{
		kind:   stepSend,
		name:   name,
		assign: nil,
		do:     nil,
		raise:  func(c C, ev any) any { return f(c, event[Ev](ev)) },
		random: false,
	})

	return t
}

// Emit hands the event f returns to whoever runs the machine: the function
// its driver was given with [OnEmit], the parent of a machine run with
// [StateBuilder.InvokeMachine], which receives it as an event of its own, or
// the handler pgdriver was given with its WithEmit option, which receives it
// once the step has committed. It is how a machine reports progress without
// knowing who listens. Without a listener the event is dropped.
//
// T must be a concrete type, so that a driver that stores effects can name
// the event and load it again.
func (t *Transition[S, C, Ev]) Emit[T any](name string, f func(C, Ev) T) *Transition[S, C, Ev] {
	emitted := reflect.TypeFor[T]()
	if emitted.Kind() == reflect.Interface {
		t.b.errorf("state %q: emit %q returns %s, an interface; emit a concrete type", t.t.source.name, name, emitted)
	} else {
		t.b.emitTypes = append(t.b.emitTypes, emitted)
	}

	t.b.emits = true
	t.t.steps = append(t.t.steps, step[C]{
		kind:   stepEmit,
		name:   name,
		assign: nil,
		do:     nil,
		raise:  func(c C, ev any) any { return f(c, event[Ev](ev)) },
		random: false,
	})

	return t
}

// RaiseAfter queues the event f returns for the machine itself, to be
// delivered once d has passed, like an event sent then. Unlike a delayed
// transition it belongs to no state: it is delivered wherever the chart is
// by then, unless a [Transition.CancelRaise] of the same name runs first or
// the machine finishes. The driver keeps the timer, and the event waits in
// the snapshot, so it survives a restart.
//
// The event's type must be one the chart handles with an On somewhere, so
// that a stored snapshot can name it; raising any other type panics.
func (t *Transition[S, C, Ev]) RaiseAfter(name string, d time.Duration, f func(C, Ev) any) *Transition[S, C, Ev] {
	if d < 0 {
		t.b.errorf("state %q: raise %q has a negative delay %s", t.t.source.name, name, d)
	}

	if t.t.raiseDelays == nil {
		t.t.raiseDelays = map[int]time.Duration{}
	}
	t.t.raiseDelays[len(t.t.steps)] = d
	t.b.raises[name] = true

	return t.Raise(name, f)
}

// CancelRaise drops the events that the [Transition.RaiseAfter] named name
// raised and that are still waiting. It does nothing when none is.
func (t *Transition[S, C, Ev]) CancelRaise(name string) *Transition[S, C, Ev] {
	t.t.steps = append(t.t.steps, step[C]{kind: stepCancel, name: name, assign: nil, do: nil, raise: nil, random: false})
	t.b.cancels = append(t.b.cancels, name)

	return t
}

// To sets the state the transition enters. Targeting a history state
// re-enters what its parent last had active.
//
// When the target is the source state or lies inside it, the source is not
// exited and entered again unless [Transition.Reenter] is also called.
//
// Naming further states with also enters each of them as well, which is how
// one transition puts several regions of a parallel state where it wants
// them. The targets must lie in different regions of one parallel state.
func (t *Transition[S, C, Ev]) To(target S, also ...S) *Transition[S, C, Ev] {
	if t.t.targetID != nil {
		t.b.errorf("state %q: To is called more than once on a transition", t.t.source.name)
		return t
	}

	t.t.targetID = &target
	t.t.alsoIDs = also

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

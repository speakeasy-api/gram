package sc

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"time"
)

// Steps declares what a state does as it is entered or exited, with
// [StateBuilder.Entry] and [StateBuilder.Exit]: the same steps a transition
// takes, run in the order they are declared. An assignment declared before
// an action is seen by it, and one declared after is not.
//
// The steps do not see the event that caused the entry or exit; a chart
// action run with [Steps.Run] does, in its [Move].
type Steps[S comparable, C any] struct {
	t *Transition[S, C, struct{}]
}

// steps returns the step list kept in slot for n, creating it on first use.
func (b *build[S, C]) steps(n *node[S, C], slot **transition[S, C]) *Steps[S, C] {
	if *slot == nil {
		t := new(transition[S, C])
		t.source = n
		t.trigger = triggerNone
		t.sendTargets = map[int]string{}
		t.keys = map[int]func(C, any) string{}
		*slot = t
	}

	return &Steps[S, C]{t: &Transition[S, C, struct{}]{b: b, t: *slot}}
}

// Entry returns the steps the state takes whenever it is entered, after the
// states around it and before the states inside it. For the chart itself
// they run as the instance starts.
func (s *StateBuilder[S, C]) Entry() *Steps[S, C] { return s.b.steps(s.n, &s.n.onEntry) }

// Exit returns the steps the state takes whenever it is exited, after the
// states inside it. For the chart itself, and for the states active when
// the machine finishes, they run as it finishes.
func (s *StateBuilder[S, C]) Exit() *Steps[S, C] { return s.b.steps(s.n, &s.n.onExit) }

// Entry returns the steps the final state takes when it is entered, before
// the enclosing state's completion is reported, so an assignment there is
// seen by the transitions that completion takes.
func (f *FinalBuilder[S, C]) Entry() *Steps[S, C] { return f.b.steps(f.n, &f.n.onEntry) }

// Exit returns the steps the final state takes when it is exited.
func (f *FinalBuilder[S, C]) Exit() *Steps[S, C] { return f.b.steps(f.n, &f.n.onExit) }

// Assign replaces the context with the value f returns, as
// [Transition.Assign] does.
func (s *Steps[S, C]) Assign(name string, f func(C) C) *Steps[S, C] {
	s.t.Assign(name, func(c C, _ struct{}) C { return f(c) })
	return s
}

// AssignAt is [Steps.Assign] for an assignment that needs the current time.
func (s *Steps[S, C]) AssignAt(name string, f func(c C, now time.Time) C) *Steps[S, C] {
	s.t.AssignAt(name, func(c C, _ struct{}, now time.Time) C { return f(c, now) })
	return s
}

// AssignRand is [Steps.AssignAt] for an assignment that also draws random
// numbers, as [Transition.AssignRand] does.
func (s *Steps[S, C]) AssignRand(name string, f func(c C, now time.Time, rnd *rand.Rand) C) *Steps[S, C] {
	s.t.AssignRand(name, func(c C, _ struct{}, now time.Time, rnd *rand.Rand) C { return f(c, now, rnd) })
	return s
}

// Do adds an action, as [Transition.Do] does. The name must be unique among
// the state's actions.
func (s *Steps[S, C]) Do(name string, f func(context.Context, C)) *Steps[S, C] {
	s.t.Do(name, func(ctx context.Context, c C, _ struct{}) { f(ctx, c) })
	return s
}

// Run runs chart actions, as [Transition.Run] does.
func (s *Steps[S, C]) Run(actions ...*Action[S, C]) *Steps[S, C] {
	s.t.Run(actions...)
	return s
}

// RunIf runs a chart action only when cond holds for the [Move], as
// [Transition.RunIf] does.
func (s *Steps[S, C]) RunIf(action *Action[S, C], cond func(Move[S]) bool) *Steps[S, C] {
	s.t.RunIf(action, cond)
	return s
}

// Raise queues the event f returns for the machine itself, as
// [Transition.Raise] does.
func (s *Steps[S, C]) Raise[T any](name string, f func(C) T) *Steps[S, C] {
	s.t.Raise(name, func(c C, _ struct{}) T { return f(c) })
	return s
}

// RaiseAfter raises the event f returns once d has passed, as
// [Transition.RaiseAfter] does.
func (s *Steps[S, C]) RaiseAfter[T any](raise *Delayed[S, C], d time.Duration, f func(C) T) *Steps[S, C] {
	s.t.RaiseAfter(raise, d, func(c C, _ struct{}) T { return f(c) })
	return s
}

// CancelRaise drops the events raise is still waiting to deliver, as
// [Transition.CancelRaise] does.
func (s *Steps[S, C]) CancelRaise(raise *Delayed[S, C]) *Steps[S, C] {
	s.t.CancelRaise(raise)
	return s
}

// CancelAfter cancels the pending delayed transitions of state, as
// [Transition.CancelAfter] does.
func (s *Steps[S, C]) CancelAfter(state S) *Steps[S, C] {
	s.t.CancelAfter(state)
	return s
}

// SendTo sends the message f returns to the running service to addresses, as
// [Transition.SendTo] does. On entry it reaches a service the state itself
// invokes, which is how a state greets its own child machine or gives a
// service its first instruction. On exit, a service the state itself
// invokes is told before it is stopped.
func (s *Steps[S, C]) SendTo[M any](name string, to Mailbox[M], f func(C) M) *Steps[S, C] {
	s.t.SendTo(name, to, func(c C, _ struct{}) M { return f(c) })
	return s
}

// SendToKey sends to one run of a spawned service, as
// [Transition.SendToKey] does.
func (s *Steps[S, C]) SendToKey[M any](name string, to Mailbox[M], key func(C) string, f func(C) M) *Steps[S, C] {
	s.t.SendToKey(name, to, func(c C, _ struct{}) string { return key(c) }, func(c C, _ struct{}) M { return f(c) })
	return s
}

// Spawn starts a run of a spawned service, as [Transition.Spawn] does. On
// entry the state is already active, so it can spawn its own services.
func (s *Steps[S, C]) Spawn(name string, service Spawnable[S, C], key func(C) string) *Steps[S, C] {
	s.t.Spawn(name, service, func(c C, _ struct{}) string { return key(c) })
	return s
}

// StopSpawned stops a run of a spawned service, as [Transition.StopSpawned]
// does.
func (s *Steps[S, C]) StopSpawned(name string, service Spawnable[S, C], key func(C) string) *Steps[S, C] {
	s.t.StopSpawned(name, service, func(c C, _ struct{}) string { return key(c) })
	return s
}

// Release gives a resource back early, as [Transition.Release] does.
func (s *Steps[S, C]) Release(resource Releasable[S, C]) *Steps[S, C] {
	s.t.Release(resource)
	return s
}

// Emit hands the event f returns to whoever runs the machine, as
// [Transition.Emit] does.
func (s *Steps[S, C]) Emit[T any](name string, f func(C) T) *Steps[S, C] {
	s.t.Emit(name, func(c C, _ struct{}) T { return f(c) })
	return s
}

// Action is a chart action declared with [Builder.Action] or
// [Builder.RequiresAction]. Transitions and step lists run it by this handle.
type Action[S comparable, C any] struct {
	b    *build[S, C]
	name string
}

// Name returns the action's name, by which [Binder.Action] binds it.
func (a *Action[S, C]) Name() string { return a.name }

// Delayed is a delayed event declared with [Builder.Delayed]: what
// [Transition.RaiseAfter] raises and [Transition.CancelRaise] drops.
type Delayed[S comparable, C any] struct {
	b    *build[S, C]
	name string
}

// Delayed declares a delayed event by name. Raise it with
// [Transition.RaiseAfter] or [Steps.RaiseAfter], and drop it again with
// CancelRaise, both of which take the handle.
func (b *Builder[S, C]) Delayed(name string) *Delayed[S, C] {
	if name == "" {
		b.b.errorf("a delayed event has no name")
	}
	if b.b.raises[name] {
		b.b.errorf("delayed event %q is declared twice", name)
	}
	b.b.raises[name] = true

	return &Delayed[S, C]{b: b.b, name: name}
}

// Spawnable is a service a chart can spawn runs of: the handle a declaration
// of a service marked [Invocation.Spawned], or [StateBuilder.SpawnMachine],
// returned.
type Spawnable[S comparable, C any] interface {
	invocation() *invocation[S, C]
}

func (i *Invocation[S, C, R]) invocation() *invocation[S, C] { return i.inv }

// Releasable is a resource a transition can give back early: the handle
// [StateBuilder.Holds], [StateBuilder.HoldsRequired] or
// [StateBuilder.HoldsPermit] returned.
type Releasable[S comparable, C any] interface {
	resource() *invocation[S, C]
}

func (r *Resource[S, C, R]) resource() *invocation[S, C] { return r.inv }

// owns reports whether inv was declared on this chart.
func (b *build[S, C]) owns(inv *invocation[S, C]) bool {
	if inv == nil || inv.state == nil {
		return false
	}

	return inv.state == b.root || b.nodes[inv.state.id] == inv.state
}

// Retry is [Invocation.Retry], returning the receiving handle.
func (r *Receiving[S, C, M, R]) Retry(policy Retry) *Receiving[S, C, M, R] {
	r.Invocation.Retry(policy)
	return r
}

// Spawned is [Invocation.Spawned], returning the receiving handle.
func (r *Receiving[S, C, M, R]) Spawned() *Receiving[S, C, M, R] {
	r.Invocation.Spawned()
	return r
}

// Child is the handle of a child machine declared with
// [StateBuilder.InvokeMachine] or [StateBuilder.SpawnMachine]. It routes the
// child's result, is the [Mailbox] the parent sends the child's events to,
// and reads the child's state: [Child.Snapshot] in a driver, and
// [Transition.WhenChildIn] in a guard.
type Child[S comparable, C any, S2 comparable, C2, R any] struct {
	*Receiving[S, C, any, R]

	machine *Machine[S2, C2]
}

// Retry is [Invocation.Retry], returning the child's handle.
func (c *Child[S, C, S2, C2, R]) Retry(policy Retry) *Child[S, C, S2, C2, R] {
	c.Invocation.Retry(policy)
	return c
}

// Snapshot returns the child's current snapshot, as the child last handed it
// up into parent, and reports whether there is one: false when the child is
// not running or has not yet handed up its first snapshot. For a spawned
// child it returns the first run.
func (c *Child[S, C, S2, C2, R]) Snapshot(parent Snapshot[S, C]) (Snapshot[S2, C2], bool) {
	var none Snapshot[S2, C2]

	for _, inv := range parent.invocations {
		if inv.name != c.inv.name || inv.root != c.inv.state.root || (!inv.root && inv.state != c.inv.state.id) || len(inv.saved) == 0 {
			continue
		}

		snap, _, err := c.machine.Restore(inv.saved, time.Time{})
		if err != nil {
			return none, false
		}

		return snap, true
	}

	return none, false
}

// WhenChildIn restricts the transition to times when child, a child machine
// of this chart, has every one of states active. The core reads the snapshot
// the child last handed up, so the parent decides on where its child is
// without the child reporting each move. The condition fails when the child
// is not running.
func (t *Transition[S, C, Ev]) WhenChildIn[S2 comparable, C2, R any](child *Child[S, C, S2, C2, R], states ...S2) *Transition[S, C, Ev] {
	switch {
	case child == nil || !t.b.owns(child.inv):
		t.b.errorf("state %q: WhenChildIn names a child machine of another chart", t.t.source.name)
		return t
	case len(states) == 0:
		t.b.errorf("state %q: WhenChildIn names no states", t.t.source.name)
	}

	cond := childCond[S, C]{inv: child.inv, states: nil}
	for _, state := range states {
		cond.states = append(cond.states, fmt.Sprint(state))
	}
	t.t.childConds = append(t.t.childConds, cond)

	return t
}

// Ending is the handle of a final state that declares an output with
// [FinalBuilder.Output]. It reads that output from a snapshot.
type Ending[S comparable, C, R any] struct {
	*FinalBuilder[S, C]

	output func(C) R
}

// Reached returns the output of the final state when snap has it active,
// computed from snap's context, and reports whether it does. For a
// top-level final state that is the output of the finished machine.
func (e *Ending[S, C, R]) Reached(snap Snapshot[S, C]) (R, bool) {
	var zero R

	if !snap.In(e.n.id) {
		return zero, false
	}

	return e.output(snap.Context), true
}

// raisedType records the type of an event a raise produces, to be checked
// against the transitions that could take it.
func (b *build[S, C]) raisedType(t reflect.Type) {
	if t.Kind() != reflect.Interface {
		b.raisedTypes = append(b.raisedTypes, t)
	}
}

// hasSteps reports whether an entry or exit list has any steps, so that an
// empty one asked for and left empty declares the same chart as none.
func hasSteps[S comparable, C any](t *transition[S, C]) bool {
	return t != nil && len(t.steps) > 0
}

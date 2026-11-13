package sc

import (
	"context"
	"iter"
	"reflect"
	"time"
)

// nodeKind is the structural role of a state in the chart.
type nodeKind string

const (
	// kindState is an atomic state, or a compound state when it has children.
	kindState nodeKind = "state"

	// kindParallel is a state whose children are all active together.
	kindParallel nodeKind = "parallel"

	// kindFinal is a terminal state of its parent.
	kindFinal nodeKind = "final"

	// kindHistory is a pseudo-state that stands for the states its parent was
	// last in.
	kindHistory nodeKind = "history"
)

// HistoryKind selects how much of a state's past configuration a history
// state remembers.
type HistoryKind string

const (
	// Shallow remembers only which direct child of the parent was active.
	// Re-entering it starts that child from its own initial state.
	Shallow HistoryKind = "shallow"

	// Deep remembers the exact leaf states that were active under the parent.
	Deep HistoryKind = "deep"
)

// node is one state in the chart tree.
type node[S comparable, C any] struct {
	// id is the caller's identifier. It is the zero value for the root.
	id S

	// name is the text form of id used in snapshots, effects, and exports.
	name string

	// kind is the node's structural role.
	kind nodeKind

	// root marks the implicit state that contains every top-level state.
	root bool

	// parent is nil only for the root.
	parent *node[S, C]

	// children are the child states in declaration order.
	children []*node[S, C]

	// order is the node's position in document order.
	order int

	// initialID is the declared initial child, when one was declared.
	initialID *S

	// initial is initialID resolved to a node.
	initial *node[S, C]

	// transitions are the outgoing transitions in declaration order.
	transitions []*transition[S, C]

	// entry are the actions run when the state is entered.
	entry []action[C]

	// exit are the actions run when the state is exited.
	exit []action[C]

	// invocations are the services that run while the state is active.
	invocations []*invocation[S, C]

	// historyKind applies to history nodes only.
	historyKind HistoryKind

	// defaultID is a history node's declared default target.
	defaultID *S

	// defaultNode is defaultID resolved to a node.
	defaultNode *node[S, C]

	// afterCount is the number of delayed transitions declared on the state.
	afterCount int
}

// states returns the children that are real states, skipping history nodes.
func (n *node[S, C]) states() []*node[S, C] {
	out := make([]*node[S, C], 0, len(n.children))
	for _, c := range n.children {
		if c.kind != kindHistory {
			out = append(out, c)
		}
	}

	return out
}

// atomic reports whether the node has no child states.
func (n *node[S, C]) atomic() bool {
	for _, c := range n.children {
		if c.kind != kindHistory {
			return false
		}
	}

	return n.kind != kindHistory
}

// within reports whether n is anc or one of its descendants.
func (n *node[S, C]) within(anc *node[S, C]) bool {
	for p := n; p != nil; p = p.parent {
		if p == anc {
			return true
		}
	}

	return false
}

// below reports whether n is a proper descendant of anc.
func (n *node[S, C]) below(anc *node[S, C]) bool {
	return n != anc && n.within(anc)
}

// action is a named side effect attached to a state's entry or exit.
type action[C any] struct {
	// name identifies the action within its state.
	name string

	// fn is the caller's implementation.
	fn func(context.Context, C)
}

// guard is a named condition on a transition.
type guard[C any] struct {
	// name labels the guard in exports.
	name string

	// fn is the caller's implementation with the event type erased.
	fn func(C, any) bool
}

// stepKind is the kind of work one step of a transition does.
type stepKind string

const (
	// stepAssign replaces the context.
	stepAssign stepKind = "assign"

	// stepDo emits an action effect.
	stepDo stepKind = "do"

	// stepRaise queues an event for the machine itself.
	stepRaise stepKind = "raise"
)

// step is one entry in a transition's ordered list of work.
type step[C any] struct {
	// kind selects which of the function fields is set.
	kind stepKind

	// name identifies the step.
	name string

	// assign is set for stepAssign.
	assign func(C, any) C

	// do is set for stepDo.
	do func(context.Context, C, any)

	// raise is set for stepRaise.
	raise func(C, any) any
}

// triggerKind is what causes a transition to be considered.
type triggerKind string

const (
	// triggerEvent fires on an event of the transition's event type.
	triggerEvent triggerKind = "event"

	// triggerAlways fires whenever its guard holds, without an event.
	triggerAlways triggerKind = "always"

	// triggerAfter fires when the state's timer elapses.
	triggerAfter triggerKind = "after"

	// triggerStateDone fires when the source state reaches a final state.
	triggerStateDone triggerKind = "done"

	// triggerInvokeDone fires when an invoked service returns a result.
	triggerInvokeDone triggerKind = "invoke.done"

	// triggerInvokeError fires when an invoked service fails.
	triggerInvokeError triggerKind = "invoke.error"
)

// transition is one outgoing edge of a state with its event type erased.
type transition[S comparable, C any] struct {
	// source is the state that declares the transition.
	source *node[S, C]

	// trigger is what causes the transition to be considered.
	trigger triggerKind

	// eventType is the dynamic type an event must have to match. It is nil
	// for triggers that carry no event value.
	eventType reflect.Type

	// delay is the timer duration for triggerAfter.
	delay time.Duration

	// afterIndex numbers the source state's delayed transitions.
	afterIndex int

	// invocation is the service a triggerInvoke* transition belongs to.
	invocation *invocation[S, C]

	// guards holds the transition's condition. More than one is a
	// declaration error reported by New.
	guards []guard[C]

	// steps are the assignments, actions, and raises in declared order.
	steps []step[C]

	// targetID is the declared target, when the transition has one.
	targetID *S

	// target is targetID resolved to a node.
	target *node[S, C]

	// reenter forces the source state to be exited and entered again even
	// when the target is the source or lies inside it.
	reenter bool
}

// invocation is a service that runs while its state is active.
type invocation[S comparable, C any] struct {
	// state is the state that owns the service.
	state *node[S, C]

	// name identifies the service within its state.
	name string

	// doneType is the Done[R] type the service reports on success.
	doneType reflect.Type

	// run executes the service and yields the events it produces, ending
	// with a Done or Failed event carrying id.
	run func(ctx context.Context, c C, id string) iter.Seq[any]
}

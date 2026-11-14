package sc

import (
	"context"
	"iter"
	"math/rand/v2"
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

	// entryRuns and exitRuns name chart actions run on entry and exit,
	// after the state's own entry and exit actions.
	entryRuns []string
	exitRuns  []string

	// entryConds and exitConds hold, at the same positions, the condition
	// each run needs, or nil for one that always runs.
	entryConds []func(Move[S]) bool
	exitConds  []func(Move[S]) bool

	// invocations are the services that run while the state is active.
	invocations []*invocation[S, C]

	// historyKind applies to history nodes only.
	historyKind HistoryKind

	// defaultID is a history node's declared default target.
	defaultID *S

	// defaultNode is defaultID resolved to a node.
	defaultNode *node[S, C]

	// defaultAlso are the further defaults of a history state that covers
	// several regions, and defaultNodes every default resolved to a node.
	defaultAlso  []S
	defaultNodes []*node[S, C]

	// afterCount is the number of delayed transitions declared on the state.
	afterCount int

	// budget is the time budget services in the state and its descendants
	// share, from when the state is entered; zero for none.
	budget time.Duration

	// tags are the labels declared with Tag.
	tags []string

	// output is set for a final state declared with Output: it computes
	// the completion event of the enclosing state, a Done carrying the
	// output, and outputValue the output alone.
	output      func(C) any
	outputValue func(C) any
	outputType  reflect.Type

	// built is set once the chart is complete, after which self holds
	// the state alone, regions what states returns, handles the event types the state's own
	// transitions take, catchAll whether one of them takes any event, and
	// eventless whether the state has an eventless transition.
	built     bool
	self      []*node[S, C]
	regions   []*node[S, C]
	handles   map[reflect.Type]bool
	catchAll  bool
	eventless bool

	// entryRaises and exitRaises are the events the state raises for the
	// machine itself when it is entered and exited.
	entryRaises []stateRaise[C]
	exitRaises  []stateRaise[C]

	// entryDelayed are the events the state raises for the machine itself
	// some time after it is entered.
	entryDelayed []delayedRaise[C]

	// entryAssigns and exitAssigns replace the context when the state is
	// entered and exited, before its entry and exit actions.
	entryAssigns []stateAssign[C]
	exitAssigns  []stateAssign[C]

	// entrySends and exitSends are the events the state sends to services
	// when it is entered and exited, and entryEmits and exitEmits those it
	// hands to whoever runs the machine.
	entrySends []stateSend[C]
	exitSends  []stateSend[C]
	entryEmits []stateRaise[C]
	exitEmits  []stateRaise[C]
}

// childCond is a condition on where a child machine is.
type childCond struct {
	service string
	states  []string
}

// stateSend is an event a state sends to a service on entry or exit.
type stateSend[C any] struct {
	name    string
	service string
	fn      func(C) any
}

// stateAssign is an assignment a state makes on entry or exit.
type stateAssign[C any] struct {
	name string
	fn   func(C) C
}

// delayedRaise is an event a state raises a while after it is entered, or,
// with cancel set, the cancellation of the waiting events of that name.
type delayedRaise[C any] struct {
	name   string
	delay  time.Duration
	fn     func(C) any
	cancel bool
}

// stateRaise is an event a state raises on entry or exit.
type stateRaise[C any] struct {
	name string
	fn   func(C) any
}

// reacts reports whether the state has a transition that an event of the
// given type could take, or one that needs no event. A nil type stands for
// time passing without an event.
func (n *node[S, C]) reacts(kind reflect.Type) bool {
	if n.eventless {
		return true
	}

	if kind == nil {
		return false
	}

	return n.catchAll || n.handles[kind]
}

// states returns the children that are real states, skipping history nodes.
// The result is shared once the chart is built, and must not be modified.
func (n *node[S, C]) states() []*node[S, C] {
	if n.built {
		return n.regions
	}

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

	// fn is the caller's implementation with the event type erased. It
	// receives the time the core is acting at.
	fn func(C, any, time.Time) bool
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

	// stepRun emits an action effect for a chart action declared with
	// Builder.Action.
	stepRun stepKind = "run"

	// stepCancel cancels the events a RaiseAfter of the same name raised
	// that are still waiting.
	stepCancel stepKind = "cancel"

	// stepRelease gives back a resource while its state stays active.
	stepRelease stepKind = "release"

	// stepSpawn starts a run of a spawned service, and stepStop stops
	// one.
	stepSpawn stepKind = "spawn"
	stepStop  stepKind = "stop"

	// stepCancelAfter cancels the pending delayed transitions of the state
	// the step names, which stays active.
	stepCancelAfter stepKind = "cancelAfter"

	// stepSend sends an event to a running service of the chart.
	stepSend stepKind = "send"

	// stepEmit hands an event to whoever runs the machine.
	stepEmit stepKind = "emit"
)

// Move is the step a chart action, declared with [Builder.Action], runs in.
type Move[S comparable] struct {
	// Source is the state whose transition runs the action, or the state
	// being entered or exited.
	Source S

	// From is the active states before the step.
	From []S

	// To is the active states after the step, before any eventless
	// transition that follows it.
	To []S

	// Event is the event the step processed. It is nil for a timer, an
	// eventless transition, and a state's completion.
	Event any
}

// chartAction is an action declared once for the whole chart.
type chartAction[S comparable, C any] func(ctx context.Context, c C, mv Move[S])

// step is one entry in a transition's ordered list of work.
type step[C any] struct {
	// kind selects which of the function fields is set.
	kind stepKind

	// name identifies the step.
	name string

	// assign is set for stepAssign. It receives the time the core is
	// acting at and the step's source of random numbers.
	assign func(C, any, time.Time, *rand.Rand) C

	// do is set for stepDo.
	do func(context.Context, C, any)

	// raise is set for stepRaise.
	raise func(C, any) any

	// random is set for an assignment that draws random numbers.
	random bool
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

	// triggerInvokeError fires when an invoked service fails, or a
	// resource cannot be acquired.
	triggerInvokeError triggerKind = "invoke.error"

	// triggerAcquired fires when a resource has been acquired.
	triggerAcquired triggerKind = "acquired"

	// triggerNone is the stimulus of something the core dealt with itself,
	// such as a failure it retries. No transition has it.
	triggerNone triggerKind = "none"
)

// transition is one outgoing edge of a state with its event type erased.
type transition[S comparable, C any] struct {
	// source is the state that declares the transition.
	source *node[S, C]

	// trigger is what causes the transition to be considered.
	trigger triggerKind

	// eventType is the concrete type an event must have to match: an
	// event's dynamic type is compared with it exactly. It is nil for
	// triggers that carry no event value.
	eventType reflect.Type

	// delay is the timer duration for triggerAfter.
	delay time.Duration

	// afterIndex numbers the source state's delayed transitions.
	afterIndex int

	// delayName names a delay or deadline computed from the context. It is
	// empty for a fixed delay.
	delayName string

	// delayFn computes the timer duration when the state is entered.
	delayFn func(C) time.Duration

	// deadlineFn computes when the timer fires when the state is entered.
	deadlineFn func(C) time.Time

	// jitter is the fraction by which drivers vary a delayed transition's
	// wait.
	jitter float64

	// inIDs are the states that must all be active for the transition to
	// be taken.
	inIDs []S

	// inNodes is inIDs resolved to nodes.
	inNodes []*node[S, C]

	// notInIDs are the states that must all be inactive for the
	// transition to be taken.
	notInIDs []S

	// notInNodes is notInIDs resolved to nodes.
	notInNodes []*node[S, C]

	// invocation is the service a triggerInvoke* transition belongs to.
	invocation *invocation[S, C]

	// guards are the transition's conditions; all must hold.
	guards []guard[C]

	// steps are the assignments, actions, and raises in declared order.
	steps []step[C]

	// targetID is the declared target, when the transition has one.
	targetID *S

	// alsoIDs are the further targets of a transition that enters several
	// regions of a parallel state at once, and targets is every target
	// resolved to a node, the first being target.
	alsoIDs []S
	targets []*node[S, C]

	// target is targetID resolved to a node.
	target *node[S, C]

	// reenter forces the source state to be exited and entered again even
	// when the target is the source or lies inside it.
	reenter bool

	// stay marks a transition that deliberately does nothing but consume
	// its event.
	stay bool

	// wildcard marks a triggerEvent transition declared with OnAny, which
	// matches an event of any type.
	wildcard bool

	// toChart marks a transition declared with ToChart, whose target is
	// the chart itself.
	toChart bool

	// doneWith is, for a transition declared with OnDoneWith, the type of
	// output it receives.
	doneWith reflect.Type

	// runConds holds, by step index, the condition a chart action run by
	// RunIf needs.
	runConds map[int]func(Move[S]) bool

	// raiseDelays holds, by step index, how long a RaiseAfter waits.
	raiseDelays map[int]time.Duration

	// childConds are the states child machines must be in for the
	// transition to be taken.
	childConds []childCond

	// sendTargets holds, by step index, the service a SendTo sends to, or
	// a Spawn or StopSpawned step starts or stops a run of.
	sendTargets map[int]string

	// keys holds, by step index, the function that names the spawned run a
	// step is about.
	keys map[int]func(C, any) string
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
	// with a Done or Failed event carrying id. For a resource it acquires
	// the value and yields an Acquired or Failed event.
	run func(ctx context.Context, c C, id string) iter.Seq[any]

	// resource is set for a resource declared with Holds, and nil for a
	// service.
	resource *resourceSpec[S]

	// retry is the service's retry policy, or nil for none.
	retry *Retry

	// spawned marks a service whose runs are started by Spawn steps, any
	// number at once, instead of one on entering the state.
	spawned bool

	// inboxType is the type of the messages the service receives, any for
	// a child machine, and nil for a service that receives none.
	inboxType reflect.Type

	// triggerType is set for a service declared with InvokeOn: the type of
	// the event its state must be entered with.
	triggerType reflect.Type
}

// resourceSpec is what makes an invocation a resource.
type resourceSpec[S comparable] struct {
	// key identifies the resource to its handle: its state and name.
	key string

	// release gives back an acquired value; nil when not yet bound.
	release func(ctx context.Context, value any, why Leave[S])
}

// Leave says why a resource is being released.
type Leave[S comparable] struct {
	// Move is the step that left the resource's state. It is the zero
	// value when Closed is set.
	Move Move[S]

	// Closed is set when the driver is shutting the instance down while
	// the chart is still in the resource's state. The snapshot still
	// records the resource as held, so resuming the instance acquires it
	// again.
	Closed bool
}

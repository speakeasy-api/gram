// Package sc implements statecharts: hierarchical state machines with
// parallel regions, history, timers, and invoked services.
//
// A chart is declared once with [New] and is immutable afterwards. Its core is
// pure: [Machine.Transition] takes a [Snapshot], an event and the current time,
// and returns the next snapshot together with the [Effect] values the caller
// must carry out. Nothing in the core performs I/O, reads a clock, or starts a
// goroutine, so the same chart can be driven by any driver and checked by
// [Machine.Explore].
//
// # States and events
//
// States are identified by values of a caller-chosen comparable type S,
// usually a string enum. Events are ordinary Go values dispatched on their
// dynamic type, so each event is its own struct type. The context C is the
// chart's extended state. Assignments return a new C instead of mutating one;
// maps and slices inside C still alias and must be copied before they are
// changed.
//
// A transition may have several guards, all of which must hold, and may depend
// on other active states with [Transition.WhenIn] and [Transition.WhenNotIn].
// An event that changes nothing is ignored: the snapshot comes back unchanged.
// [StateBuilder.OnAny] catches the events a state's other transitions do not
// take. [StateBuilder.Tag] labels states, so a caller can ask a snapshot what
// kind of state it is in with [Snapshot.HasTag] without naming each one.
//
// # Time
//
// The driver passes the current time into the core, which never reads a clock.
// [Transition.WhenAt] and [Transition.AssignAt] see it. [StateBuilder.After]
// waits a fixed duration, [StateBuilder.Delay] a duration computed from the
// context, and [StateBuilder.Deadline] until a time computed from it. Timers
// that are due fire inside Transition before the event, earliest first and in
// the order they started when due together, so their order never depends on
// a driver. [Machine.Advance] fires them without an event, which is how a read
// sees an expiry nothing has processed. [StateBuilder.Within] gives the
// services inside a state one shared deadline. [Transition.RaiseAfter] sends
// the chart an event after a wait, wherever it is by then.
//
// # Actions and services
//
// Actions are side effects the core returns rather than runs. A state's own
// actions are declared inline; a chart action, declared once with
// [Builder.Action], runs by name from any transition or state and is told the
// [Move] it belongs to. Services run while their state is active and report
// back as [Done] and [Failed] events. Implementations can be given later with
// [Machine.Bind], so a chart can be declared, exported and explored without the
// dependencies its services need. [Invocation.Retry] runs a failed service
// again after growing waits, so the chart sees only the failure that is
// final.
//
// # Composing machines
//
// [StateBuilder.InvokeMachine] runs another machine as a child while a state
// is active: the child starts from the parent's context and its last
// snapshot becomes the service's result. The parent sends it events with
// [Transition.SendTo], and the child reports back with [Transition.Emit],
// which the parent handles like any other event. A plain service reads what
// the chart sends it from [Inbox]. The child's snapshot is kept inside the
// parent's, so a stored parent resumes its child where it was. Any service
// can do the same for its own progress with [Checkpoint] and [Resumed], and
// [RunID] gives it an idempotency key that survives being run again.
//
// A service or child machine declared [Invocation.Spawned], or with
// [StateBuilder.SpawnMachine], runs any number of times at once, each run
// started under a key by [Transition.Spawn]: one per upload, per host, per
// job. [Group] does the same for whole instances from outside a chart. Each machine is explored on its own, the
// child standing in the parent's exploration as a service with outcomes.
//
// A state can hold a live resource, such as a connection, for as long as it
// is active: [StateBuilder.Holds] acquires it on entry and releases it on
// exit, the driver keeps the value out of the snapshot, and the services and
// actions inside the state read it with [Resource.From].
// [StateBuilder.HoldsPermit] is a resource that takes one unit of a shared
// budget without waiting, which caps how many instances are in a state at
// once. [Transition.Release] gives a resource back early, while its state
// stays active.
//
// Every guard, assignment, action, and service has a name. Names identify
// actions inside persisted effects and label the chart in [Machine.Definition],
// which exports it in the XState machine configuration format.
//
// # Drivers
//
// [Instance] runs an instance on its callers' goroutines: Send returns once
// the event and its actions are done and says whether the event was accepted,
// which suits poll loops and request handlers. [Actor] runs one on a goroutine
// of its own with a mailbox. The pgdriver subpackage keeps instances in
// PostgreSQL, inside the caller's transaction if wanted, with effects recorded
// through a pluggable sink.
//
// # Parallel states
//
// The regions of a parallel state are active at the same time, but they are
// not run concurrently. One call to [Machine.Transition] processes an event
// against every region in declaration order, which keeps the result
// deterministic. Services invoked from different regions do run concurrently,
// because drivers execute them outside the core.
//
// Regions are independent. A transition into a region exits and enters only
// that region: the others, the source's own included, keep their states,
// timers and services, and a region that re-enters itself restarts itself
// alone. That follows XState rather than SCXML, where such a transition
// leaves the whole parallel state. A transition between regions that is
// marked [Transition.Reenter] keeps SCXML's rule and restarts every region,
// and [StrictSCXML] applies SCXML's rule throughout, for imported charts.
// One transition can place several regions at once by naming a target in
// each with [Transition.To].
//
// # Conformance
//
// The core's semantics are checked against two outside sources. The SCXML
// conformance fixtures of the SCION test framework that need no scripting
// run under internal/scxmltest. XState's own tests for parallel, history,
// final and eventless behaviour are ported in the xstate_*_test.go files,
// where a comment starting "DIVERGES from XState" marks each place sc
// behaves differently on purpose.
//
// # Checking a chart
//
// [Machine.Explore] walks every state a chart can reach, on a clock of its
// own, and checks invariants on each, the way a model checker walks a
// specification. It reports the shortest timed event sequence to each broken
// rule, the states nothing moves out of, and the states from which the chart
// can no longer finish. [Report.Paths] turns the walk into tests: a shortest
// event sequence to every state found. [Machine.StartIn] places an instance directly in
// given states, for tests and for adopting work already under way.
//
// # Observability
//
// [Hooks] are called by drivers for each transition, each ignored event, and
// the start and end of each action and service run. The context an event was
// sent with reaches the hooks, so spans nest under the request that caused
// them. [JoinHooks] combines several sets. The sco11y subpackage builds Hooks
// that report to OpenTelemetry.
package sc

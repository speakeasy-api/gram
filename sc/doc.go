// Package sc implements statecharts: hierarchical state machines with
// parallel regions, history, delayed and eventless transitions, and invoked
// services.
//
// A chart is declared once with [New] and is immutable afterwards. Its core is
// pure: [Machine.Transition] takes a [Snapshot] and an event and returns the
// next snapshot together with the [Effect] values the caller must carry out.
// Nothing in the core performs I/O, reads a clock, or starts a goroutine, so
// the same chart can be driven by the in-memory [Actor], by a database-backed
// driver such as the pgdriver subpackage, or by a test that folds over a list
// of events.
//
// # States and events
//
// States are identified by values of a caller-chosen comparable type S,
// usually a string enum. Events are ordinary Go values dispatched on their
// dynamic type, so each event is its own struct type. The context C is the
// chart's extended state. Assign functions return a new C instead of mutating
// one, which keeps transitions pure; maps and slices inside C still alias and
// must be copied before they are changed.
//
// # Names
//
// Every guard, assignment, action, and service has a name. Names identify
// actions inside persisted effects and label the chart in [Machine.Definition],
// which exports it in the XState machine configuration format.
//
// # Parallel states
//
// The regions of a parallel state are active at the same time, but they are
// not run concurrently. One call to [Machine.Transition] processes an event
// against every region in declaration order, which keeps the result
// deterministic. Services invoked from different regions do run concurrently,
// because drivers execute them outside the core.
package sc

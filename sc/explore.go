package sc

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strings"
	"time"
)

// defaultExploreStart is the time an exploration starts at when it sets none.
// Its value carries no meaning; a fixed one keeps reports reproducible.
var defaultExploreStart = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// defaultMaxStates bounds an exploration that sets no limit of its own. A
// chart meant to be explored has a few hundred states at most; a count this
// large means the context is unbounded, and stopping there reports that
// instead of exhausting memory.
const defaultMaxStates = 100_000

// Outcome produces an event for one run of an invoked service, given the
// run's ID and the exploration's time when the result arrives, so a result
// can carry times relative to then, such as a certificate valid for 90
// days. [Succeeds], [Fails], and [Emits] build the usual ones.
type Outcome func(id string, now time.Time) any

// Succeeds is the outcome of a service returning result.
func Succeeds[R any](result R) Outcome {
	return func(id string, _ time.Time) any { return Done[R]{ID: id, Result: result} }
}

// Fails is the outcome of a service returning err.
func Fails(err error) Outcome {
	return func(id string, _ time.Time) any { return Failed{ID: id, Err: err} }
}

// Acquires is the outcome of a resource being acquired with value. List it
// under the resource's name in Outcomes, beside [Fails] for an acquisition
// that fails.
func Acquires[R any](value R) Outcome {
	return func(id string, _ time.Time) any { return Acquired[R]{ID: id, Value: value} }
}

// Emits is the outcome of a [StateBuilder.Stream] service yielding ev.
func Emits(ev any) Outcome {
	return func(id string, _ time.Time) any { return Emitted{ID: id, Event: ev} }
}

// Invariant is a named property that must hold in every reachable state.
// Set Holds, or HoldsAt for a property that depends on the time.
type Invariant[S comparable, C any] struct {
	// Name identifies the invariant in the report.
	Name string

	// Holds reports whether the property is true of snap.
	Holds func(snap Snapshot[S, C]) bool

	// HoldsAt reports whether the property is true of snap at now, the
	// exploration's time in that state, such as "ready only while the
	// certificate held has not expired".
	HoldsAt func(snap Snapshot[S, C], now time.Time) bool
}

// holds reports whether the invariant is true of snap at now.
func (inv Invariant[S, C]) holds(snap Snapshot[S, C], now time.Time) bool {
	if inv.Holds != nil && !inv.Holds(snap) {
		return false
	}

	return inv.HoldsAt == nil || inv.HoldsAt(snap, now)
}

// Step is one transition: during an exploration, or as a driver reports it
// to [Hooks.Transition].
type Step[S comparable, C any] struct {
	// At is the time the event was processed at.
	At time.Time

	// From is the snapshot before the event.
	From Snapshot[S, C]

	// Event is the event that was processed.
	Event any

	// To is the snapshot after the event.
	To Snapshot[S, C]

	// Effects are the effects the transition produced.
	Effects []Effect[C]
}

// Accepted reports whether the step changed the instance. It is false for
// an event that [Machine.Transition] ignored.
func (s Step[S, C]) Accepted() bool { return s.To.seq != s.From.seq }

// Service returns the service run whose result or stream event the step
// processed, as it was listed in From. It reports false for any other
// event.
func (s Step[S, C]) Service() (RunningService[S], bool) {
	var none RunningService[S]

	var id string
	switch e := s.Event.(type) {
	case invocationResult:
		id = e.invocationID()
	case Emitted:
		id = e.ID
	default:
		return none, false
	}

	for _, run := range s.From.Services() {
		if run.ID == id {
			return run, true
		}
	}

	return none, false
}

// StepInvariant is a named property that must hold of every transition, such
// as an action being emitted only from certain states.
type StepInvariant[S comparable, C any] struct {
	// Name identifies the invariant in the report.
	Name string

	// Holds reports whether the property is true of step.
	Holds func(step Step[S, C]) bool
}

// Goal is a condition the chart must always be able to get back to, such as
// a gateway being online: from every reachable state, some sequence of
// events, service outcomes and timers leads to a state where it holds. That
// is weaker than "eventually holds", which also needs the environment to
// cooperate; restrict Events and Outcomes to model what the environment is
// assumed to do.
type Goal[S comparable, C any] struct {
	// Name identifies the goal in the report.
	Name string

	// Holds reports whether the goal is met in snap.
	Holds func(snap Snapshot[S, C]) bool

	// Ignoring lists events whose types the goal leaves out entirely, such
	// as an operator closing the instance: the goal is checked only on
	// states reachable without them, and the paths back to it may not use
	// them.
	Ignoring []any
}

// Exploration describes the environment a machine is explored in and the
// properties to check.
type Exploration[S comparable, C any] struct {
	// Events are the external events tried in every reachable state.
	Events []any

	// Outcomes lists, for each service name, the outcomes tried whenever
	// that service is running. A service with no outcomes never reports,
	// which models one that hangs.
	Outcomes map[string][]Outcome

	// Invariants are checked in every reachable state.
	Invariants []Invariant[S, C]

	// Steps are checked on every transition.
	Steps []StepInvariant[S, C]

	// Goals are checked once the whole state space is known. Each goal
	// that some reachable state can never get back to is reported as a
	// violation named "goal: " followed by its name, with a shortest trace
	// to such a state. They are not checked when the exploration is
	// truncated.
	Goals []Goal[S, C]

	// MaxStates stops the exploration once this many distinct states have
	// been found. Zero selects a default of 100,000.
	MaxStates int

	// Key projects the context onto what tells one state from another.
	// Two snapshots count as the same state when their active states,
	// history, outstanding services, the time left on each pending timer,
	// and Key results are equal. Use it when the context holds values that
	// do not marshal to JSON, such as a TLS certificate, or that take
	// endlessly many values without changing what the chart can do, such as
	// timestamps. now is the exploration's clock in that state, so a key can
	// keep how far a stored time is from now instead of the time itself.
	// Its result must marshal to JSON. Nil means the whole context.
	//
	// A Key that drops something a guard or delay reads merges states that
	// behave differently, and the exploration then misses some of them.
	Key func(c C, now time.Time) any

	// Start is the time the exploration begins at. The zero value selects
	// a fixed time in the year 2000.
	Start time.Time

	// Starts are further contexts to start instances from, beside the one
	// passed to Explore, for a chart whose behaviour depends on its input.
	// The exploration covers the states reachable from any of them.
	Starts []C

	// CheckKey checks that Key only merges states that behave alike. When
	// a state's key matches one already found but its snapshot differs, it
	// tries every candidate from both and reports a violation named "key"
	// if their successors' keys differ. It costs a second expansion of every
	// merged state.
	CheckKey bool

	// CheckDeterminism runs every transition twice and reports a violation
	// named "deterministic" when the two results differ. It catches guards,
	// assignments, or delays that read the clock, draw random numbers,
	// iterate a map, or keep state of their own, which make explorations
	// and replays wrong. It doubles the exploration's cost.
	CheckDeterminism bool

	// Waits are lengths of time the exploration lets pass, with no event,
	// in every reachable state, firing the timers that fall due meanwhile.
	// Events, service outcomes and timer firings already happen at their
	// own times; waits are for guards that compare the context with the
	// current time, such as an expiry an event may arrive before or after.
	Waits []time.Duration
}

// Trace is a reachable state with the shortest sequence of events that
// leads to it from the start. Feeding Events to [Machine.Fold] from the
// snapshot [Machine.Start] makes from Initial at Start reproduces Snapshot.
type Trace[S comparable, C any] struct {
	// Start is the time the exploration started the instance at.
	Start time.Time

	// Initial is the context the instance was started with.
	Initial C

	// Events are the events in the order they were processed, each with
	// its time. An event that is nil only let time pass.
	Events []Timed

	// Snapshot is the state the events lead to.
	Snapshot Snapshot[S, C]
}

// Violation is an invariant that does not hold, with a shortest trace to a
// state or transition that breaks it.
type Violation[S comparable, C any] struct {
	// Invariant is the name of the broken invariant.
	Invariant string

	// Trace leads to the offending state. For a step invariant its last
	// event is the offending transition.
	Trace Trace[S, C]
}

// Report is the result of an exploration.
type Report[S comparable, C any] struct {
	// States is the number of distinct states found.
	States int

	// Violations holds one entry for each broken invariant.
	Violations []Violation[S, C]

	// Stuck lists the states that are not done and that no event, timer,
	// or service outcome moves out of. Whether one is a bug depends on
	// the chart: a state that waits for an event absent from Events is
	// stuck by construction.
	Stuck []Trace[S, C]

	// Truncated is set when the exploration stopped at MaxStates, which
	// means the properties were not checked on every reachable state.
	Truncated bool

	// Trapped lists the states that are not done and from which no
	// sequence of events, timers, and service outcomes reaches a top-level
	// final state, such as a retry loop with no way out. Every Stuck state
	// is also Trapped. It is nil for a chart with no top-level final state,
	// which is not meant to finish, and for a truncated exploration, where
	// it cannot be known.
	Trapped []Trace[S, C]

	reached map[S]bool

	// paths yields the shortest trace to every explored state.
	paths iter.Seq[Trace[S, C]]
}

// Paths yields a shortest trace to every state the exploration found, the
// nearest first. Each is a ready-made test: start an instance with the
// trace's Initial context at its Start time, feed it the trace's events,
// with [Machine.Fold] or through a driver with fakes for its services, and
// check that it arrives at the trace's Snapshot. Together the traces visit
// everything the chart can do within the exploration's bounds.
func (r Report[S, C]) Paths() iter.Seq[Trace[S, C]] {
	if r.paths == nil {
		return func(func(Trace[S, C]) bool) {}
	}

	return r.paths
}

// PathTo returns a shortest trace to a state for which cond holds, and
// reports whether the exploration found one.
func (r Report[S, C]) PathTo(cond func(Snapshot[S, C]) bool) (Trace[S, C], bool) {
	for tr := range r.Paths() {
		if cond(tr.Snapshot) {
			return tr, true
		}
	}

	var none Trace[S, C]

	return none, false
}

// FormatTrace describes a trace as numbered steps, one per event, naming
// each event the way [Machine.EventName] does and showing the timer or
// service run an internal event belongs to, followed by the states the trace
// ends in. Use it in test failure messages.
func (m *Machine[S, C]) FormatTrace(tr Trace[S, C]) string {
	var b strings.Builder
	for i, ev := range tr.Events {
		fmt.Fprintf(&b, "%d. +%s %s\n", i+1, ev.At.Sub(tr.Start), m.describeEvent(ev.Event))
	}
	fmt.Fprintf(&b, "ends in %v", tr.Snapshot.Active())

	return b.String()
}

// describeEvent names an event, with the ID of the timer or service run an
// internal event belongs to.
func (m *Machine[S, C]) describeEvent(ev any) string {
	switch e := ev.(type) {
	case nil:
		return "time passes"
	case TimerFired:
		return "timer " + e.ID
	case Failed:
		return fmt.Sprintf("service %s failed: %v", e.ID, e.Err)
	case Emitted:
		return fmt.Sprintf("service %s emitted %s", e.ID, m.EventName(e.Event))
	case invocationResult:
		return fmt.Sprintf("service %s done: %v", e.invocationID(), reflect.ValueOf(ev).FieldByName("Result").Interface())
	default:
		return m.EventName(ev)
	}
}

// Reached reports whether state was entered at least once during the
// exploration, including a state that an eventless transition left again
// in the same step.
func (r Report[S, C]) Reached(state S) bool { return r.reached[state] }

// explored is one discovered state with the way it was reached.
type explored[S comparable, C any] struct {
	snap   Snapshot[S, C]
	now    time.Time
	key    string
	parent int
	event  Timed

	// initial is the context a start state was created with.
	initial C
}

// edge is a step between two explored states, kept for the reachability
// checks.
type edge struct {
	from  int
	event reflect.Type
}

// Explore walks every state the machine can reach from a new instance with
// context c and checks the exploration's invariants in each, the way a model
// checker walks a specification. It runs the chart's guards and assignments
// but never its actions or services: in every state it tries each external
// event and each configured outcome of each running service at the state's
// time, fires each pending timer at its due time, and lets each of the
// exploration's waits pass.
//
// The exploration keeps its own clock, so timers fire in the order they fall
// due, as they do under a driver. A jittered timer fires at its nominal due
// time.
//
// Two snapshots count as the same state when their active states, history,
// outstanding services, the time left on each pending timer and Within
// budget, and context are equal, so the context must marshal to JSON and must take finitely many
// values for the exploration to end. A counter that only grows has to be
// capped by a guard, and stored times need a Key.
//
// The search is breadth-first, so every reported trace is a shortest one. It
// continues past violations, reporting each invariant once.
func (m *Machine[S, C]) Explore(c C, x Exploration[S, C]) (Report[S, C], error) {
	report := Report[S, C]{States: 0, Violations: nil, Stuck: nil, Truncated: false, Trapped: nil, reached: map[S]bool{}, paths: nil}

	limit := x.MaxStates
	if limit <= 0 {
		limit = defaultMaxStates
	}

	begin := x.Start
	if begin.IsZero() {
		begin = defaultExploreStart
	}

	var (
		found []explored[S, C]
		seen  = map[string]int{}

		// preds lists, for each found state, the steps that lead to it.
		preds [][]edge
	)

	for _, initial := range append([]C{c}, x.Starts...) {
		start, _, passed := m.start(initial, begin, seedFor(begin, nil))
		for _, n := range passed {
			report.reached[n.id] = true
		}

		startKey, err := m.stateKey(start, begin, x.Key)
		if err != nil {
			return report, err
		}
		if _, ok := seen[startKey]; ok {
			continue
		}
		if len(found) >= limit {
			report.Truncated = true
			continue
		}

		seen[startKey] = len(found)
		found = append(found, explored[S, C]{snap: start, now: begin, key: startKey, parent: -1, event: Timed{At: begin, Event: nil}, initial: initial})
		preds = append(preds, nil)
	}
	roots := len(found)
	broken := map[string]bool{}

	trace := func(i int) Trace[S, C] {
		var events []Timed
		at := i
		for ; found[at].parent >= 0; at = found[at].parent {
			events = append(events, found[at].event)
		}
		slices.Reverse(events)

		return Trace[S, C]{Start: begin, Initial: found[at].initial, Events: events, Snapshot: found[i].snap}
	}

	// violate reports the first violation of each invariant, and builds a
	// trace only for that one.
	violate := func(name string, tr func() Trace[S, C]) {
		if !broken[name] {
			broken[name] = true
			report.Violations = append(report.Violations, Violation[S, C]{Invariant: name, Trace: tr()})
		}
	}

	// through is the trace to found[i] followed by one more step.
	through := func(i int, ev Timed, next Snapshot[S, C]) func() Trace[S, C] {
		return func() Trace[S, C] {
			tr := trace(i)

			return Trace[S, C]{Start: begin, Initial: tr.Initial, Events: append(tr.Events, ev), Snapshot: next}
		}
	}

	check := func(i int) {
		for _, s := range found[i].snap.active {
			report.reached[s] = true
		}

		for _, inv := range x.Invariants {
			if !inv.holds(found[i].snap, found[i].now) {
				violate(inv.Name, func() Trace[S, C] { return trace(i) })
			}
		}
	}

	for i := range roots {
		check(i)
	}

	for i := 0; i < len(found); i++ {
		from := found[i]
		if from.snap.done {
			continue
		}

		moved := false

		for _, cand := range m.candidates(from.snap, from.now, x) {
			ev := cand.Timed
			next, effects, passed := m.transition(from.snap, ev.Event, ev.At)
			for _, n := range passed {
				report.reached[n.id] = true
			}

			key, err := m.stateKey(next, ev.At, x.Key)
			if err != nil {
				return report, err
			}

			if x.CheckDeterminism {
				again, againEffects, _ := m.transition(from.snap, ev.Event, ev.At)
				againKey, err := m.stateKey(again, ev.At, x.Key)
				if err != nil {
					return report, err
				}
				if againKey != key || !sameEffects(effects, againEffects) {
					violate("deterministic", through(i, ev, next))
				}
			}
			moved = moved || next.seq != from.snap.seq

			for _, inv := range x.Steps {
				if !inv.Holds(Step[S, C]{At: ev.At, From: from.snap, Event: ev.Event, To: next, Effects: effects}) {
					violate(inv.Name, through(i, ev, next))
				}
			}

			if j, ok := seen[key]; ok {
				preds[j] = append(preds[j], edge{from: i, event: reflect.TypeOf(ev.Event)})

				if x.CheckKey && !sameSnapshot(found[j].snap, next) {
					same, err := m.sameSuccessors(found[j].snap, found[j].now, next, ev.At, x)
					if err != nil {
						return report, err
					}
					if !same {
						violate("key", through(i, ev, next))
					}
				}

				continue
			}

			if len(found) >= limit {
				report.Truncated = true
				continue
			}

			seen[key] = len(found)
			var notStart C
			found = append(found, explored[S, C]{snap: next, now: ev.At, key: key, parent: i, event: ev, initial: notStart})
			preds = append(preds, []edge{{from: i, event: reflect.TypeOf(ev.Event)}})
			check(len(found) - 1)
		}

		if !moved {
			report.Stuck = append(report.Stuck, trace(i))
		}
	}

	report.States = len(found)
	report.paths = func(yield func(Trace[S, C]) bool) {
		for i := range found {
			if !yield(trace(i)) {
				return
			}
		}
	}

	// canReach marks the states from which some path, using no event of an
	// ignored type, leads to a state where target holds.
	canReach := func(target func(Snapshot[S, C]) bool, ignoring []any) []bool {
		ignored := map[reflect.Type]bool{}
		for _, ev := range ignoring {
			ignored[reflect.TypeOf(ev)] = true
		}

		reaches := make([]bool, len(found))
		var queue []int
		for i, f := range found {
			if target(f.snap) {
				reaches[i] = true
				queue = append(queue, i)
			}
		}

		for len(queue) > 0 {
			j := queue[0]
			queue = queue[1:]
			for _, e := range preds[j] {
				if !reaches[e.from] && !ignored[e.event] {
					reaches[e.from] = true
					queue = append(queue, e.from)
				}
			}
		}

		return reaches
	}

	if !report.Truncated && m.canFinish() {
		finishes := canReach(func(s Snapshot[S, C]) bool { return s.done }, nil)
		for i := range found {
			if !finishes[i] {
				report.Trapped = append(report.Trapped, trace(i))
			}
		}
	}

	if !report.Truncated {
		for _, goal := range x.Goals {
			reaches := canReach(goal.Holds, goal.Ignoring)
			inScope := reachableWithout(preds, roots, goal.Ignoring)

			// found is in breadth-first order, so the first state that
			// cannot reach the goal has a shortest trace.
			for i := range found {
				if inScope[i] && !reaches[i] {
					violate("goal: "+goal.Name, func() Trace[S, C] { return trace(i) })
					break
				}
			}
		}
	}

	return report, nil
}

// canFinish reports whether the chart has a top-level final state.
func (m *Machine[S, C]) canFinish() bool {
	finishes := func(n *node[S, C]) bool {
		return n.kind == kindFinal || slices.ContainsFunc(n.children, func(c *node[S, C]) bool { return c.kind == kindFinal })
	}

	// A chart of regions finishes when every region can.
	if m.root.kind == kindParallel {
		return !slices.ContainsFunc(m.root.states(), func(n *node[S, C]) bool { return !finishes(n) })
	}

	return slices.ContainsFunc(m.root.children, func(n *node[S, C]) bool { return n.kind == kindFinal })
}

// sameEffects reports whether two effect lists ask for the same work. The
// contexts they carry are compared through the state key instead.
func sameEffects[C any](a, b []Effect[C]) bool {
	return slices.EqualFunc(a, b, func(x, y Effect[C]) bool {
		return x.Kind == y.Kind && x.ID == y.ID && x.State == y.State && x.Name == y.Name &&
			x.Delay == y.Delay && x.At.Equal(y.At) && x.Jitter == y.Jitter
	})
}

// candidate is one thing the environment can do in a state.
type candidate struct {
	Timed

	// label names the candidate by what it is rather than by a run ID, so
	// two states that share a key name their candidates alike.
	label string
}

// candidates lists everything the environment can do in snap at now: each
// external event and service outcome now, each pending timer at its due
// time, and each wait.
func (m *Machine[S, C]) candidates(snap Snapshot[S, C], now time.Time, x Exploration[S, C]) []candidate {
	events := make([]candidate, 0, len(x.Events)+len(snap.timers)+len(x.Waits))
	add := func(at time.Time, ev any, label string) {
		events = append(events, candidate{Timed: Timed{At: at, Event: ev}, label: label})
	}

	for i, ev := range x.Events {
		add(now, ev, fmt.Sprintf("event %d", i))
	}

	for _, t := range snap.timers {
		add(later(now, t.due), TimerFired{ID: t.id}, "timer "+m.timerLabel(t))
	}

	for _, inv := range snap.invocations {
		// A held resource has nothing more to report.
		if inv.acquired {
			continue
		}

		service := m.at(inv.state, inv.root).name + "." + inv.name
		if key, spawned := SpawnKey(inv.id); spawned {
			service += fmt.Sprintf("[%q]", key)
		}

		// A service under a Within budget can only report before the
		// deadline, and fails with the deadline at it.
		deadline := m.serviceDeadline(snap, m.at(inv.state, inv.root))
		if deadline.IsZero() || now.Before(deadline) {
			for i, outcome := range x.Outcomes[inv.name] {
				add(now, outcome(inv.id, now), fmt.Sprintf("service %s outcome %d", service, i))
			}
		}
		if !deadline.IsZero() {
			add(later(now, deadline), Failed{ID: inv.id, Err: context.DeadlineExceeded}, "service "+service+" deadline")
		}
	}

	for _, d := range x.Waits {
		add(now.Add(d), nil, "wait "+d.String())
	}

	return events
}

// reachableWithout marks the states reachable from the start without any
// event of an ignored type.
func reachableWithout(preds [][]edge, roots int, ignoring []any) []bool {
	ignored := map[reflect.Type]bool{}
	for _, ev := range ignoring {
		ignored[reflect.TypeOf(ev)] = true
	}

	succs := make([][]int, len(preds))
	for j, in := range preds {
		for _, e := range in {
			if !ignored[e.event] {
				succs[e.from] = append(succs[e.from], j)
			}
		}
	}

	reached := make([]bool, len(preds))
	var queue []int
	for i := range roots {
		reached[i] = true
		queue = append(queue, i)
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		for _, j := range succs[i] {
			if !reached[j] {
				reached[j] = true
				queue = append(queue, j)
			}
		}
	}

	return reached
}

// serviceDeadline is the earliest Within deadline around state, or zero.
func (m *Machine[S, C]) serviceDeadline(snap Snapshot[S, C], state *node[S, C]) time.Time {
	deadline := snap.rootScope
	for n := state; n != nil && !n.root; n = n.parent {
		if d, ok := snap.scopes[n.id]; ok && (deadline.IsZero() || d.Before(deadline)) {
			deadline = d
		}
	}

	return deadline
}

// sameSnapshot reports whether two snapshots hold the same state apart from
// their sequence numbers and run IDs.
func sameSnapshot[S comparable, C any](a, b Snapshot[S, C]) bool {
	ja, errA := json.Marshal(a.Context)
	jb, errB := json.Marshal(b.Context)

	return errA == nil && errB == nil && string(ja) == string(jb) &&
		slices.Equal(a.active, b.active) && len(a.timers) == len(b.timers) && len(a.invocations) == len(b.invocations)
}

// sameSuccessors reports whether two states that share a key lead, through
// each candidate, to states that share a key too. Successors are matched by
// candidate, so two states whose candidates lead to each other's successors
// do not pass.
func (m *Machine[S, C]) sameSuccessors(a Snapshot[S, C], aNow time.Time, b Snapshot[S, C], bNow time.Time, x Exploration[S, C]) (bool, error) {
	keys := func(snap Snapshot[S, C], now time.Time) ([]string, error) {
		var out []string
		for _, ev := range m.candidates(snap, now, x) {
			next, _, _ := m.transition(snap, ev.Event, ev.At)
			key, err := m.stateKey(next, ev.At, x.Key)
			if err != nil {
				return nil, err
			}
			out = append(out, ev.label+" -> "+key)
		}
		slices.Sort(out)

		return out, nil
	}

	ka, err := keys(a, aNow)
	if err != nil {
		return false, err
	}
	kb, err := keys(b, bNow)
	if err != nil {
		return false, err
	}

	return slices.Equal(ka, kb), nil
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

// stateIdentity is what distinguishes one explored state from another. It
// leaves out the event count and the numbers inside timer and service run
// IDs, which grow on every visit to a state without changing what the
// machine can do next.
type stateIdentity struct {
	// Done is set once the machine reached a top-level final state.
	Done bool `json:"done"`

	// Active lists the active states.
	Active []string `json:"active"`

	// History maps each history state to the states it recorded.
	History map[string][]string `json:"history"`

	// Timers lists the delayed transitions whose timers are running, with
	// the time each has left.
	Timers []string `json:"timers"`

	// Services lists the services that have not finished.
	Services []string `json:"services"`

	// Budgets lists the active states with a Within budget, with the time
	// each has left, which decides when the services inside them fail.
	Budgets []string `json:"budgets"`

	// Context is the chart's extended state, or what the exploration's Key
	// projects it onto.
	Context any `json:"context"`
}

// timerLabel names a pending timer by what it is for rather than by its ID.
func (m *Machine[S, C]) timerLabel(t pendingTimer[S]) string {
	state := m.at(t.state, t.root).name

	switch t.kind {
	case timerAfter:
	case timerRetry:
		return fmt.Sprintf("%q retry %q %q attempt=%d", state, t.name, t.key, t.attempt)
	case timerRaise:
		// A raised event that does not marshal is caught when the
		// snapshot is stored; here its text form is enough.
		ev, _ := json.Marshal(t.raised)

		return fmt.Sprintf("%q raise %q %s %s", state, t.name, t.eventName, ev)
	}

	return fmt.Sprintf("%q.%d", state, t.index)
}

func (m *Machine[S, C]) stateKey(snap Snapshot[S, C], now time.Time, key func(C, time.Time) any) (string, error) {
	var ctx any = snap.Context
	if key != nil {
		ctx = key(snap.Context, now)
	}

	id := stateIdentity{
		Done:     snap.done,
		Active:   make([]string, 0, len(snap.active)),
		History:  make(map[string][]string, len(snap.history)),
		Timers:   make([]string, 0, len(snap.timers)),
		Services: make([]string, 0, len(snap.invocations)),
		Budgets:  make([]string, 0, len(snap.scopes)),
		Context:  ctx,
	}

	for _, s := range snap.active {
		id.Active = append(id.Active, m.nodes[s].name)
	}

	for h, recorded := range snap.history {
		names := make([]string, 0, len(recorded))
		for _, s := range recorded {
			names = append(names, m.nodes[s].name)
		}
		id.History[m.nodes[h].name] = names
	}

	for _, t := range snap.timers {
		id.Timers = append(id.Timers, fmt.Sprintf("%s in %s", m.timerLabel(t), t.due.Sub(now)))
	}
	slices.Sort(id.Timers)

	for _, inv := range snap.invocations {
		// Quoting keeps a name that ends like the marker apart from it.
		id.Services = append(id.Services, fmt.Sprintf("%q %q %q held=%t attempt=%d", m.at(inv.state, inv.root).name, inv.name, keyOf(inv.id), inv.acquired, inv.attempt))
	}
	slices.Sort(id.Services)

	// A budget that has run out behaves the same however long ago it did.
	for state, deadline := range snap.scopes {
		id.Budgets = append(id.Budgets, fmt.Sprintf("%s in %s", m.nodes[state].name, max(deadline.Sub(now), 0)))
	}
	if !snap.rootScope.IsZero() {
		id.Budgets = append(id.Budgets, fmt.Sprintf("%s in %s", m.root.name, max(snap.rootScope.Sub(now), 0)))
	}
	slices.Sort(id.Budgets)

	data, err := json.Marshal(id)
	if err != nil {
		return "", fmt.Errorf("marshal state for exploration: %w", err)
	}

	return string(data), nil
}

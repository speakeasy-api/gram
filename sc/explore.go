package sc

import (
	"encoding/json"
	"fmt"
	"slices"
)

// defaultMaxStates bounds an exploration that sets no limit of its own. A
// chart meant to be explored has a few hundred states at most; a count this
// large means the context is unbounded, and stopping there reports that
// instead of exhausting memory.
const defaultMaxStates = 100_000

// Outcome produces an event for one run of an invoked service, given the
// run's ID. [Succeeds], [Fails], and [Emits] build the usual ones.
type Outcome func(id string) any

// Succeeds is the outcome of a service returning result.
func Succeeds[R any](result R) Outcome {
	return func(id string) any { return Done[R]{ID: id, Result: result} }
}

// Fails is the outcome of a service returning err.
func Fails(err error) Outcome {
	return func(id string) any { return Failed{ID: id, Err: err} }
}

// Emits is the outcome of a [StateBuilder.Stream] service yielding ev.
func Emits(ev any) Outcome {
	return func(id string) any { return Emitted{ID: id, Event: ev} }
}

// Invariant is a named property that must hold in every reachable state.
type Invariant[S comparable, C any] struct {
	// Name identifies the invariant in the report.
	Name string

	// Holds reports whether the property is true of snap.
	Holds func(snap Snapshot[S, C]) bool
}

// Step is one transition taken during an exploration.
type Step[S comparable, C any] struct {
	// From is the snapshot before the event.
	From Snapshot[S, C]

	// Event is the event that was processed.
	Event any

	// To is the snapshot after the event.
	To Snapshot[S, C]

	// Effects are the effects the transition produced.
	Effects []Effect[C]
}

// StepInvariant is a named property that must hold of every transition, such
// as an action being emitted only from certain states.
type StepInvariant[S comparable, C any] struct {
	// Name identifies the invariant in the report.
	Name string

	// Holds reports whether the property is true of step.
	Holds func(step Step[S, C]) bool
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

	// MaxStates stops the exploration once this many distinct states have
	// been found. Zero selects a default of 100,000.
	MaxStates int
}

// Trace is a reachable state with the shortest sequence of events that
// leads to it from the start. Feeding Events to [Machine.Fold] from
// [Machine.Start] reproduces Snapshot.
type Trace[S comparable, C any] struct {
	// Events are the events in the order they were processed.
	Events []any

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

	reached map[S]bool
}

// Reached reports whether state was active in at least one explored state.
func (r Report[S, C]) Reached(state S) bool { return r.reached[state] }

// explored is one discovered state with the way it was reached.
type explored[S comparable, C any] struct {
	snap   Snapshot[S, C]
	key    string
	parent int
	event  any
}

// Explore walks every state the machine can reach from a new instance with
// context c and checks the exploration's invariants in each, the way a model
// checker walks a specification. It runs the chart's guards and assignments
// but never its actions or services: in every state it tries each external
// event, fires each pending timer, and applies each configured outcome of
// each running service.
//
// Two snapshots count as the same state when their active states, history,
// outstanding timers and services, and context are equal, so the context
// must marshal to JSON and must take finitely many values for the
// exploration to end. A counter that only grows has to be capped by a guard.
//
// The search is breadth-first, so every reported trace is a shortest one. It
// continues past violations, reporting each invariant once.
func (m *Machine[S, C]) Explore(c C, x Exploration[S, C]) (Report[S, C], error) {
	report := Report[S, C]{States: 0, Violations: nil, Stuck: nil, Truncated: false, reached: map[S]bool{}}

	limit := x.MaxStates
	if limit <= 0 {
		limit = defaultMaxStates
	}

	start, _ := m.Start(c)
	startKey, err := m.stateKey(start)
	if err != nil {
		return report, err
	}

	found := []explored[S, C]{{snap: start, key: startKey, parent: -1, event: nil}}
	seen := map[string]bool{startKey: true}
	broken := map[string]bool{}

	trace := func(i int) Trace[S, C] {
		var events []any
		for at := i; found[at].parent >= 0; at = found[at].parent {
			events = append(events, found[at].event)
		}
		slices.Reverse(events)

		return Trace[S, C]{Events: events, Snapshot: found[i].snap}
	}

	violate := func(name string, tr Trace[S, C]) {
		if !broken[name] {
			broken[name] = true
			report.Violations = append(report.Violations, Violation[S, C]{Invariant: name, Trace: tr})
		}
	}

	check := func(i int) {
		for _, s := range found[i].snap.active {
			report.reached[s] = true
		}

		for _, inv := range x.Invariants {
			if !inv.Holds(found[i].snap) {
				violate(inv.Name, trace(i))
			}
		}
	}

	check(0)

	for i := 0; i < len(found); i++ {
		from := found[i]
		if from.snap.done {
			continue
		}

		moved := false

		for _, ev := range m.candidates(from.snap, x) {
			next, effects := m.Transition(from.snap, ev)

			key, err := m.stateKey(next)
			if err != nil {
				return report, err
			}
			moved = moved || key != from.key

			for _, inv := range x.Steps {
				if !inv.Holds(Step[S, C]{From: from.snap, Event: ev, To: next, Effects: effects}) {
					tr := trace(i)
					violate(inv.Name, Trace[S, C]{Events: append(tr.Events, ev), Snapshot: next})
				}
			}

			if seen[key] {
				continue
			}

			if len(found) >= limit {
				report.Truncated = true
				continue
			}

			seen[key] = true
			found = append(found, explored[S, C]{snap: next, key: key, parent: i, event: ev})
			check(len(found) - 1)
		}

		if !moved {
			report.Stuck = append(report.Stuck, trace(i))
		}
	}

	report.States = len(found)

	return report, nil
}

// candidates lists every event the environment can deliver in snap.
func (m *Machine[S, C]) candidates(snap Snapshot[S, C], x Exploration[S, C]) []any {
	events := slices.Clone(x.Events)

	for _, t := range snap.timers {
		events = append(events, TimerFired{ID: t.id})
	}

	for _, inv := range snap.invocations {
		for _, outcome := range x.Outcomes[inv.name] {
			events = append(events, outcome(inv.id))
		}
	}

	return events
}

// stateIdentity is what distinguishes one explored state from another. It
// leaves out the event count and the numbers inside timer and service run
// IDs, which grow on every visit to a state without changing what the
// machine can do next.
type stateIdentity[C any] struct {
	// Done is set once the machine reached a top-level final state.
	Done bool `json:"done"`

	// Active lists the active states.
	Active []string `json:"active"`

	// History maps each history state to the states it recorded.
	History map[string][]string `json:"history"`

	// Timers lists the delayed transitions whose timers are running.
	Timers []string `json:"timers"`

	// Services lists the services that have not finished.
	Services []string `json:"services"`

	// Context is the chart's extended state.
	Context C `json:"context"`
}

func (m *Machine[S, C]) stateKey(snap Snapshot[S, C]) (string, error) {
	id := stateIdentity[C]{
		Done:     snap.done,
		Active:   make([]string, 0, len(snap.active)),
		History:  make(map[string][]string, len(snap.history)),
		Timers:   make([]string, 0, len(snap.timers)),
		Services: make([]string, 0, len(snap.invocations)),
		Context:  snap.Context,
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
		id.Timers = append(id.Timers, fmt.Sprintf("%s.%d", m.nodes[t.state].name, t.index))
	}
	slices.Sort(id.Timers)

	for _, inv := range snap.invocations {
		id.Services = append(id.Services, m.nodes[inv.state].name+"."+inv.name)
	}
	slices.Sort(id.Services)

	data, err := json.Marshal(id)
	if err != nil {
		return "", fmt.Errorf("marshal state for exploration: %w", err)
	}

	return string(data), nil
}

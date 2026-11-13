package sc

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"
)

// Snapshot is the complete state of one machine instance: its active states,
// its context, what its history states remember, and which timers and
// services are outstanding. It is a value; [Machine.Transition] returns a new
// one and leaves its input alone.
//
// A Snapshot marshals to JSON. Loading one back needs the machine, so use
// [Machine.Restore] instead of json.Unmarshal.
type Snapshot[S comparable, C any] struct {
	// Context is the chart's extended state.
	Context C

	machine     string
	version     int
	hash        string
	seq         uint64
	done        bool
	active      []S
	history     map[S][]S
	timers      []pendingTimer[S]
	invocations []runningInvocation[S]
	scopes      map[S]time.Time
	counter     uint64
	seed        uint64

	// tags are the tags of the active states, sorted. They follow from
	// active and are not stored.
	tags []string

	// config and nodes are the active states again, in the forms the core
	// works with: a set indexed by position in document order, and a list
	// in that order. Both are shared between snapshots and never changed.
	config []bool
	nodes  []*node[S, C]
}

// pendingTimer is a delayed transition whose timer has not fired.
type pendingTimer[S comparable] struct {
	id     string
	kind   timerKind
	state  S
	index  int
	name   string
	delay  time.Duration
	due    time.Time
	seq    uint64
	jitter float64

	// attempt is, for a retry, how many times the service has failed.
	attempt int

	// raised is a delayed raise's event, and eventName its name.
	raised    any
	eventName string

	// key and saved are what a retried run keeps from the one that failed.
	key   string
	saved json.RawMessage
}

// runningInvocation is a service run that has not reported a result.
type runningInvocation[S comparable] struct {
	id    string
	state S
	name  string

	// resource marks a resource; acquired is set once it is held.
	resource bool
	acquired bool

	// attempt is how many times a retried service failed before this run.
	attempt int

	// saved is the progress the run last recorded with Checkpoint.
	saved json.RawMessage
}

// PendingTimer is a timer that has started and not yet fired or been
// cancelled.
type PendingTimer[S comparable] struct {
	// ID is the ID of the effect that started the timer, which the
	// TimerFired event for it carries.
	ID string

	// State is the state whose delayed transition the timer belongs to,
	// whose service is waiting to be retried, or whose transition raised a
	// delayed event.
	State S

	// Name is the name of a computed delay or deadline, and empty for a
	// fixed delay. For a retry it is the service name, and for a delayed
	// event the name of the raise.
	Name string

	// Retry is set for the wait before a failed service runs again, and
	// Attempt is then how many times it has failed.
	Retry   bool
	Attempt int

	// Raised is the event a delayed raise will deliver, and nil otherwise.
	Raised any

	// Delay is how long the timer was started for; zero for a deadline.
	Delay time.Duration

	// Due is when the timer fires. [Actor.Timers] reports when the actor
	// scheduled it instead, which differs for a jittered timer.
	Due time.Time

	// Jitter is the fraction by which the driver varies the wait.
	Jitter float64
}

// RunningService is a service run that has started and not yet reported a
// result or been stopped.
type RunningService[S comparable] struct {
	// ID is the ID of the run, which its Done or Failed event carries.
	ID string

	// State is the state that invoked the service.
	State S

	// Name is the service name.
	Name string
}

// Timers returns the timers that have started and not fired, in the order
// they started.
func (s Snapshot[S, C]) Timers() []PendingTimer[S] {
	out := make([]PendingTimer[S], 0, len(s.timers))
	for _, t := range s.timers {
		out = append(out, PendingTimer[S]{ID: t.id, State: t.state, Name: t.name, Retry: t.kind == timerRetry, Attempt: t.attempt, Raised: t.raised, Delay: t.delay, Due: t.due, Jitter: t.jitter})
	}

	return out
}

// Services returns the service runs that have started and not finished, in
// the order they started.
func (s Snapshot[S, C]) Services() []RunningService[S] {
	out := make([]RunningService[S], 0, len(s.invocations))
	for _, inv := range s.invocations {
		if !inv.resource {
			out = append(out, RunningService[S]{ID: inv.id, State: inv.state, Name: inv.name})
		}
	}

	return out
}

// Checkpoint returns the progress the service run id last recorded with
// [Checkpoint], or nil when it recorded none or is not running.
func (s Snapshot[S, C]) Checkpoint(id string) json.RawMessage {
	for _, inv := range s.invocations {
		if inv.id == id {
			return inv.saved
		}
	}

	return nil
}

// HeldResource is a resource of an active state.
type HeldResource[S comparable] struct {
	// ID is the ID of the effect that acquires the resource.
	ID string

	// State is the state that holds the resource.
	State S

	// Name is the resource name.
	Name string

	// Acquired is false while the acquisition is still in flight.
	Acquired bool
}

// Resources returns the resources of the active states, in the order their
// acquisition started.
func (s Snapshot[S, C]) Resources() []HeldResource[S] {
	var out []HeldResource[S]
	for _, inv := range s.invocations {
		if inv.resource {
			out = append(out, HeldResource[S]{ID: inv.id, State: inv.state, Name: inv.name, Acquired: inv.acquired})
		}
	}

	return out
}

// Active returns every active state in document order: each active leaf
// together with all of its ancestors.
func (s Snapshot[S, C]) Active() []S { return slices.Clone(s.active) }

// In reports whether state is active, either as a leaf or as an ancestor of
// one.
func (s Snapshot[S, C]) In(state S) bool { return slices.Contains(s.active, state) }

// WithSeed returns the snapshot with the seed opt gives, from which the
// random numbers [Transition.AssignRand] receives are derived. It is how an
// instance placed with [Machine.StartIn] gets draws of its own:
//
//	snap = snap.WithSeed(sc.RandomSeed())
func (s Snapshot[S, C]) WithSeed(opt StartOption) Snapshot[S, C] {
	s.seed = seedFor(time.Time{}, []StartOption{opt})
	return s
}

// HasTag reports whether any active state was declared with tag, with
// [StateBuilder.Tag].
func (s Snapshot[S, C]) HasTag(tag string) bool { return slices.Contains(s.tags, tag) }

// Tags returns the tags of the active states, sorted, each once.
func (s Snapshot[S, C]) Tags() []string { return slices.Clone(s.tags) }

// Done reports whether the machine has reached a top-level final state.
func (s Snapshot[S, C]) Done() bool { return s.done }

// Seq returns the number of events the instance has processed. Drivers use
// it to detect a concurrent update when saving.
func (s Snapshot[S, C]) Seq() uint64 { return s.seq }

// RawSnapshot is the stored form of a [Snapshot], with states as the text
// form of their identifiers. It is what a [Migrate] hook reads and returns.
type RawSnapshot struct {
	// Machine is the ID of the machine that took the snapshot.
	Machine string `json:"machine"`

	// Version is the chart version the snapshot was taken at.
	Version int `json:"version"`

	// Hash fingerprints the chart the snapshot was taken with.
	Hash string `json:"hash"`

	// Seq is the number of events the instance has processed.
	Seq uint64 `json:"seq"`

	// Done is set once the machine reached a top-level final state.
	Done bool `json:"done"`

	// Active lists the active states.
	Active []string `json:"active"`

	// History maps each history state to the states it recorded.
	History map[string][]string `json:"history,omitempty"`

	// Timers lists the delayed transitions whose timers are running.
	Timers []RawTimer `json:"timers,omitempty"`

	// Invocations lists the service runs that have not finished.
	Invocations []RawInvocation `json:"invocations,omitempty"`

	// Scopes maps each active state that declared a budget to its
	// deadline.
	Scopes map[string]time.Time `json:"scopes,omitempty"`

	// Counter is the last number used in a timer or service run ID.
	Counter uint64 `json:"counter"`

	// Seed is the seed the instance's random numbers are derived from.
	Seed uint64 `json:"seed,omitempty"`

	// Context is the chart's extended state.
	Context json.RawMessage `json:"context"`
}

// RawTimer is the stored form of a running timer.
type RawTimer struct {
	// ID is the ID of the effect that started the timer.
	ID string `json:"id"`

	// State is the state whose delayed transition the timer belongs to.
	State string `json:"state"`

	// Index is the position of that transition among the state's delayed
	// transitions.
	Index int `json:"index"`

	// DelayNanos is how long the timer was started for, in nanoseconds.
	DelayNanos int64 `json:"delay_ns,omitempty"`

	// Due is when the timer fires. A snapshot taken before due times were
	// recorded has none, and restoring it starts the timer afresh.
	Due time.Time `json:"due,omitzero"`

	// Seq orders timers due at the same time: the lower one started first.
	Seq uint64 `json:"seq,omitempty"`

	// Kind is empty for a delayed transition, "retry" for the wait before
	// a failed service runs again, and "raise" for a delayed event.
	Kind string `json:"kind,omitempty"`

	// Name is the retried service's name, or the name of the raise.
	Name string `json:"name,omitempty"`

	// Attempt is how many times a retried service has failed.
	Attempt int `json:"attempt,omitempty"`

	// Jitter is the fraction by which the driver varies a retry's wait.
	Jitter float64 `json:"jitter,omitempty"`

	// EventName and Event are a delayed raise's event.
	EventName string          `json:"event_name,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`

	// Key and Saved are, for a retry, the spawn key and the recorded
	// progress of the run that failed.
	Key   string          `json:"key,omitempty"`
	Saved json.RawMessage `json:"saved,omitempty"`
}

// RawInvocation is the stored form of an unfinished service run.
type RawInvocation struct {
	// ID is the ID of the effect that started the run.
	ID string `json:"id"`

	// State is the state that owns the service.
	State string `json:"state"`

	// Name is the service name.
	Name string `json:"name"`

	// Resource marks a resource declared with Holds.
	Resource bool `json:"resource,omitempty"`

	// Acquired is set for a resource that was held.
	Acquired bool `json:"acquired,omitempty"`

	// Attempt is how many times a retried service failed before this run.
	Attempt int `json:"attempt,omitempty"`

	// Saved is the progress the run last recorded with Checkpoint.
	Saved json.RawMessage `json:"saved,omitempty"`
}

// MarshalJSON encodes the snapshot in the [RawSnapshot] format.
func (s Snapshot[S, C]) MarshalJSON() ([]byte, error) {
	ctx, err := json.Marshal(s.Context)
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot context: %w", err)
	}

	raw := RawSnapshot{
		Machine:     s.machine,
		Version:     s.version,
		Hash:        s.hash,
		Seq:         s.seq,
		Done:        s.done,
		Active:      make([]string, 0, len(s.active)),
		History:     make(map[string][]string, len(s.history)),
		Timers:      make([]RawTimer, 0, len(s.timers)),
		Invocations: make([]RawInvocation, 0, len(s.invocations)),
		Scopes:      make(map[string]time.Time, len(s.scopes)),
		Counter:     s.counter,
		Seed:        s.seed,
		Context:     ctx,
	}

	for _, st := range s.active {
		raw.Active = append(raw.Active, fmt.Sprint(st))
	}

	for h, recorded := range s.history {
		names := make([]string, 0, len(recorded))
		for _, st := range recorded {
			names = append(names, fmt.Sprint(st))
		}
		raw.History[fmt.Sprint(h)] = names
	}

	for _, t := range s.timers {
		rt := RawTimer{ID: t.id, State: fmt.Sprint(t.state), Index: t.index, DelayNanos: int64(t.delay), Due: t.due, Seq: t.seq, Kind: string(t.kind), Name: "", Attempt: t.attempt, Jitter: 0, EventName: "", Event: nil, Key: t.key, Saved: t.saved}

		switch t.kind {
		case timerAfter:
		case timerRetry:
			rt.Name, rt.Jitter = t.name, t.jitter
		case timerRaise:
			ev, err := json.Marshal(t.raised)
			if err != nil {
				return nil, fmt.Errorf("marshal raised event %q: %w", t.eventName, err)
			}
			rt.Name, rt.EventName, rt.Event = t.name, t.eventName, ev
		}

		raw.Timers = append(raw.Timers, rt)
	}

	for _, inv := range s.invocations {
		raw.Invocations = append(raw.Invocations, RawInvocation{ID: inv.id, State: fmt.Sprint(inv.state), Name: inv.name, Resource: inv.resource, Acquired: inv.acquired, Attempt: inv.attempt, Saved: inv.saved})
	}

	for state, deadline := range s.scopes {
		raw.Scopes[fmt.Sprint(state)] = deadline
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}

	return data, nil
}

var (
	// ErrVersionMismatch is returned by [Machine.Restore] for a snapshot
	// taken at another chart version when the machine has no [Migrate]
	// hook.
	ErrVersionMismatch = errors.New("snapshot was taken at another chart version and no migration is configured")

	// ErrChartChanged is returned by [Machine.Restore] for a snapshot whose
	// chart differs from the machine's although the versions are equal,
	// which means the chart was changed without raising its [Version].
	ErrChartChanged = errors.New("chart changed without a version change")

	// ErrInvalidSnapshot is returned by [Machine.Restore] for a snapshot
	// that does not describe a legal state of the chart.
	ErrInvalidSnapshot = errors.New("invalid snapshot")
)

// Restore loads a snapshot produced by marshalling a [Snapshot].
//
// A snapshot taken at another [Version] is passed through the [Migrate] hook
// first. Restore then checks that the result is a legal state of the chart.
// The returned effects bring the outstanding timers and services in line
// with the active states: they cancel those belonging to states that are no
// longer active and start those of states that became active. They are empty
// unless a migration changed the active states. now is the time a timer
// started by a migration counts from, and the start of a timer recorded
// without a due time. Restore fires nothing itself: pass the snapshot to
// [Machine.Advance] to bring it up to date.
func (m *Machine[S, C]) Restore(data []byte, now time.Time) (Snapshot[S, C], []Effect[C], error) {
	var zero Snapshot[S, C]

	var raw RawSnapshot
	if err := json.Unmarshal(data, &raw); err != nil {
		return zero, nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}

	if raw.Machine != m.id {
		return zero, nil, fmt.Errorf("%w: taken by machine %q, not %q", ErrInvalidSnapshot, raw.Machine, m.id)
	}

	// previously holds the states that were active before a migration. It
	// stays nil when the snapshot is not migrated.
	var previously map[string]bool

	switch {
	case raw.Version != m.version && m.migrate == nil:
		return zero, nil, fmt.Errorf("%w: snapshot version %d, chart version %d", ErrVersionMismatch, raw.Version, m.version)
	case raw.Version != m.version:
		// A stored snapshot may name only leaf states; restoring it
		// activates their ancestors too, so those count as active before.
		previously = map[string]bool{}
		for _, name := range raw.Active {
			previously[name] = true
			for n := m.names[name]; n != nil; n = n.parent {
				previously[n.name] = true
			}
		}

		migrated, err := m.migrate(raw)
		if err != nil {
			return zero, nil, fmt.Errorf("migrate snapshot from version %d: %w", raw.Version, err)
		}
		raw = migrated
	case raw.Hash != m.hash:
		return zero, nil, fmt.Errorf("%w: version %d", ErrChartChanged, m.version)
	}

	r := &run[S, C]{
		m:        m,
		config:   m.rootOnly(),
		history:  nil,
		timers:   nil,
		services: nil,
		counter:  raw.Counter,
		ctx:      zero.Context,
		done:     raw.Done,
		now:      now,
		fired:    nil,
		seed:     raw.Seed,
		stream:   raw.Seq + 1,
		rng:      nil,
		scopes:   nil,
		acquired: nil,
		pending:  nil,
		effects:  nil,
		queue:    nil,
		entered:  nil,
		passed:   nil,
		changed:  false,
		cause:    nil,
		list:     nil,
		listed:   false,
	}

	if err := json.Unmarshal(raw.Context, &r.ctx); err != nil {
		return zero, nil, fmt.Errorf("unmarshal snapshot context: %w", err)
	}

	if err := r.restoreStates(raw); err != nil {
		return zero, nil, err
	}

	for name, deadline := range raw.Scopes {
		if n, ok := m.names[name]; ok && r.config[n.order] && n.budget > 0 {
			r.setScope(n, deadline)
		}
	}

	if err := r.restoreOutstanding(raw, previously); err != nil {
		return zero, nil, err
	}

	return r.snapshot(raw.Seq), r.effects, nil
}

// restoreStates loads and checks the active and remembered states.
func (r *run[S, C]) restoreStates(raw RawSnapshot) error {
	for _, name := range raw.Active {
		n, ok := r.m.names[name]
		if !ok || n.kind == kindHistory {
			return fmt.Errorf("%w: %q is not a state of the chart", ErrInvalidSnapshot, name)
		}

		for a := n; a != nil; a = a.parent {
			r.activate(a)
		}
	}

	if err := r.checkConfig(); err != nil {
		return err
	}

	for name, recorded := range raw.History {
		h, ok := r.m.names[name]
		if !ok || h.kind != kindHistory {
			return fmt.Errorf("%w: %q is not a history state of the chart", ErrInvalidSnapshot, name)
		}

		nodes := make([]*node[S, C], 0, len(recorded))
		for _, rn := range recorded {
			n, ok := r.m.names[rn]
			if !ok || n.kind == kindHistory || !n.below(h.parent) {
				return fmt.Errorf("%w: history state %q remembers %q, which is not a state inside %q", ErrInvalidSnapshot, name, rn, h.parent.name)
			}
			nodes = append(nodes, n)
		}

		if len(nodes) > 0 {
			r.setHistory(h, nodes)
		}
	}

	return nil
}

// checkConfig reports whether the active states are a legal configuration:
// one active child under each active compound state, and every region active
// under each active parallel state.
func (r *run[S, C]) checkConfig() error {
	for _, n := range append([]*node[S, C]{r.m.root}, r.active()...) {
		activeChildren := 0
		for _, c := range n.states() {
			if r.config[c.order] {
				activeChildren++
			}
		}

		switch {
		case n.kind == kindParallel && activeChildren != len(n.states()):
			return fmt.Errorf("%w: parallel state %q does not have every region active", ErrInvalidSnapshot, n.name)
		case n.kind == kindState && !n.atomic() && activeChildren != 1:
			return fmt.Errorf("%w: state %q has %d active child states, want 1", ErrInvalidSnapshot, n.name, activeChildren)
		}
	}

	return nil
}

// StartIn returns a new instance with context c that is already in the
// given states at now, as if it had arrived there. states names leaf states,
// one per active region; their ancestors become active too. No entry action
// runs, but the timers and services of every active state start, and an
// Always transition whose guard holds is taken as it would be on arrival, so
// the returned effects are the ones a driver must carry out to keep the
// instance going.
//
// It is for tests that need an instance in a particular state, and for
// adopting work that is already under way, such as an existing row whose
// status names its state. It returns an error wrapping [ErrInvalidSnapshot]
// when the states are not a legal configuration of the chart.
//
// The instance's random seed is derived from now, so instances placed at
// the same instant make the same [Transition.AssignRand] draws. Give each
// its own with [Snapshot.WithSeed] when they must differ.
func (m *Machine[S, C]) StartIn(c C, now time.Time, states ...S) (Snapshot[S, C], []Effect[C], error) {
	var zero Snapshot[S, C]

	r := &run[S, C]{
		m:        m,
		config:   m.rootOnly(),
		history:  nil,
		timers:   nil,
		services: nil,
		counter:  0,
		ctx:      c,
		done:     false,
		now:      now,
		fired:    nil,
		seed:     seedFor(now, nil),
		stream:   0,
		rng:      nil,
		scopes:   nil,
		acquired: nil,
		pending:  nil,
		effects:  nil,
		queue:    nil,
		entered:  nil,
		passed:   nil,
		changed:  false,
		cause:    nil,
		list:     nil,
		listed:   false,
	}

	for _, s := range states {
		n, ok := m.nodes[s]
		if !ok || n.kind == kindHistory {
			return zero, nil, fmt.Errorf("%w: %q is not a state of the chart", ErrInvalidSnapshot, fmt.Sprint(s))
		}

		for a := n; a != nil; a = a.parent {
			r.activate(a)
		}
	}

	if err := r.checkConfig(); err != nil {
		return zero, nil, err
	}

	for _, n := range r.active() {
		if n.kind == kindFinal && n.parent.root {
			r.done = true
		}
	}

	if !r.done {
		for _, n := range r.active() {
			if n.budget > 0 {
				r.setScope(n, now.Add(n.budget))
			}
		}

		for _, n := range r.active() {
			for _, t := range n.transitions {
				if t.trigger == triggerAfter {
					r.startTimer(t)
				}
			}
			r.entered = append(r.entered, n)
		}
		r.startServices()
		r.settle()
		r.fireDue()
	}

	return r.snapshot(0), r.effects, nil
}

// restoreOutstanding loads the timers and service runs that still belong to
// an active state and cancels the rest. When the snapshot was migrated,
// previously is non-nil and names the states active before the migration;
// states the migration made active get the timers, services and budget that
// entering them would have started.
func (r *run[S, C]) restoreOutstanding(raw RawSnapshot, previously map[string]bool) error {
	for _, rt := range raw.Timers {
		n, ok := r.m.names[rt.State]
		kept := false

		switch {
		case timerKind(rt.Kind) == timerRaise && ok && !r.done:
			// A raised event waits whatever state the chart is in.
			t, known := r.m.eventTypes[rt.EventName]
			if !known {
				return fmt.Errorf("%w: delayed event %q is not one the chart handles", ErrInvalidSnapshot, rt.EventName)
			}

			ev := reflect.New(t)
			if err := json.Unmarshal(rt.Event, ev.Interface()); err != nil {
				return fmt.Errorf("unmarshal delayed event %q: %w", rt.EventName, err)
			}

			r.timers = append(r.timers, timer[S, C]{id: rt.ID, kind: timerRaise, t: nil, retry: nil, attempt: 0, source: n, label: rt.Name, raised: ev.Elem().Interface(), delay: time.Duration(rt.DelayNanos), due: rt.Due, seq: rt.Seq, jitter: 0, key: "", saved: nil})
			kept = true
		case timerKind(rt.Kind) == timerRetry && ok && r.config[n.order] && !r.done:
			for _, inv := range n.invocations {
				if inv.name == rt.Name && inv.retry != nil {
					r.timers = append(r.timers, timer[S, C]{id: rt.ID, kind: timerRetry, t: nil, retry: inv, attempt: rt.Attempt, source: nil, label: "", raised: nil, delay: time.Duration(rt.DelayNanos), due: rt.Due, seq: rt.Seq, jitter: rt.Jitter, key: rt.Key, saved: rt.Saved})
					kept = true
				}
			}
		case timerKind(rt.Kind) == timerAfter && ok && r.config[n.order] && !r.done:
			for _, t := range n.transitions {
				if t.trigger == triggerAfter && t.afterIndex == rt.Index {
					r.timers = append(r.timers, restoredTimer(rt.ID, t, time.Duration(rt.DelayNanos), rt.Due, rt.Seq, r.now))
					kept = true
				}
			}
		}

		if !kept {
			var zero C
			r.effects = append(r.effects, Effect[C]{Kind: EffectCancelTimer, ID: rt.ID, State: rt.State, Name: "", Delay: 0, At: time.Time{}, Jitter: 0, Context: zero, Event: nil, From: nil, To: nil, Needs: nil, Saved: nil})
		}
	}

	for _, ri := range raw.Invocations {
		n, ok := r.m.names[ri.State]
		kept := false

		if ok && r.config[n.order] && !r.done {
			for _, inv := range n.invocations {
				if inv.name == ri.Name && (inv.resource != nil) == ri.Resource {
					r.services = append(r.services, service[S, C]{id: ri.ID, inv: inv, acquired: ri.Acquired, attempt: ri.Attempt, saved: ri.Saved})
					kept = true
				}
			}
		}

		// A resource that is dropped needs no release: whatever process
		// held its value is gone.
		if !kept && !ri.Resource {
			var zero C
			r.effects = append(r.effects, Effect[C]{Kind: EffectStopInvocation, ID: ri.ID, State: ri.State, Name: ri.Name, Delay: 0, At: time.Time{}, Jitter: 0, Context: zero, Event: nil, From: nil, To: nil, Needs: nil, Saved: nil})
		}
	}

	if r.done || previously == nil {
		return nil
	}

	for _, n := range r.active() {
		if previously[n.name] {
			continue
		}

		if _, ok := r.scopes[n]; !ok && n.budget > 0 {
			r.setScope(n, r.now.Add(n.budget))
		}

		for _, t := range n.transitions {
			if t.trigger == triggerAfter {
				r.startTimer(t)
			}
		}

		for _, inv := range n.invocations {
			if !inv.spawned {
				r.startService(inv)
			}
		}
	}

	return nil
}

// Resume returns the effects that restart everything outstanding in snap:
// one [EffectStartTimer] per pending timer, carrying the time it is due,
// which may already be past, one [EffectAcquire] per resource of an active
// state, and one
// [EffectStartInvocation] per unfinished service run. Drivers that keep
// timers and services in memory call it after loading a snapshot in a new
// process. Drivers that store them durably do not need it for timers and
// services, but resource values never outlive their process: any driver
// that holds resources must acquire them again through Resume.
func (m *Machine[S, C]) Resume(snap Snapshot[S, C]) []Effect[C] {
	r := m.load(snap, time.Time{})

	effects := make([]Effect[C], 0, len(r.timers)+len(r.services))

	for _, t := range r.timers {
		effects = append(effects, timerEffect(t))
	}

	// Resources are acquired again before the services that run inside
	// them.
	for _, resources := range []bool{true, false} {
		for _, s := range r.services {
			if (s.inv.resource != nil) == resources {
				effects = append(effects, r.serviceEffect(s.id, s.inv))
			}
		}
	}

	return effects
}

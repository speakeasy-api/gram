package sc

import (
	"cmp"
	"fmt"
	"iter"
	"reflect"
	"slices"
)

// maxSettleSteps bounds the eventless transitions and internal events one
// call to Transition may process. A correct chart settles in a handful; the
// bound turns an Always or Raise cycle into a panic instead of a hang.
const maxSettleSteps = 10_000

// timer is a delayed transition whose timer is running.
type timer[S comparable, C any] struct {
	id string
	t  *transition[S, C]
}

// service is a run of an invocation that has not finished.
type service[S comparable, C any] struct {
	id  string
	inv *invocation[S, C]
}

// stimulus is the thing the machine is currently reacting to.
type stimulus[S comparable, C any] struct {
	// trigger selects which kind of transition can match.
	trigger triggerKind

	// event is the event value for triggers that carry one.
	event any

	// timer is the delayed transition whose timer fired.
	timer *transition[S, C]

	// inv is the invocation a Done or Failed event belongs to.
	inv *invocation[S, C]

	// done is the state that completed.
	done *node[S, C]
}

// on returns a stimulus with the given trigger and nothing else set.
func on[S comparable, C any](trigger triggerKind) stimulus[S, C] {
	return stimulus[S, C]{trigger: trigger, event: nil, timer: nil, inv: nil, done: nil}
}

// run is the mutable working state of one call into the core.
type run[S comparable, C any] struct {
	m *Machine[S, C]

	// config is the set of active states.
	config map[*node[S, C]]bool

	// history maps a history state to the states it recorded.
	history map[*node[S, C]][]*node[S, C]

	timers   []timer[S, C]
	services []service[S, C]

	// counter numbers timers and service runs.
	counter uint64

	ctx  C
	done bool

	effects []Effect[C]

	// queue holds raised events and state completions awaiting processing.
	queue []stimulus[S, C]

	// entered lists states entered during this call, whose services start
	// once the machine has settled.
	entered []*node[S, C]
}

func byOrder[S comparable, C any](a, b *node[S, C]) int { return cmp.Compare(a.order, b.order) }

// Start returns the snapshot of a new machine instance with context c, and
// the effects of entering its initial states.
func (m *Machine[S, C]) Start(c C) (Snapshot[S, C], []Effect[C]) {
	r := &run[S, C]{
		m:        m,
		config:   map[*node[S, C]]bool{m.root: true},
		history:  map[*node[S, C]][]*node[S, C]{},
		timers:   nil,
		services: nil,
		counter:  0,
		ctx:      c,
		done:     false,
		effects:  nil,
		queue:    nil,
		entered:  nil,
	}

	enter := new(transition[S, C])
	enter.source = m.root
	enter.trigger = triggerAlways
	enter.target = m.root.initial

	r.microstep([]*transition[S, C]{enter}, on[S, C](triggerAlways))
	r.settle()

	return r.snapshot(0), r.effects
}

// Transition processes one event and returns the next snapshot with the
// effects the driver must carry out, in order. The input snapshot is not
// modified.
//
// An event no active state handles leaves the active states and context
// unchanged. [TimerFired], [Done], [Failed], and [Emitted] events whose ID
// does not belong to a pending timer or a running service are ignored, as is
// any event sent to a machine that is done.
//
// Transition panics if snap was not produced by this machine, or if the
// chart's eventless transitions and raised events never settle.
func (m *Machine[S, C]) Transition(snap Snapshot[S, C], ev any) (Snapshot[S, C], []Effect[C]) {
	if snap.done {
		return snap, nil
	}

	r := m.load(snap)

	stim, ok := r.accept(ev)
	if !ok {
		return snap, nil
	}

	if ts := r.selectTransitions(stim); len(ts) > 0 {
		r.microstep(ts, stim)
	}
	r.settle()

	return r.snapshot(snap.seq + 1), r.effects
}

// Can reports whether ev would cause at least one transition in snap.
func (m *Machine[S, C]) Can(snap Snapshot[S, C], ev any) bool {
	if snap.done {
		return false
	}

	r := m.load(snap)

	stim, ok := r.accept(ev)

	return ok && len(r.selectTransitions(stim)) > 0
}

// Fold feeds events to the machine one at a time, starting from snap, and
// yields the snapshot and effects after each. It executes nothing, which
// makes it suited to replaying a recorded event log and to tests.
func (m *Machine[S, C]) Fold(snap Snapshot[S, C], events iter.Seq[any]) iter.Seq2[Snapshot[S, C], []Effect[C]] {
	return func(yield func(Snapshot[S, C], []Effect[C]) bool) {
		for ev := range events {
			var effects []Effect[C]
			snap, effects = m.Transition(snap, ev)
			if !yield(snap, effects) {
				return
			}
		}
	}
}

// load builds the working state for snap.
func (m *Machine[S, C]) load(snap Snapshot[S, C]) *run[S, C] {
	if snap.machine != m.id || snap.hash != m.hash {
		panic(fmt.Sprintf("sc: snapshot of machine %q (hash %.8s) used with machine %q (hash %.8s)", snap.machine, snap.hash, m.id, m.hash))
	}

	r := &run[S, C]{
		m:        m,
		config:   map[*node[S, C]]bool{m.root: true},
		history:  make(map[*node[S, C]][]*node[S, C], len(snap.history)),
		timers:   make([]timer[S, C], 0, len(snap.timers)),
		services: make([]service[S, C], 0, len(snap.invocations)),
		counter:  snap.counter,
		ctx:      snap.Context,
		done:     snap.done,
		effects:  nil,
		queue:    nil,
		entered:  nil,
	}

	for _, s := range snap.active {
		r.config[m.nodes[s]] = true
	}

	for h, recorded := range snap.history {
		nodes := make([]*node[S, C], 0, len(recorded))
		for _, s := range recorded {
			nodes = append(nodes, m.nodes[s])
		}
		r.history[m.nodes[h]] = nodes
	}

	for _, pt := range snap.timers {
		for _, t := range m.nodes[pt.state].transitions {
			if t.trigger == triggerAfter && t.afterIndex == pt.index {
				r.timers = append(r.timers, timer[S, C]{id: pt.id, t: t})
			}
		}
	}

	for _, ri := range snap.invocations {
		for _, inv := range m.nodes[ri.state].invocations {
			if inv.name == ri.name {
				r.services = append(r.services, service[S, C]{id: ri.id, inv: inv})
			}
		}
	}

	return r
}

// snapshot freezes the working state.
func (r *run[S, C]) snapshot(seq uint64) Snapshot[S, C] {
	snap := Snapshot[S, C]{
		Context:     r.ctx,
		machine:     r.m.id,
		version:     r.m.version,
		hash:        r.m.hash,
		seq:         seq,
		done:        r.done,
		active:      make([]S, 0, len(r.config)),
		history:     make(map[S][]S, len(r.history)),
		timers:      make([]pendingTimer[S], 0, len(r.timers)),
		invocations: make([]runningInvocation[S], 0, len(r.services)),
		counter:     r.counter,
	}

	for _, n := range r.active() {
		snap.active = append(snap.active, n.id)
	}

	for h, recorded := range r.history {
		ids := make([]S, 0, len(recorded))
		for _, n := range recorded {
			ids = append(ids, n.id)
		}
		snap.history[h.id] = ids
	}

	for _, t := range r.timers {
		snap.timers = append(snap.timers, pendingTimer[S]{id: t.id, state: t.t.source.id, index: t.t.afterIndex})
	}

	for _, s := range r.services {
		snap.invocations = append(snap.invocations, runningInvocation[S]{id: s.id, state: s.inv.state.id, name: s.inv.name})
	}

	return snap
}

// active returns the active states, without the root, in document order.
func (r *run[S, C]) active() []*node[S, C] {
	out := make([]*node[S, C], 0, len(r.config))
	for n := range r.config {
		if !n.root {
			out = append(out, n)
		}
	}
	slices.SortFunc(out, byOrder[S, C])

	return out
}

// accept turns an incoming event into a stimulus. It reports false for
// events that must be ignored because the timer or service run they refer to
// is no longer current.
func (r *run[S, C]) accept(ev any) (stimulus[S, C], bool) {
	var none stimulus[S, C]

	switch e := ev.(type) {
	case TimerFired:
		i := slices.IndexFunc(r.timers, func(t timer[S, C]) bool { return t.id == e.ID })
		if i < 0 {
			return none, false
		}

		t := r.timers[i].t
		r.timers = slices.Delete(r.timers, i, i+1)

		stim := on[S, C](triggerAfter)
		stim.timer = t

		return stim, true
	case Emitted:
		if !slices.ContainsFunc(r.services, func(s service[S, C]) bool { return s.id == e.ID }) {
			return none, false
		}

		stim := on[S, C](triggerEvent)
		stim.event = e.Event

		return stim, true
	case invocationResult:
		i := slices.IndexFunc(r.services, func(s service[S, C]) bool { return s.id == e.invocationID() })
		if i < 0 {
			return none, false
		}

		inv := r.services[i].inv
		r.services = slices.Delete(r.services, i, i+1)

		stim := on[S, C](triggerInvokeDone)
		if _, failed := ev.(Failed); failed {
			stim.trigger = triggerInvokeError
		}
		stim.event = ev
		stim.inv = inv

		return stim, true
	default:
		stim := on[S, C](triggerEvent)
		stim.event = ev

		return stim, true
	}
}

// matches reports whether t reacts to stim, including its guard.
func (r *run[S, C]) matches(t *transition[S, C], stim stimulus[S, C]) bool {
	if t.trigger != stim.trigger {
		return false
	}

	switch t.trigger {
	case triggerEvent:
		if reflect.TypeOf(stim.event) != t.eventType {
			return false
		}
	case triggerAfter:
		if stim.timer != t {
			return false
		}
	case triggerStateDone:
		if stim.done != t.source {
			return false
		}
	case triggerInvokeDone, triggerInvokeError:
		if stim.inv != t.invocation || reflect.TypeOf(stim.event) != t.eventType {
			return false
		}
	case triggerAlways:
	}

	for _, g := range t.guards {
		if !g.fn(r.ctx, stim.event) {
			return false
		}
	}

	return true
}

// selectTransitions picks, for each active leaf state, the first matching
// transition on the leaf or its nearest ancestor, then drops transitions
// that conflict with a higher-priority one.
func (r *run[S, C]) selectTransitions(stim stimulus[S, C]) []*transition[S, C] {
	var enabled []*transition[S, C]

	for _, leaf := range r.active() {
		if !leaf.atomic() {
			continue
		}

	ancestors:
		for n := leaf; n != nil; n = n.parent {
			for _, t := range n.transitions {
				if !r.matches(t, stim) {
					continue
				}
				if !slices.Contains(enabled, t) {
					enabled = append(enabled, t)
				}

				break ancestors
			}
		}
	}

	// Two transitions conflict when they would exit the same state. The
	// one whose source is nested inside the other's wins; otherwise the
	// one selected first, which comes from the earlier region, wins.
	var kept []*transition[S, C]
	for _, t1 := range enabled {
		preempted := false
		var displaced []*transition[S, C]

		for _, t2 := range kept {
			if !r.exitsOverlap(t1, t2) {
				continue
			}

			if t1.source.below(t2.source) {
				displaced = append(displaced, t2)
			} else {
				preempted = true
				break
			}
		}

		if preempted {
			continue
		}

		kept = slices.DeleteFunc(kept, func(t *transition[S, C]) bool { return slices.Contains(displaced, t) })
		kept = append(kept, t1)
	}

	return kept
}

func (r *run[S, C]) exitsOverlap(a, b *transition[S, C]) bool {
	exits := r.exitSet(a)
	for _, n := range r.exitSet(b) {
		if slices.Contains(exits, n) {
			return true
		}
	}

	return false
}

// domain returns the state whose descendants t exits and re-enters. It is
// nil for a transition without a target.
func (r *run[S, C]) domain(t *transition[S, C]) *node[S, C] {
	if t.target == nil {
		return nil
	}

	if !t.reenter && t.target.within(t.source) {
		return t.source
	}

	for a := t.source.parent; a != nil; a = a.parent {
		if (a.kind == kindState) && t.target.below(a) {
			return a
		}
	}

	return r.m.root
}

// exitSet returns the active states t exits.
func (r *run[S, C]) exitSet(t *transition[S, C]) []*node[S, C] {
	dom := r.domain(t)
	if dom == nil {
		return nil
	}

	var out []*node[S, C]
	for n := range r.config {
		if n.below(dom) {
			out = append(out, n)
		}
	}

	return out
}

// microstep takes a set of non-conflicting transitions together: it exits
// their states, runs their steps, and enters their targets.
func (r *run[S, C]) microstep(ts []*transition[S, C], stim stimulus[S, C]) {
	var exits []*node[S, C]
	for _, t := range ts {
		for _, n := range r.exitSet(t) {
			if !slices.Contains(exits, n) {
				exits = append(exits, n)
			}
		}
	}
	slices.SortFunc(exits, func(a, b *node[S, C]) int { return byOrder(b, a) })

	// History is recorded against the configuration as it stood before
	// any state is removed from it.
	for _, n := range exits {
		for _, h := range n.children {
			if h.kind == kindHistory {
				r.history[h] = r.record(h)
			}
		}
	}

	for _, n := range exits {
		r.exit(n)
	}

	for _, t := range ts {
		r.runSteps(t, stim)
	}

	for _, n := range r.entrySet(ts) {
		r.enter(n)
	}
}

// record returns what history state h must remember about its parent.
func (r *run[S, C]) record(h *node[S, C]) []*node[S, C] {
	var out []*node[S, C]
	for _, n := range r.active() {
		switch h.historyKind {
		case Deep:
			if n.atomic() && n.below(h.parent) {
				out = append(out, n)
			}
		case Shallow:
			if n.parent == h.parent {
				out = append(out, n)
			}
		}
	}

	return out
}

func (r *run[S, C]) exit(n *node[S, C]) {
	for _, a := range n.exit {
		r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: n.name, Name: a.name, Delay: 0, Context: r.ctx, Event: nil})
	}

	r.timers = slices.DeleteFunc(r.timers, func(t timer[S, C]) bool {
		if t.t.source != n {
			return false
		}
		r.effects = append(r.effects, r.bare(EffectCancelTimer, t.id, n, ""))

		return true
	})

	r.services = slices.DeleteFunc(r.services, func(s service[S, C]) bool {
		if s.inv.state != n {
			return false
		}
		r.effects = append(r.effects, r.bare(EffectStopInvocation, s.id, n, s.inv.name))

		return true
	})

	delete(r.config, n)
}

// bare builds an effect that carries no context or event.
func (r *run[S, C]) bare(kind EffectKind, id string, n *node[S, C], name string) Effect[C] {
	var zero C
	return Effect[C]{Kind: kind, ID: id, State: n.name, Name: name, Delay: 0, Context: zero, Event: nil}
}

func (r *run[S, C]) runSteps(t *transition[S, C], stim stimulus[S, C]) {
	for _, s := range t.steps {
		switch s.kind {
		case stepAssign:
			r.ctx = s.assign(r.ctx, stim.event)
		case stepDo:
			r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: t.source.name, Name: s.name, Delay: 0, Context: r.ctx, Event: stim.event})
		case stepRaise:
			raised := on[S, C](triggerEvent)
			raised.event = s.raise(r.ctx, stim.event)
			r.queue = append(r.queue, raised)
		}
	}
}

func (r *run[S, C]) enter(n *node[S, C]) {
	r.config[n] = true
	r.entered = append(r.entered, n)

	for _, a := range n.entry {
		r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: n.name, Name: a.name, Delay: 0, Context: r.ctx, Event: nil})
	}

	for _, t := range n.transitions {
		if t.trigger == triggerAfter {
			r.startTimer(t)
		}
	}

	if n.kind != kindFinal {
		return
	}

	parent := n.parent
	if parent.root {
		r.done = true
		return
	}

	r.queueDone(parent)

	if gp := parent.parent; gp.kind == kindParallel && r.allRegionsFinal(gp) {
		r.queueDone(gp)
	}
}

// queueDone queues the completion of n for its OnDone transitions.
func (r *run[S, C]) queueDone(n *node[S, C]) {
	stim := on[S, C](triggerStateDone)
	stim.done = n
	r.queue = append(r.queue, stim)
}

// allRegionsFinal reports whether every region of parallel state p is in a
// final state.
func (r *run[S, C]) allRegionsFinal(p *node[S, C]) bool {
	for _, region := range p.states() {
		final := false
		for _, c := range region.children {
			final = final || (c.kind == kindFinal && r.config[c])
		}
		if !final {
			return false
		}
	}

	return true
}

func (r *run[S, C]) nextID(n *node[S, C], label string) string {
	r.counter++
	return fmt.Sprintf("%s.%s#%d", n.name, label, r.counter)
}

func (r *run[S, C]) startTimer(t *transition[S, C]) {
	id := r.nextID(t.source, fmt.Sprintf("after.%d", t.afterIndex))
	r.timers = append(r.timers, timer[S, C]{id: id, t: t})

	e := r.bare(EffectStartTimer, id, t.source, "")
	e.Delay = t.delay
	r.effects = append(r.effects, e)
}

func (r *run[S, C]) startService(inv *invocation[S, C]) {
	id := r.nextID(inv.state, inv.name)
	r.services = append(r.services, service[S, C]{id: id, inv: inv})
	r.effects = append(r.effects, Effect[C]{Kind: EffectStartInvocation, ID: id, State: inv.state.name, Name: inv.name, Delay: 0, Context: r.ctx, Event: nil})
}

// entrySet returns the states ts enters, in document order.
func (r *run[S, C]) entrySet(ts []*transition[S, C]) []*node[S, C] {
	set := map[*node[S, C]]bool{}

	for _, t := range ts {
		if t.target == nil {
			continue
		}

		dom := r.domain(t)
		r.addDescendants(set, t.target)

		for _, n := range r.effectiveTargets(t.target) {
			r.addAncestors(set, n, dom)
		}

		// The domain stays active throughout, so it is never entered.
		// When it is a parallel state its other regions were exited
		// and need their default entry.
		delete(set, dom)
		r.fillRegions(set, dom)
	}

	out := make([]*node[S, C], 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	slices.SortFunc(out, byOrder[S, C])

	return out
}

// effectiveTargets resolves a history state to the states it stands for.
func (r *run[S, C]) effectiveTargets(n *node[S, C]) []*node[S, C] {
	if n.kind != kindHistory {
		return []*node[S, C]{n}
	}

	if recorded, ok := r.history[n]; ok {
		return recorded
	}

	switch {
	case n.defaultNode != nil:
		return []*node[S, C]{n.defaultNode}
	case n.parent.kind == kindParallel:
		return n.parent.states()
	default:
		return []*node[S, C]{n.parent.initial}
	}
}

// addDescendants adds n and the states entered along with it by default.
func (r *run[S, C]) addDescendants(set map[*node[S, C]]bool, n *node[S, C]) {
	if n.kind == kindHistory {
		targets := r.effectiveTargets(n)
		for _, s := range targets {
			r.addDescendants(set, s)
		}
		for _, s := range targets {
			r.addAncestors(set, s, n.parent)
		}

		return
	}

	set[n] = true

	if n.kind == kindState && n.initial != nil {
		r.addDescendants(set, n.initial)
	}

	r.fillRegions(set, n)
}

// addAncestors adds the states between n and anc, exclusive of both. It adds
// nothing when n is anc itself, as it is for a transition that targets its
// own source.
func (r *run[S, C]) addAncestors(set map[*node[S, C]]bool, n, anc *node[S, C]) {
	if !n.below(anc) {
		return
	}

	for a := n.parent; a != anc; a = a.parent {
		set[a] = true
		r.fillRegions(set, a)
	}
}

// fillRegions gives every region of parallel state p that has nothing in set
// its default entry.
func (r *run[S, C]) fillRegions(set map[*node[S, C]]bool, p *node[S, C]) {
	if p.kind != kindParallel {
		return
	}

	for _, region := range p.states() {
		covered := false
		for s := range set {
			covered = covered || s.within(region)
		}
		if !covered {
			r.addDescendants(set, region)
		}
	}
}

// settle takes eventless transitions and processes queued internal events
// until nothing more can happen, then starts the services of the states that
// ended up active.
func (r *run[S, C]) settle() {
	for steps := 0; !r.done; steps++ {
		if steps > maxSettleSteps {
			panic(fmt.Sprintf("sc: machine %q does not settle: its Always transitions or raised events form a cycle", r.m.id))
		}

		always := on[S, C](triggerAlways)
		if ts := r.selectTransitions(always); len(ts) > 0 {
			r.microstep(ts, always)
			continue
		}

		if len(r.queue) == 0 {
			break
		}

		stim := r.queue[0]
		r.queue = r.queue[1:]

		if ts := r.selectTransitions(stim); len(ts) > 0 {
			r.microstep(ts, stim)
		}
	}

	if r.done {
		for _, t := range r.timers {
			r.effects = append(r.effects, r.bare(EffectCancelTimer, t.id, t.t.source, ""))
		}
		for _, s := range r.services {
			r.effects = append(r.effects, r.bare(EffectStopInvocation, s.id, s.inv.state, s.inv.name))
		}
		r.timers = nil
		r.services = nil
	} else {
		r.startServices()
	}

	r.dropShortLivedTimers()
}

// startServices starts the services of entered states that are still active
// and not already running one.
func (r *run[S, C]) startServices() {
	slices.SortFunc(r.entered, byOrder[S, C])

	for _, n := range slices.Compact(r.entered) {
		if !r.config[n] {
			continue
		}

		for _, inv := range n.invocations {
			if !slices.ContainsFunc(r.services, func(s service[S, C]) bool { return s.inv == inv }) {
				r.startService(inv)
			}
		}
	}

	r.entered = nil
}

// dropShortLivedTimers removes the start and cancel effects of timers that
// were started and cancelled within the same call, so drivers never see
// them.
func (r *run[S, C]) dropShortLivedTimers() {
	started := map[string]bool{}
	cancelled := map[string]bool{}

	for _, e := range r.effects {
		switch {
		case e.Kind == EffectStartTimer:
			started[e.ID] = true
		case e.Kind == EffectCancelTimer && started[e.ID]:
			cancelled[e.ID] = true
		}
	}

	r.effects = slices.DeleteFunc(r.effects, func(e Effect[C]) bool {
		return (e.Kind == EffectStartTimer || e.Kind == EffectCancelTimer) && cancelled[e.ID]
	})
}

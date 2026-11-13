package sc

import (
	"cmp"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"iter"
	"math/rand/v2"
	"reflect"
	"slices"
	"time"
)

// maxSettleSteps bounds the eventless transitions and internal events one
// call to Transition may process. A correct chart settles in a handful; the
// bound turns an Always or Raise cycle into a panic instead of a hang.
const maxSettleSteps = 10_000

// timer is a delayed transition whose timer is running.
type timer[S comparable, C any] struct {
	id string
	t  *transition[S, C]

	// delay is how long the timer was started for; zero for a deadline.
	delay time.Duration

	// due is when the timer fires.
	due time.Time

	// seq orders timers that are due at the same time: the one started
	// first fires first, which within one state is declaration order.
	seq uint64
}

// latest is the latest time the timer can fire: its due time, or for a
// jittered timer the end of its jitter window. The core fires a timer by
// then even if its driver has not.
func (tm timer[S, C]) latest() time.Time {
	if tm.t.jitter == 0 {
		return tm.due
	}

	return tm.due.Add(time.Duration(tm.t.jitter * float64(tm.delay)))
}

// restoredTimer rebuilds a pending timer from what a snapshot recorded. A
// snapshot that recorded no due time is treated as started at now, which is
// how such snapshots behaved before due times were recorded.
func restoredTimer[S comparable, C any](id string, t *transition[S, C], delay time.Duration, due time.Time, seq uint64, now time.Time) timer[S, C] {
	if t.delayFn == nil && t.deadlineFn == nil {
		delay = t.delay
	}
	if due.IsZero() {
		due = now.Add(delay)
	}

	return timer[S, C]{id: id, t: t, delay: delay, due: due, seq: seq}
}

// service is a run of an invocation that has not finished.
type service[S comparable, C any] struct {
	id  string
	inv *invocation[S, C]

	// acquired is set for a resource once its value has been acquired.
	acquired bool
}

// endEffect is the effect that ends the run: a release for a resource, a
// stop for a service.
func (r *run[S, C]) endEffect(s service[S, C]) Effect[C] {
	kind := EffectStopInvocation
	if s.inv.resource != nil {
		kind = EffectRelease
	}

	return r.bare(kind, s.id, s.inv.state, s.inv.name)
}

// endServices ends the given runs: services are stopped first, then
// resources are released in the reverse of the order they started.
func (r *run[S, C]) endServices(ending []service[S, C]) {
	for _, s := range ending {
		if s.inv.resource == nil {
			r.effects = append(r.effects, r.endEffect(s))
		}
	}

	for _, s := range slices.Backward(ending) {
		if s.inv.resource != nil {
			r.effects = append(r.effects, r.endEffect(s))
		}
	}
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

	// now is the time the core is acting at: the time the driver passed
	// in, or a timer's due time while that timer fires.
	now time.Time

	// fired lists the IDs of timers that fired during this call.
	fired []string

	// seed is the instance's seed, and stream numbers this call among the
	// instance's calls; together they fix the call's random numbers.
	seed   uint64
	stream uint64

	// rng is the call's source of random numbers, made on first use.
	rng *rand.Rand

	// scopes holds the deadline of each active state that declared a
	// budget with Within.
	scopes map[*node[S, C]]time.Time

	// acquired lists the resources acquired during this call; the next
	// resource of each one's state starts once the machine has settled.
	acquired []*invocation[S, C]

	// pending holds the conditional chart action runs of the current
	// microstep, decided once its move is known.
	pending []pendingRun[S, C]

	effects []Effect[C]

	// queue holds raised events and state completions awaiting processing.
	queue []stimulus[S, C]

	// entered lists states entered during this call, whose services start
	// once the machine has settled.
	entered []*node[S, C]

	// passed lists every state entered during this call, including ones
	// left again before the machine settled.
	passed []*node[S, C]

	// changed is set once this call has changed anything: taken a
	// transition, or consumed a timer or service result.
	changed bool
}

func byOrder[S comparable, C any](a, b *node[S, C]) int { return cmp.Compare(a.order, b.order) }

// StartOption configures a new instance in [Machine.Start].
type StartOption func(*startOptions)

// startOptions holds what the StartOption values set.
type startOptions struct {
	seed    uint64
	hasSeed bool
}

// Seed sets the seed from which the instance's random numbers, those
// [Transition.AssignRand] receives, are derived. Drivers pass a random one so
// that instances differ. Without it the seed comes from the start time, so
// instances started at the same instant draw the same numbers.
func Seed(seed uint64) StartOption {
	return func(o *startOptions) { o.seed, o.hasSeed = seed, true }
}

// RandomSeed sets a seed drawn from the system's random source, which is
// what a driver passes when it starts an instance.
func RandomSeed() StartOption {
	var b [8]byte

	// crypto/rand.Read never fails on the platforms Go supports.
	_, _ = crand.Read(b[:])

	return Seed(binary.LittleEndian.Uint64(b[:]))
}

// seedFor returns the seed opts ask for, or one derived from now.
func seedFor(now time.Time, opts []StartOption) uint64 {
	o := startOptions{seed: 0, hasSeed: false}
	for _, opt := range opts {
		opt(&o)
	}

	if !o.hasSeed {
		return uint64(now.UnixNano())
	}

	return o.seed
}

// random returns the call's source of random numbers.
func (r *run[S, C]) random() *rand.Rand {
	if r.rng == nil {
		r.rng = rand.New(rand.NewPCG(r.seed, r.stream)) //nolint:gosec // G404: reproducible draws for jitter are the point; nothing here is secret.
	}

	return r.rng
}

// Start returns the snapshot of a new machine instance with context c,
// started at now, and the effects of entering its initial states.
func (m *Machine[S, C]) Start(c C, now time.Time, opts ...StartOption) (Snapshot[S, C], []Effect[C]) {
	snap, effects, _ := m.start(c, now, seedFor(now, opts))
	return snap, effects
}

// start is Start, also returning every state entered on the way.
func (m *Machine[S, C]) start(c C, now time.Time, seed uint64) (Snapshot[S, C], []Effect[C], []*node[S, C]) {
	r := &run[S, C]{
		m:        m,
		config:   map[*node[S, C]]bool{m.root: true},
		history:  map[*node[S, C]][]*node[S, C]{},
		timers:   nil,
		services: nil,
		counter:  0,
		ctx:      c,
		done:     false,
		now:      now,
		fired:    nil,
		seed:     seed,
		stream:   0,
		rng:      nil,
		scopes:   map[*node[S, C]]time.Time{},
		acquired: nil,
		pending:  nil,
		effects:  nil,
		queue:    nil,
		entered:  nil,
		passed:   nil,
		changed:  false,
	}

	enter := new(transition[S, C])
	enter.source = m.root
	enter.trigger = triggerAlways
	enter.target = m.root.initial

	r.microstep([]*transition[S, C]{enter}, on[S, C](triggerAlways))
	r.settle()
	r.fireDue()

	return r.snapshot(0), r.effects, r.passed
}

// Transition processes one event at time now and returns the next snapshot
// with the effects the driver must carry out, in order. The input snapshot
// is not modified.
//
// The core never reads a clock: now is the driver's. Guards and assignments
// declared with [Transition.WhenAt] and [Transition.AssignAt] see it, and
// timers count from it. Before the event, every timer due by now fires,
// earliest first, and timers due at the same time fire in the order they
// started; each sees now as its own due time. Timers that the event's
// transitions start and that are already due fire before Transition
// returns, the same way. A jittered timer normally fires when the driver
// delivers its [TimerFired], somewhere inside its jitter window, and fires
// here once the window has ended. A TimerFired for a pending timer fires it
// whatever its due time, because the driver's clock is the authority.
//
// An event that changes nothing is ignored: Transition returns snap itself,
// with the same [Snapshot.Seq], and no effects. That covers an event no
// active state handles or whose guards all fail, [TimerFired], [Done],
// [Failed], and [Emitted] events whose ID does not belong to a pending timer
// or a running service, and any event sent to a machine that is done. A
// service result whose transitions all have failing guards still ends the
// service run, so it is not ignored.
//
// Transition panics if snap was not produced by this machine, or if the
// chart's eventless transitions and raised events never settle.
func (m *Machine[S, C]) Transition(snap Snapshot[S, C], ev any, now time.Time) (Snapshot[S, C], []Effect[C]) {
	next, effects, _ := m.transition(snap, ev, now)
	return next, effects
}

// transition is Transition, also returning every state entered on the way.
// A nil ev only advances time.
func (m *Machine[S, C]) transition(snap Snapshot[S, C], ev any, now time.Time) (Snapshot[S, C], []Effect[C], []*node[S, C]) {
	if snap.done {
		return snap, nil, nil
	}

	r := m.load(snap, now)
	r.fireDue()

	if ev != nil && !r.done {
		if stim, ok := r.accept(ev); ok {
			if ts := r.selectTransitions(stim); len(ts) > 0 {
				r.microstep(ts, stim)
			}
			r.settle()
			r.fireDue()
		}
	}

	if !r.changed {
		return snap, nil, nil
	}

	return r.snapshot(snap.seq + 1), r.effects, r.passed
}

// Advance returns the snapshot as of now: every timer due by now fires, as
// [Machine.Transition] fires them before an event. It returns snap itself
// when nothing is due. Use it to read an instance's current state without
// waiting for its driver, such as an invitation that expired while nothing
// ran, or to move time on in a test.
func (m *Machine[S, C]) Advance(snap Snapshot[S, C], now time.Time) (Snapshot[S, C], []Effect[C]) {
	next, effects, _ := m.transition(snap, nil, now)
	return next, effects
}

// Can reports whether ev, sent at now, would cause at least one transition
// in snap, once the timers due by now have fired.
func (m *Machine[S, C]) Can(snap Snapshot[S, C], ev any, now time.Time) bool {
	if snap.done {
		return false
	}

	r := m.load(snap, now)
	r.fireDue()
	if r.done {
		return false
	}

	stim, ok := r.accept(ev)

	return ok && len(r.selectTransitions(stim)) > 0
}

// Timed is an event with the time it happened.
type Timed struct {
	// At is when the event happened.
	At time.Time

	// Event is the event. Nil means only that time passed, which fires the
	// timers due by At.
	Event any
}

// Fold feeds events to the machine one at a time, starting from snap, and
// yields the snapshot and effects after each. It executes nothing, which
// makes it suited to replaying a recorded event log and to tests.
func (m *Machine[S, C]) Fold(snap Snapshot[S, C], events iter.Seq[Timed]) iter.Seq2[Snapshot[S, C], []Effect[C]] {
	return func(yield func(Snapshot[S, C], []Effect[C]) bool) {
		for ev := range events {
			var effects []Effect[C]
			snap, effects, _ = m.transition(snap, ev.Event, ev.At)
			if !yield(snap, effects) {
				return
			}
		}
	}
}

// load builds the working state for snap, acting at now.
func (m *Machine[S, C]) load(snap Snapshot[S, C], now time.Time) *run[S, C] {
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
		now:      now,
		fired:    nil,
		seed:     snap.seed,
		stream:   snap.seq + 1,
		rng:      nil,
		scopes:   map[*node[S, C]]time.Time{},
		acquired: nil,
		pending:  nil,
		effects:  nil,
		queue:    nil,
		entered:  nil,
		passed:   nil,
		changed:  false,
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
				r.timers = append(r.timers, restoredTimer(pt.id, t, pt.delay, pt.due, pt.seq, now))
			}
		}
	}

	for s, deadline := range snap.scopes {
		r.scopes[m.nodes[s]] = deadline
	}

	for _, ri := range snap.invocations {
		for _, inv := range m.nodes[ri.state].invocations {
			if inv.name == ri.name {
				r.services = append(r.services, service[S, C]{id: ri.id, inv: inv, acquired: ri.acquired})
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
		seed:        r.seed,
		done:        r.done,
		active:      make([]S, 0, len(r.config)),
		history:     make(map[S][]S, len(r.history)),
		timers:      make([]pendingTimer[S], 0, len(r.timers)),
		invocations: make([]runningInvocation[S], 0, len(r.services)),
		scopes:      make(map[S]time.Time, len(r.scopes)),
		counter:     r.counter,
	}

	for n, deadline := range r.scopes {
		snap.scopes[n.id] = deadline
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
		snap.timers = append(snap.timers, pendingTimer[S]{
			id: t.id, state: t.t.source.id, index: t.t.afterIndex, name: t.t.delayName,
			delay: t.delay, due: t.due, seq: t.seq, jitter: t.t.jitter,
		})
	}

	for _, s := range r.services {
		snap.invocations = append(snap.invocations, runningInvocation[S]{id: s.id, state: s.inv.state.id, name: s.inv.name, resource: s.inv.resource != nil, acquired: s.acquired})
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
		r.changed = true
		r.fired = append(r.fired, e.ID)

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
	case acquisition:
		id, _ := e.acquisition()
		i := slices.IndexFunc(r.services, func(s service[S, C]) bool { return s.id == id })

		// An acquisition of a resource that is already held is the
		// driver acquiring it again after a restart; nothing changes.
		if i < 0 || r.services[i].acquired {
			return none, false
		}

		r.services[i].acquired = true
		r.acquired = append(r.acquired, r.services[i].inv)
		r.changed = true

		stim := on[S, C](triggerAcquired)
		stim.event = ev
		stim.inv = r.services[i].inv

		return stim, true
	case invocationResult:
		i := slices.IndexFunc(r.services, func(s service[S, C]) bool { return s.id == e.invocationID() })
		if i < 0 {
			return none, false
		}

		inv := r.services[i].inv
		r.services = slices.Delete(r.services, i, i+1)
		r.changed = true

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
	case triggerInvokeDone, triggerInvokeError, triggerAcquired:
		if stim.inv != t.invocation || reflect.TypeOf(stim.event) != t.eventType {
			return false
		}
	case triggerAlways:
	}

	for _, in := range t.inNodes {
		if !r.config[in] {
			return false
		}
	}

	for _, out := range t.notInNodes {
		if r.config[out] {
			return false
		}
	}

	for _, g := range t.guards {
		if !g.fn(r.ctx, stim.event, r.now) {
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
	r.changed = true

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

	before := r.activeNames()
	first := len(r.effects)
	r.pending = r.pending[:0]

	for _, n := range exits {
		r.exit(n, stim.event)
	}

	for _, t := range ts {
		r.runSteps(t, stim)
	}

	for _, n := range r.entrySet(ts) {
		r.enter(n, stim.event)
	}

	// Every action this step produced is told the move it belongs to.
	after := r.activeNames()
	for i := first; i < len(r.effects); i++ {
		if r.effects[i].Kind == EffectAction || r.effects[i].Kind == EffectRelease {
			r.effects[i].From = before
			r.effects[i].To = after
		}
		if r.effects[i].Kind == EffectRelease {
			r.effects[i].Event = stim.event
		}
	}

	// Conditional runs are kept only when their condition holds for it.
	if len(r.pending) > 0 {
		from, to := r.m.stateIDs(before), r.m.stateIDs(after)
		drop := map[int]bool{}
		for _, p := range r.pending {
			if !p.cond(Move[S]{Source: p.source.id, From: from, To: to, Event: stim.event}) {
				drop[p.index] = true
			}
		}

		kept := r.effects[:first]
		for i := first; i < len(r.effects); i++ {
			if !drop[i] {
				kept = append(kept, r.effects[i])
			}
		}
		r.effects = kept
		r.pending = r.pending[:0]
	}
}

// activeNames returns the names of the active states in document order.
func (r *run[S, C]) activeNames() []string {
	active := r.active()
	names := make([]string, 0, len(active))
	for _, n := range active {
		names = append(names, n.name)
	}

	return names
}

// pendingRun is a conditional chart action run whose effect is kept only if
// its condition holds for the move it belongs to.
type pendingRun[S comparable, C any] struct {
	index  int
	source *node[S, C]
	cond   func(Move[S]) bool
}

// emitRun adds the effect of running a chart action, to be decided at the
// end of the microstep when it has a condition.
func (r *run[S, C]) emitRun(source *node[S, C], name string, event any, cond func(Move[S]) bool) {
	r.effects = append(r.effects, r.chartActionEffect(source, name, event))
	if cond != nil {
		r.pending = append(r.pending, pendingRun[S, C]{index: len(r.effects) - 1, source: source, cond: cond})
	}
}

// chartActionEffect is the effect of running a chart action.
func (r *run[S, C]) chartActionEffect(source *node[S, C], name string, event any) Effect[C] {
	return Effect[C]{Kind: EffectAction, ID: "", State: source.name, Name: name, Delay: 0, At: time.Time{}, Jitter: 0, Context: r.ctx, Event: event, From: nil, To: nil, Needs: nil}
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

func (r *run[S, C]) exit(n *node[S, C], event any) {
	for _, a := range n.exit {
		r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: n.name, Name: a.name, Delay: 0, At: time.Time{}, Jitter: 0, Context: r.ctx, Event: nil, From: nil, To: nil, Needs: nil})
	}
	for k, name := range n.exitRuns {
		r.emitRun(n, name, event, n.exitConds[k])
	}

	r.timers = slices.DeleteFunc(r.timers, func(t timer[S, C]) bool {
		if t.t.source != n {
			return false
		}
		r.effects = append(r.effects, r.bare(EffectCancelTimer, t.id, n, ""))

		return true
	})

	delete(r.scopes, n)

	var ending []service[S, C]
	r.services = slices.DeleteFunc(r.services, func(s service[S, C]) bool {
		if s.inv.state == n {
			ending = append(ending, s)
		}

		return s.inv.state == n
	})
	r.endServices(ending)

	delete(r.config, n)
}

// bare builds an effect that carries no context or event.
func (r *run[S, C]) bare(kind EffectKind, id string, n *node[S, C], name string) Effect[C] {
	var zero C
	return Effect[C]{Kind: kind, ID: id, State: n.name, Name: name, Delay: 0, At: time.Time{}, Jitter: 0, Context: zero, Event: nil, From: nil, To: nil, Needs: nil}
}

func (r *run[S, C]) runSteps(t *transition[S, C], stim stimulus[S, C]) {
	for k, s := range t.steps {
		switch s.kind {
		case stepAssign:
			r.ctx = s.assign(r.ctx, stim.event, r.now, r.random())
		case stepDo:
			r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: t.source.name, Name: s.name, Delay: 0, At: time.Time{}, Jitter: 0, Context: r.ctx, Event: stim.event, From: nil, To: nil, Needs: nil})
		case stepRun:
			r.emitRun(t.source, s.name, stim.event, t.runConds[k])
		case stepRaise:
			raised := on[S, C](triggerEvent)
			raised.event = s.raise(r.ctx, stim.event)
			r.queue = append(r.queue, raised)
		}
	}
}

func (r *run[S, C]) enter(n *node[S, C], event any) {
	r.config[n] = true
	if n.budget > 0 {
		r.scopes[n] = r.now.Add(n.budget)
	}
	r.entered = append(r.entered, n)
	r.passed = append(r.passed, n)

	for _, a := range n.entry {
		r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: n.name, Name: a.name, Delay: 0, At: time.Time{}, Jitter: 0, Context: r.ctx, Event: nil, From: nil, To: nil, Needs: nil})
	}
	for k, name := range n.entryRuns {
		r.emitRun(n, name, event, n.entryConds[k])
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

// startTimer starts the timer of a delayed transition, computing its delay
// or deadline from the context as it is now.
func (r *run[S, C]) startTimer(t *transition[S, C]) {
	tm := timer[S, C]{
		id:    r.nextID(t.source, fmt.Sprintf("after.%d", t.afterIndex)),
		t:     t,
		delay: t.delay,
		due:   time.Time{},
		seq:   r.counter,
	}

	switch {
	case t.delayFn != nil:
		tm.delay = max(t.delayFn(r.ctx), 0)
		tm.due = r.now.Add(tm.delay)
	case t.deadlineFn != nil:
		tm.delay = 0
		tm.due = t.deadlineFn(r.ctx)
	default:
		tm.due = r.now.Add(tm.delay)
	}

	r.timers = append(r.timers, tm)
	r.effects = append(r.effects, timerEffect(tm))
}

// timerEffect is the effect that starts tm.
func timerEffect[S comparable, C any](tm timer[S, C]) Effect[C] {
	var zero C

	return Effect[C]{
		Kind:    EffectStartTimer,
		ID:      tm.id,
		State:   tm.t.source.name,
		Name:    tm.t.delayName,
		Delay:   tm.delay,
		At:      tm.due,
		Jitter:  tm.t.jitter,
		Context: zero,
		Event:   nil,
		From:    nil,
		To:      nil,
		Needs:   nil,
	}
}

// fireDue fires, one at a time, every timer that is due by the call's time:
// the earliest first, and among equally early ones the one started first.
// A jittered timer counts as due at the end of its jitter window, since its
// driver may fire it anywhere inside. Each fires with now set to that time,
// so the transitions it takes, and the timers they start, behave as if it
// had fired on time.
func (r *run[S, C]) fireDue() {
	callNow := r.now
	defer func() { r.now = callNow }()

	for steps := 0; !r.done; steps++ {
		if steps > maxSettleSteps {
			panic(fmt.Sprintf("sc: machine %q does not settle: its timers keep firing at %s", r.m.id, callNow))
		}

		next := -1
		for i, tm := range r.timers {
			if tm.latest().After(callNow) {
				continue
			}
			if next < 0 || tm.latest().Before(r.timers[next].latest()) || (tm.latest().Equal(r.timers[next].latest()) && tm.seq < r.timers[next].seq) {
				next = i
			}
		}
		if next < 0 {
			return
		}

		tm := r.timers[next]
		r.timers = slices.Delete(r.timers, next, next+1)
		r.changed = true
		r.fired = append(r.fired, tm.id)
		r.now = callNow
		if tm.latest().Before(callNow) {
			r.now = tm.latest()
		}

		stim := on[S, C](triggerAfter)
		stim.timer = tm.t
		if ts := r.selectTransitions(stim); len(ts) > 0 {
			r.microstep(ts, stim)
		}
		r.settle()
	}
}

func (r *run[S, C]) startService(inv *invocation[S, C]) {
	id := r.nextID(inv.state, inv.name)
	r.services = append(r.services, service[S, C]{id: id, inv: inv, acquired: false})
	r.effects = append(r.effects, r.serviceEffect(id, inv))
}

// serviceEffect is the effect that starts a run of inv, carrying the
// deadline of the budgets around it.
func (r *run[S, C]) serviceEffect(id string, inv *invocation[S, C]) Effect[C] {
	// A service or acquisition runs inside every held resource of its
	// state and the states around it.
	var needs []string
	for _, s := range r.services {
		if !s.acquired || s.inv == inv || !inv.state.within(s.inv.state) {
			continue
		}

		// A resource waits only for the ones declared before it on its
		// own state, which is the order they are acquired in.
		if inv.resource != nil && s.inv.state == inv.state &&
			slices.Index(inv.state.invocations, s.inv) > slices.Index(inv.state.invocations, inv) {
			continue
		}

		needs = append(needs, s.id)
	}

	if inv.resource != nil {
		return Effect[C]{Kind: EffectAcquire, ID: id, State: inv.state.name, Name: inv.name, Delay: 0, At: r.deadlineFor(inv.state), Jitter: 0, Context: r.ctx, Event: nil, From: nil, To: nil, Needs: needs}
	}

	return Effect[C]{Kind: EffectStartInvocation, ID: id, State: inv.state.name, Name: inv.name, Delay: 0, At: r.deadlineFor(inv.state), Jitter: 0, Context: r.ctx, Event: nil, From: nil, To: nil, Needs: needs}
}

// deadlineFor returns the earliest deadline among the budgets of n and the
// states around it, or the zero time when none declared one.
func (r *run[S, C]) deadlineFor(n *node[S, C]) time.Time {
	var deadline time.Time
	for a := n; a != nil; a = a.parent {
		if d, ok := r.scopes[a]; ok && (deadline.IsZero() || d.Before(deadline)) {
			deadline = d
		}
	}

	return deadline
}

// running reports whether inv has a run or, for a resource, is held.
func (r *run[S, C]) running(inv *invocation[S, C]) bool {
	return slices.ContainsFunc(r.services, func(s service[S, C]) bool { return s.inv == inv })
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
		names := r.activeNames()
		first := len(r.effects)
		r.endServices(r.services)
		for i := first; i < len(r.effects); i++ {
			r.effects[i].From, r.effects[i].To = names, names
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

		// A state's resources are acquired one after another, in the
		// order they were declared, so only the first starts now. It
		// starts before the services that may use it.
		for _, inv := range n.invocations {
			if inv.resource != nil {
				if !r.running(inv) {
					r.startService(inv)
				}

				break
			}
		}

		for _, inv := range n.invocations {
			if inv.resource == nil && !r.running(inv) {
				r.startService(inv)
			}
		}
	}

	r.entered = nil

	// Each resource acquired in this call lets the next one declared on
	// its state start, if the state is still active.
	for _, got := range r.acquired {
		if !r.config[got.state] {
			continue
		}

		next := false
		for _, inv := range got.state.invocations {
			if inv == got {
				next = true
				continue
			}

			if next && inv.resource != nil {
				if !r.running(inv) {
					r.startService(inv)
				}

				break
			}
		}
	}

	r.acquired = nil
}

// dropShortLivedTimers removes the start and cancel effects of timers that
// were started and then cancelled or fired within the same call, so drivers
// never see them.
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

	for _, id := range r.fired {
		if started[id] {
			cancelled[id] = true
		}
	}

	r.effects = slices.DeleteFunc(r.effects, func(e Effect[C]) bool {
		return (e.Kind == EffectStartTimer || e.Kind == EffectCancelTimer) && cancelled[e.ID]
	})
}

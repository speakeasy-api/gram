package sc

import (
	"cmp"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"iter"
	"math/rand/v2"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"
)

// maxSettleSteps bounds, unless [MaxSteps] says otherwise, the eventless
// transitions and internal events one call to Transition may process. A
// correct chart settles in a handful; the bound turns an Always or Raise
// cycle into a panic instead of a hang.
const maxSettleSteps = 10_000

// timerKind is what a running timer does when it fires.
type timerKind string

const (
	// timerAfter takes a delayed transition.
	timerAfter timerKind = ""

	// timerRetry runs a failed service again.
	timerRetry timerKind = "retry"

	// timerRaise delivers an event raised with a delay.
	timerRaise timerKind = "raise"
)

// timer is a running timer: a delayed transition waiting to be taken, a
// failed service waiting to be run again, or a raised event waiting to be
// delivered.
type timer[S comparable, C any] struct {
	id   string
	kind timerKind

	// t is the delayed transition of a timerAfter timer.
	t *transition[S, C]

	// retry is the service a timerRetry timer runs again, and attempt how
	// many times it has failed.
	retry   *invocation[S, C]
	attempt int

	// source is the state whose transition raised a timerRaise timer's
	// event, label the name of that raise, and raised the event.
	source *node[S, C]
	label  string
	raised any

	// delay is how long the timer was started for; zero for a deadline.
	delay time.Duration

	// due is when the timer fires.
	due time.Time

	// seq orders timers that are due at the same time: the one started
	// first fires first, which within one state is declaration order.
	seq uint64

	// jitter is the fraction by which drivers vary the wait.
	jitter float64

	// key and saved are, for a timerRetry timer, the spawn key and the
	// recorded progress of the run that failed, which the next run keeps.
	key   string
	saved json.RawMessage
}

// owner is the state whose exit cancels the timer. A raised event belongs
// to no state: it is delivered wherever the chart is by then.
func (tm timer[S, C]) owner() *node[S, C] {
	switch tm.kind {
	case timerAfter:
		return tm.t.source
	case timerRetry:
		return tm.retry.state
	case timerRaise:
	}

	return nil
}

// state is the state the timer is reported under.
func (tm timer[S, C]) state() *node[S, C] {
	if tm.kind == timerRaise {
		return tm.source
	}

	return tm.owner()
}

// name is the name the timer is reported under: a computed delay's, the
// retried service's, or the raise's.
func (tm timer[S, C]) name() string {
	switch tm.kind {
	case timerAfter:
		return tm.t.delayName
	case timerRetry:
		return tm.retry.name
	case timerRaise:
	}

	return tm.label
}

// latest is the latest time the timer can fire: its due time, or for a
// jittered timer the end of its jitter window. The core fires a timer by
// then even if its driver has not.
func (tm timer[S, C]) latest() time.Time {
	if tm.jitter == 0 {
		return tm.due
	}

	return tm.due.Add(time.Duration(tm.jitter * float64(tm.delay)))
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

	return timer[S, C]{id: id, kind: timerAfter, t: t, retry: nil, attempt: 0, source: nil, label: "", raised: nil, delay: delay, due: due, seq: seq, jitter: t.jitter, key: "", saved: nil}
}

// service is a run of an invocation that has not finished.
type service[S comparable, C any] struct {
	id  string
	inv *invocation[S, C]

	// acquired is set for a resource once its value has been acquired.
	acquired bool

	// attempt is how many times a service with a retry policy has failed
	// before this run.
	attempt int

	// saved is the progress the run last recorded with Checkpoint.
	saved json.RawMessage

	// cause is the event being processed when the run's state was
	// entered, which the service reads with Trigger.
	cause any
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

	// config is the set of active states, indexed by each state's position
	// in document order. list caches them as a list while listed is set.
	config []bool
	list   []*node[S, C]
	listed bool

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

	// cause is the event being processed, which the eventless transitions
	// that follow it see.
	cause any

	// sends holds the events sent to services during the step.
	sends []outgoing

	// starting is set while a new instance takes its first step, in which
	// the chart itself is entered.
	starting bool

	// ended lists the service runs the current microstep stopped.
	ended []service[S, C]
}

// namedCause is the event being processed, if the chart names its type and
// so can store it; otherwise nil.
func (r *run[S, C]) namedCause() any {
	if _, named := r.m.eventNames[reflect.TypeOf(r.cause)]; !named {
		return nil
	}

	return r.cause
}

// causeName is the stored name of a service run's cause, or "".
func (r *run[S, C]) causeName(cause any) string {
	if cause == nil {
		return ""
	}

	return r.m.EventName(cause)
}

// noteCause records the event of stim as the one being processed.
func (r *run[S, C]) noteCause(stim stimulus[S, C]) {
	if stim.event != nil {
		r.cause = stim.event
	}
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
		config:   m.rootOnly(),
		history:  nil,
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
		scopes:   nil,
		acquired: nil,
		pending:  nil,
		effects:  nil,
		queue:    nil,
		entered:  nil,
		passed:   nil,
		changed:  false,
		cause:    nil,
		sends:    nil,
		starting: false,
		ended:    nil,
		list:     nil,
		listed:   false,
	}

	enter := new(transition[S, C])
	enter.source = m.root
	enter.trigger = triggerAlways
	// A chart without states has only itself to enter.
	if m.root.initial != nil {
		enter.target = m.root.initial
		enter.targets = []*node[S, C]{m.root.initial}
	}

	// A chart whose top-level states are regions enters all of them.
	if m.root.kind == kindParallel {
		enter.targets = m.root.states()
		enter.target = enter.targets[0]
	}

	// The chart itself is entered by the same step, before its states.
	r.starting = true
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

	// Most events an instance is sent change nothing. Those are told
	// apart here, before any working state is built for them.
	if m.inert(snap, ev, now) {
		return snap, nil, nil
	}

	r := m.load(snap, now)
	r.fireDue()

	// Time passing is reason enough to look at the eventless transitions
	// again: a guard on the time may hold now.
	if ev == nil && !r.done {
		r.settle()
		r.fireDue()
	}

	if ev != nil && !r.done {
		// Bookkeeping the core does for itself, such as recording a
		// service's progress, changes nothing a transition can see, so no
		// eventless transition is looked at again for it. Otherwise one
		// that sends to a child machine would be taken for every step the
		// child records, which the send itself causes.
		stim, ok := r.accept(ev)

		switch {
		case ok && stim.trigger != triggerNone:
			r.noteCause(stim)
			if ts := r.selectTransitions(stim); len(ts) > 0 {
				r.microstep(ts, stim)
			}
			r.settle()
			r.fireDue()
		case ok:
			// It may still have started a timer that is due at once,
			// such as a retry without a wait.
			r.fireDue()
		}
	}

	if !r.changed {
		return snap, nil, nil
	}

	return r.snapshot(snap.seq + 1), r.effects, r.passed
}

// Advance returns the snapshot as of now: every timer due by now fires, as
// [Machine.Transition] fires them before an event, and an eventless
// transition whose guard holds by now is taken. It returns snap itself when
// nothing changes. Use it to read an instance's current state without
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
		config:   slices.Clone(snap.config),
		history:  nil,
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
		scopes:   nil,
		acquired: nil,
		pending:  nil,
		effects:  nil,
		queue:    nil,
		entered:  nil,
		passed:   nil,
		changed:  false,
		cause:    nil,
		sends:    nil,
		starting: false,
		ended:    nil,
		list:     snap.nodes,
		listed:   true,
	}

	for h, recorded := range snap.history {
		nodes := make([]*node[S, C], 0, len(recorded))
		for _, s := range recorded {
			nodes = append(nodes, m.nodes[s])
		}
		r.setHistory(m.nodes[h], nodes)
	}

	for _, pt := range snap.timers {
		n := m.at(pt.state, pt.root)

		switch pt.kind {
		case timerAfter:
			for _, t := range n.transitions {
				if t.trigger == triggerAfter && t.afterIndex == pt.index {
					r.timers = append(r.timers, restoredTimer(pt.id, t, pt.delay, pt.due, pt.seq, now))
				}
			}
		case timerRetry:
			for _, inv := range n.invocations {
				if inv.name == pt.name && inv.retry != nil {
					r.timers = append(r.timers, timer[S, C]{id: pt.id, kind: timerRetry, t: nil, retry: inv, attempt: pt.attempt, source: nil, label: "", raised: pt.raised, delay: pt.delay, due: pt.due, seq: pt.seq, jitter: pt.jitter, key: pt.key, saved: pt.saved})
				}
			}
		case timerRaise:
			r.timers = append(r.timers, timer[S, C]{id: pt.id, kind: timerRaise, t: nil, retry: nil, attempt: 0, source: n, label: pt.name, raised: pt.raised, delay: pt.delay, due: pt.due, seq: pt.seq, jitter: 0, key: "", saved: nil})
		}
	}

	for s, deadline := range snap.scopes {
		r.setScope(m.nodes[s], deadline)
	}
	if !snap.rootScope.IsZero() {
		r.setScope(m.root, snap.rootScope)
	}

	for _, ri := range snap.invocations {
		for _, inv := range m.at(ri.state, ri.root).invocations {
			if inv.name == ri.name {
				r.services = append(r.services, service[S, C]{id: ri.id, inv: inv, acquired: ri.acquired, attempt: ri.attempt, saved: ri.saved, cause: ri.cause})
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
		active:      make([]S, 0, len(r.active())),
		history:     nil,
		timers:      make([]pendingTimer[S], 0, len(r.timers)),
		invocations: make([]runningInvocation[S], 0, len(r.services)),
		scopes:      nil,
		counter:     r.counter,
		tags:        nil,
		config:      r.config,
		nodes:       r.active(),
		rootScope:   time.Time{},
	}

	// The working state ends here, so the snapshot keeps its set of active
	// states, not a copy.

	for _, n := range append([]*node[S, C]{r.m.root}, r.active()...) {
		for _, tag := range n.tags {
			if !slices.Contains(snap.tags, tag) {
				snap.tags = append(snap.tags, tag)
			}
		}
	}
	slices.Sort(snap.tags)

	if len(r.scopes) > 0 {
		snap.scopes = make(map[S]time.Time, len(r.scopes))
	}
	for n, deadline := range r.scopes {
		if n.root {
			snap.rootScope = deadline
			continue
		}
		snap.scopes[n.id] = deadline
	}

	for _, n := range r.active() {
		snap.active = append(snap.active, n.id)
	}

	if len(r.history) > 0 {
		snap.history = make(map[S][]S, len(r.history))
	}
	for h, recorded := range r.history {
		ids := make([]S, 0, len(recorded))
		for _, n := range recorded {
			ids = append(ids, n.id)
		}
		snap.history[h.id] = ids
	}

	for _, t := range r.timers {
		index := 0
		if t.kind == timerAfter {
			index = t.t.afterIndex
		}

		snap.timers = append(snap.timers, pendingTimer[S]{
			id: t.id, kind: t.kind, state: t.state().id, root: t.state().root, index: index, name: t.name(),
			delay: t.delay, due: t.due, seq: t.seq, jitter: t.jitter,
			attempt: t.attempt, raised: t.raised, eventName: "",
			key: t.key, saved: t.saved,
		})
		if t.raised != nil {
			snap.timers[len(snap.timers)-1].eventName = r.m.EventName(t.raised)
		}
	}

	for _, s := range r.services {
		snap.invocations = append(snap.invocations, runningInvocation[S]{id: s.id, state: s.inv.state.id, root: s.inv.state.root, name: s.inv.name, resource: s.inv.resource != nil, acquired: s.acquired, attempt: s.attempt, saved: s.saved, cause: s.cause, eventName: r.causeName(s.cause)})
	}

	return snap
}

// active returns the active states, without the root, in document order.
// The result is shared between calls until the active states change, and
// must not be modified.
func (r *run[S, C]) active() []*node[S, C] {
	if r.listed {
		return r.list
	}

	out := make([]*node[S, C], 0, 8)
	for _, n := range r.m.ordered {
		if r.config[n.order] {
			out = append(out, n)
		}
	}
	r.list, r.listed = out, true

	return out
}

// activate and deactivate change whether n is active.
func (r *run[S, C]) activate(n *node[S, C]) {
	r.config[n.order], r.listed = true, false
}

func (r *run[S, C]) deactivate(n *node[S, C]) {
	r.config[n.order], r.listed = false, false
}

// rootOnly returns a set of active states that holds only the root. The set
// is indexed by a state's position in document order.
func (m *Machine[S, C]) rootOnly() []bool {
	config := make([]bool, len(m.ordered)+1)
	config[m.root.order] = true

	return config
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

		tm := r.timers[i]
		r.timers = slices.Delete(r.timers, i, i+1)
		r.changed = true
		r.fired = append(r.fired, e.ID)

		return r.fire(tm), true
	case Emitted:
		if !slices.ContainsFunc(r.services, func(s service[S, C]) bool { return s.id == e.ID }) {
			return none, false
		}

		stim := on[S, C](triggerEvent)
		stim.event = e.Event

		return stim, true
	case Checkpointed:
		i := slices.IndexFunc(r.services, func(s service[S, C]) bool { return s.id == e.ID })
		if i < 0 {
			return none, false
		}

		r.services[i].saved = e.Data
		r.changed = true

		return on[S, C](triggerNone), true
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

		ended := r.services[i]
		inv, attempt := ended.inv, ended.attempt
		r.services = slices.Delete(r.services, i, i+1)
		r.changed = true

		stim := on[S, C](triggerInvokeDone)
		if failed, ok := ev.(Failed); ok {
			// A failure the service's retry policy covers is not the
			// chart's to see: the service runs again after a wait.
			if r.retries(inv, attempt, failed.Err) {
				r.startRetry(ended, attempt+1)
				return on[S, C](triggerNone), true
			}

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
		if !t.wildcard && reflect.TypeOf(stim.event) != t.eventType {
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
	case triggerAlways, triggerNone:
	}

	for _, in := range t.inNodes {
		if !r.config[in.order] {
			return false
		}
	}

	for _, out := range t.notInNodes {
		if r.config[out.order] {
			return false
		}
	}

	for _, cond := range t.childConds {
		if !r.childIn(cond) {
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

// childIn reports whether the child machine a condition names is running
// and has every one of the condition's states active, going by the snapshot
// the child last handed up.
func (r *run[S, C]) childIn(cond childCond) bool {
	for _, running := range r.services {
		if running.inv.name != cond.service || len(running.saved) == 0 {
			continue
		}

		var raw RawSnapshot
		if err := json.Unmarshal(running.saved, &raw); err != nil {
			return false
		}

		return !slices.ContainsFunc(cond.states, func(state string) bool { return !slices.Contains(raw.Active, state) })
	}

	return false
}

// selectTransitions picks, for each active leaf state, the first matching
// transition on the leaf or its nearest ancestor, then drops transitions
// that conflict with a higher-priority one.
func (r *run[S, C]) selectTransitions(stim stimulus[S, C]) []*transition[S, C] {
	var enabled []*transition[S, C]

	// A chart without states is its own only leaf.
	// With no state active, the chart is the only leaf there is.
	leaves := r.active()
	alone := len(leaves) == 0
	if alone {
		leaves = r.m.root.self
	}

	for _, leaf := range leaves {
		if !leaf.atomic() && !alone {
			continue
		}

	ancestors:
		for n := leaf; n != nil; n = n.parent {
			// A state's OnAny transitions are tried after its others.
			for _, wildcard := range []bool{false, true} {
				for _, t := range n.transitions {
					if t.wildcard != wildcard || !r.matches(t, stim) {
						continue
					}
					if !slices.Contains(enabled, t) {
						enabled = append(enabled, t)
					}

					break ancestors
				}
			}
		}
	}

	// One transition, or none, has nothing to conflict with.
	if len(enabled) <= 1 {
		return enabled
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
//
// A transition that does not re-enter disturbs as little as it can: it
// leaves its source alone when the target lies inside it, and inside a
// parallel state it exits only the region that contains its target, so the
// other regions, the source's own included, stay as they are. That follows
// XState rather than SCXML, whose rule a transition marked Reenter keeps:
// its domain is the nearest compound state that properly contains both
// source and target, which for a transition between regions is outside the
// parallel state, so every region is exited and entered again. The one
// exception is a region re-entering itself; see reentersRegion.
func (r *run[S, C]) domain(t *transition[S, C]) *node[S, C] {
	if t.target == nil {
		return nil
	}

	// A new instance enters everything under the chart.
	if r.starting {
		return r.m.root
	}

	all := func(pred func(*node[S, C]) bool) bool {
		return !slices.ContainsFunc(t.targets, func(n *node[S, C]) bool { return !pred(n) })
	}

	if r.reentersRegion(t) {
		return t.source.parent
	}

	if t.reenter {
		for a := t.source.parent; a != nil; a = a.parent {
			if a.kind == kindState && all(func(n *node[S, C]) bool { return n.below(a) }) {
				return a
			}
		}

		return r.m.root
	}

	if all(func(n *node[S, C]) bool { return n.within(t.source) }) {
		return r.narrow(t.source, t.targets)
	}

	for a := t.source.parent; a != nil; a = a.parent {
		if all(func(n *node[S, C]) bool { return n.below(a) }) {
			return r.narrow(a, t.targets)
		}
	}

	return r.narrow(r.m.root, t.targets)
}

// narrow moves a domain that is a parallel state down to the one region
// that contains everything the targets stand for, and further while that is
// again a parallel state. Targets in several regions, or a history target
// that restores states in several, leave the domain where it is.
func (r *run[S, C]) narrow(dom *node[S, C], declared []*node[S, C]) *node[S, C] {
	if dom.kind != kindParallel {
		return dom
	}

	// Only a history target stands for states other than itself.
	targets := declared
	if slices.ContainsFunc(declared, func(n *node[S, C]) bool { return n.kind == kindHistory }) {
		targets = nil
		for _, target := range declared {
			targets = append(targets, r.effectiveTargets(target)...)
		}
	}

	for dom.kind == kindParallel {
		var region *node[S, C]
		for _, child := range dom.states() {
			if !slices.ContainsFunc(targets, func(n *node[S, C]) bool { return !n.within(child) }) {
				region = child
				break
			}
		}

		if region == nil {
			break
		}
		dom = region
	}

	return dom
}

// reentersRegion reports whether t re-enters a region of a parallel state
// from inside it: its source is a region and every target lies within it.
// Such a transition exits and enters that region alone. SCXML would have it
// leave the whole parallel state, which restarts the other regions for no
// reason of their own.
func (r *run[S, C]) reentersRegion(t *transition[S, C]) bool {
	return !r.m.strict && t.reenter && t.source.parent != nil && t.source.parent.kind == kindParallel &&
		!slices.ContainsFunc(t.targets, func(n *node[S, C]) bool { return !n.within(t.source) })
}

// exitSet returns the active states t exits.
func (r *run[S, C]) exitSet(t *transition[S, C]) []*node[S, C] {
	dom := r.domain(t)
	if dom == nil {
		return nil
	}

	region := r.reentersRegion(t)

	var out []*node[S, C]
	for _, n := range r.active() {
		if region {
			if n.within(t.source) {
				out = append(out, n)
			}

			continue
		}

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
				r.setHistory(h, r.record(h))
			}
		}
	}

	// The active states before and after are named only for a step that
	// produced something that needs them.
	beforeNodes := r.active()
	first := len(r.effects)
	r.ended = r.ended[:0]
	r.pending = r.pending[:0]

	for _, n := range exits {
		r.exit(n, stim.event)
	}

	// A transition of the chart itself marked Reenter leaves the chart
	// and enters it again: its exit and entry actions run, and its timers
	// and services start afresh.
	restart := slices.ContainsFunc(ts, func(t *transition[S, C]) bool {
		return t.reenter && t.target != nil && (t.source.root || t.target.root)
	})
	if restart {
		r.exit(r.m.root, stim.event)
	}

	for _, t := range ts {
		r.runSteps(t, stim)
	}

	// A new instance's first step enters the chart itself before its
	// states, and so does a transition of the chart that re-enters it.
	entering := r.entrySet(ts)
	if r.starting || restart {
		r.starting = false
		r.enter(r.m.root, stim.event)
	}

	for _, n := range entering {
		r.enter(n, stim.event)
	}

	moved := len(r.pending) > 0
	for i := first; i < len(r.effects) && !moved; i++ {
		moved = r.effects[i].Kind == EffectAction || r.effects[i].Kind == EffectRelease
	}
	if !moved {
		return
	}

	// Every action this step produced is told the move it belongs to.
	before, after := nodeNames(beforeNodes), r.activeNames()
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
	return nodeNames(r.active())
}

func nodeNames[S comparable, C any](nodes []*node[S, C]) []string {
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.name)
	}

	return names
}

// setScope and setHistory write to maps that most runs never need.
func (r *run[S, C]) setScope(n *node[S, C], deadline time.Time) {
	if r.scopes == nil {
		r.scopes = map[*node[S, C]]time.Time{}
	}
	r.scopes[n] = deadline
}

func (r *run[S, C]) setHistory(h *node[S, C], recorded []*node[S, C]) {
	if r.history == nil {
		r.history = map[*node[S, C]][]*node[S, C]{}
	}
	r.history[h] = recorded
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
	return Effect[C]{Kind: EffectAction, ID: "", State: source.name, Name: name, Delay: 0, At: time.Time{}, Jitter: 0, Context: r.ctx, Event: event, From: nil, To: nil, Needs: nil, Saved: nil}
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
	for _, assign := range n.exitAssigns {
		r.ctx = assign.fn(r.ctx)
	}

	for _, a := range n.exit {
		r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: n.name, Name: a.name, Delay: 0, At: time.Time{}, Jitter: 0, Context: r.ctx, Event: nil, From: nil, To: nil, Needs: nil, Saved: nil})
	}
	for k, name := range n.exitRuns {
		r.emitRun(n, name, event, n.exitConds[k])
	}
	r.raise(n.exitRaises)
	r.message(n, n.exitSends, n.exitEmits)

	r.timers = slices.DeleteFunc(r.timers, func(t timer[S, C]) bool {
		if t.owner() != n {
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
	r.ended = append(r.ended, ending...)

	r.deactivate(n)
}

// cancelRaise drops the events a raise of the given name is still waiting to
// deliver.
func (r *run[S, C]) cancelRaise(name string) {
	r.timers = slices.DeleteFunc(r.timers, func(tm timer[S, C]) bool {
		if tm.kind != timerRaise || tm.label != name {
			return false
		}
		r.effects = append(r.effects, r.bare(EffectCancelTimer, tm.id, tm.source, tm.label))

		return true
	})
}

// raise queues the events a state raises on entry or exit.
func (r *run[S, C]) raise(raises []stateRaise[C]) {
	for _, raise := range raises {
		raised := on[S, C](triggerEvent)
		raised.event = raise.fn(r.ctx)
		r.queue = append(r.queue, raised)
	}
}

// message queues the events a state sends to services on entry or exit, and
// emits those it hands to whoever runs the machine.
func (r *run[S, C]) message(n *node[S, C], sends []stateSend[C], emits []stateRaise[C]) {
	for _, send := range sends {
		r.send(outgoing{service: send.service, keyed: false, key: "", event: send.fn(r.ctx), leaving: nil})
	}

	for _, emit := range emits {
		e := r.bare(EffectEmit, "", n, emit.name)
		e.Event = emit.fn(r.ctx)
		r.effects = append(r.effects, e)
	}
}

// bare builds an effect that carries no context or event.
func (r *run[S, C]) bare(kind EffectKind, id string, n *node[S, C], name string) Effect[C] {
	var zero C
	return Effect[C]{Kind: kind, ID: id, State: n.name, Name: name, Delay: 0, At: time.Time{}, Jitter: 0, Context: zero, Event: nil, From: nil, To: nil, Needs: nil, Saved: nil}
}

func (r *run[S, C]) runSteps(t *transition[S, C], stim stimulus[S, C]) {
	for k, s := range t.steps {
		switch s.kind {
		case stepAssign:
			// The source of random numbers is made only for a step
			// that draws from it.
			var rnd *rand.Rand
			if s.random {
				rnd = r.random()
			}

			r.ctx = s.assign(r.ctx, stim.event, r.now, rnd)
		case stepDo:
			r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: t.source.name, Name: s.name, Delay: 0, At: time.Time{}, Jitter: 0, Context: r.ctx, Event: stim.event, From: nil, To: nil, Needs: nil, Saved: nil})
		case stepRun:
			r.emitRun(t.source, s.name, stim.event, t.runConds[k])
		case stepRaise:
			ev := s.raise(r.ctx, stim.event)
			if delay, ok := t.raiseDelays[k]; ok {
				r.startRaise(t.source, s.name, ev, delay)
				continue
			}

			raised := on[S, C](triggerEvent)
			raised.event = ev
			r.queue = append(r.queue, raised)
		case stepRelease:
			var ending []service[S, C]
			r.services = slices.DeleteFunc(r.services, func(held service[S, C]) bool {
				if held.inv.resource == nil || held.inv.name != s.name {
					return false
				}
				ending = append(ending, held)

				return true
			})
			r.endServices(ending)
		case stepSpawn:
			key := t.keys[k](r.ctx, stim.event)
			for _, inv := range r.spawnable(t.sendTargets[k]) {
				if !slices.ContainsFunc(r.services, func(running service[S, C]) bool { return running.inv == inv && keyOf(running.id) == key }) {
					r.startRun(inv, 0, key, nil)
				}
			}
		case stepStop:
			key := t.keys[k](r.ctx, stim.event)

			var ending []service[S, C]
			r.services = slices.DeleteFunc(r.services, func(running service[S, C]) bool {
				if !running.inv.spawned || running.inv.name != t.sendTargets[k] || keyOf(running.id) != key {
					return false
				}
				ending = append(ending, running)

				return true
			})
			r.endServices(ending)

			// A run waiting to be retried is stopped as well.
			r.timers = slices.DeleteFunc(r.timers, func(tm timer[S, C]) bool {
				if tm.kind != timerRetry || !tm.retry.spawned || tm.retry.name != t.sendTargets[k] || tm.key != key {
					return false
				}
				r.effects = append(r.effects, r.bare(EffectCancelTimer, tm.id, tm.retry.state, ""))

				return true
			})
		case stepSend:
			// The event is delivered once the step has settled, so that
			// it reaches a service the step itself starts.
			send := outgoing{service: t.sendTargets[k], keyed: false, key: "", event: s.raise(r.ctx, stim.event), leaving: nil}
			if keyFn, keyed := t.keys[k]; keyed {
				send.keyed, send.key = true, keyFn(r.ctx, stim.event)
			}
			r.send(send)
		case stepEmit:
			e := r.bare(EffectEmit, "", t.source, s.name)
			e.Event = s.raise(r.ctx, stim.event)
			r.effects = append(r.effects, e)
		case stepCancel:
			r.cancelRaise(s.name)
		case stepCancelAfter:
			target := r.m.names[s.name]
			r.timers = slices.DeleteFunc(r.timers, func(tm timer[S, C]) bool {
				if tm.kind != timerAfter || tm.t.source != target {
					return false
				}
				r.effects = append(r.effects, r.bare(EffectCancelTimer, tm.id, target, tm.t.delayName))

				return true
			})
		}
	}
}

func (r *run[S, C]) enter(n *node[S, C], event any) {
	r.activate(n)
	if n.budget > 0 {
		r.setScope(n, r.now.Add(n.budget))
	}
	r.entered = append(r.entered, n)
	if !n.root {
		r.passed = append(r.passed, n)
	}

	for _, assign := range n.entryAssigns {
		r.ctx = assign.fn(r.ctx)
	}

	for _, a := range n.entry {
		r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: n.name, Name: a.name, Delay: 0, At: time.Time{}, Jitter: 0, Context: r.ctx, Event: nil, From: nil, To: nil, Needs: nil, Saved: nil})
	}
	for k, name := range n.entryRuns {
		r.emitRun(n, name, event, n.entryConds[k])
	}
	r.raise(n.entryRaises)
	for _, raise := range n.entryDelayed {
		if raise.cancel {
			r.cancelRaise(raise.name)
			continue
		}

		r.startRaise(n, raise.name, raise.fn(r.ctx), raise.delay)
	}
	r.message(n, n.entrySends, n.entryEmits)

	for _, t := range n.transitions {
		if t.trigger == triggerAfter {
			r.startTimer(t)
		}
	}

	if n.kind != kindFinal {
		return
	}

	// A final state completes the compound state it is a child of, and
	// reports its output with that. One declared directly in a parallel
	// state is a region of it. The chart completing ends the machine.
	p := n.parent
	if p.kind != kindParallel {
		if p.root {
			r.done = true
			return
		}

		var output any
		if n.output != nil {
			output = n.output(r.ctx)
		}
		r.queueDone(p, output)
		p = p.parent
	}

	// A parallel state completes once every region has, and that may in
	// turn complete a parallel state it is a region of.
	for ; p != nil && p.kind == kindParallel && r.allRegionsFinal(p); p = p.parent {
		if p.root {
			r.done = true
			return
		}
		r.queueDone(p, Done[map[string]any]{ID: p.name, Result: r.m.outputs(p, r.config, r.ctx)})
	}
}

// queueDone queues the completion of n for its OnDone transitions, with the
// completion event of the final state reached, if it declared an output.
func (r *run[S, C]) queueDone(n *node[S, C], output any) {
	stim := on[S, C](triggerStateDone)
	stim.done = n
	stim.event = output
	r.queue = append(r.queue, stim)
}

// allRegionsFinal reports whether every region of parallel state p has
// completed.
func (r *run[S, C]) allRegionsFinal(p *node[S, C]) bool {
	for _, region := range p.states() {
		if !r.completed(region) {
			return false
		}
	}

	return true
}

// completed reports whether n has completed: a compound state is in one of
// its final children, and a parallel state has every region completed.
func (r *run[S, C]) completed(n *node[S, C]) bool {
	if n.kind == kindParallel {
		return r.allRegionsFinal(n)
	}

	if n.kind == kindFinal {
		return r.config[n.order]
	}

	for _, c := range n.children {
		if c.kind == kindFinal && r.config[c.order] {
			return true
		}
	}

	return false
}

func (r *run[S, C]) nextID(n *node[S, C], label string) string {
	r.counter++
	return fmt.Sprintf("%s.%s#%d", n.name, label, r.counter)
}

// startTimer starts the timer of a delayed transition, computing its delay
// or deadline from the context as it is now.
func (r *run[S, C]) startTimer(t *transition[S, C]) {
	tm := timer[S, C]{
		id:      r.nextID(t.source, fmt.Sprintf("after.%d", t.afterIndex)),
		kind:    timerAfter,
		t:       t,
		retry:   nil,
		attempt: 0,
		source:  nil,
		label:   "",
		raised:  nil,
		delay:   t.delay,
		due:     time.Time{},
		seq:     0,
		jitter:  t.jitter,
		key:     "",
		saved:   nil,
	}
	tm.seq = r.counter

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
		State:   tm.state().name,
		Name:    tm.name(),
		Delay:   tm.delay,
		At:      tm.due,
		Jitter:  tm.jitter,
		Context: zero,
		Event:   nil,
		From:    nil,
		To:      nil,
		Needs:   nil,
		Saved:   nil,
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
		if steps > r.m.maxSteps {
			panic(fmt.Sprintf("sc: machine %q does not settle within %d steps: its timers keep firing at %s, in states %v", r.m.id, r.m.maxSteps, callNow, r.activeNames()))
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

		stim := r.fire(tm)
		r.noteCause(stim)
		if ts := r.selectTransitions(stim); len(ts) > 0 {
			r.microstep(ts, stim)
		}
		r.settle()
	}
}

// fire does what a timer that has come due is for, and returns what the
// chart must react to.
func (r *run[S, C]) fire(tm timer[S, C]) stimulus[S, C] {
	switch tm.kind {
	case timerAfter:
		stim := on[S, C](triggerAfter)
		stim.timer = tm.t

		return stim
	case timerRetry:
		r.startRunFor(tm.retry, tm.attempt, tm.key, tm.saved, tm.raised)
	case timerRaise:
		stim := on[S, C](triggerEvent)
		stim.event = tm.raised

		return stim
	}

	return on[S, C](triggerNone)
}

// retries reports whether inv, having failed with err after attempt earlier
// failures, is to be run again instead of failing.
func (r *run[S, C]) retries(inv *invocation[S, C], attempt int, err error) bool {
	p := inv.retry
	if p == nil || (p.Max != Forever && attempt >= p.Max) || (p.If != nil && !p.If(err)) {
		return false
	}

	// A retry that could only start once the state's budget has run out
	// would fail at once, so the failure stands.
	deadline := r.deadlineFor(inv.state)

	return deadline.IsZero() || r.now.Add(p.wait(attempt+1)).Before(deadline)
}

// startRetry starts the wait before the service of the run that ended runs
// again after its failures-th failure, keeping the run's key and progress.
func (r *run[S, C]) startRetry(ended service[S, C], failures int) {
	inv := ended.inv
	p := inv.retry
	key, _ := SpawnKey(ended.id)

	// Under Forever the count stops growing once the wait has, so the
	// snapshot takes finitely many values.
	if p.Max == Forever {
		failures = min(failures, p.steps())
	}

	tm := timer[S, C]{
		id:      r.nextID(inv.state, "retry."+inv.name),
		kind:    timerRetry,
		t:       nil,
		retry:   inv,
		attempt: failures,
		source:  nil,
		label:   "",
		raised:  ended.cause,
		delay:   p.wait(failures),
		due:     time.Time{},
		seq:     0,
		jitter:  p.Jitter,
		key:     key,
		saved:   ended.saved,
	}
	tm.seq = r.counter
	tm.due = r.now.Add(tm.delay)

	r.timers = append(r.timers, tm)
	r.effects = append(r.effects, timerEffect[S, C](tm))
}

// startRaise starts the wait before ev, raised by a transition of source, is
// delivered.
func (r *run[S, C]) startRaise(source *node[S, C], name string, ev any, delay time.Duration) {
	if _, ok := r.m.eventNames[reflect.TypeOf(ev)]; !ok {
		panic(fmt.Sprintf("sc: machine %q: RaiseAfter %q in state %q raised a %T, which no On transition of the chart handles", r.m.id, name, source.name, ev))
	}

	tm := timer[S, C]{
		id:      r.nextID(source, "raise."+name),
		kind:    timerRaise,
		t:       nil,
		retry:   nil,
		attempt: 0,
		source:  source,
		label:   name,
		raised:  ev,
		delay:   delay,
		due:     time.Time{},
		seq:     0,
		jitter:  0,
		key:     "",
		saved:   nil,
	}
	tm.seq = r.counter
	tm.due = r.now.Add(delay)

	r.timers = append(r.timers, tm)
	r.effects = append(r.effects, timerEffect[S, C](tm))
}

func (r *run[S, C]) startService(inv *invocation[S, C]) {
	r.startRun(inv, 0, "", nil)
}

// startRun starts a run of inv that follows attempt failed ones.
// key is the key of a spawned run, and saved the progress it carries on from.
func (r *run[S, C]) startRun(inv *invocation[S, C], attempt int, key string, saved json.RawMessage) {
	r.startRunFor(inv, attempt, key, saved, r.namedCause())
}

// startRunFor is startRun for a run whose cause is already known, as it is
// for a run a retry starts.
func (r *run[S, C]) startRunFor(inv *invocation[S, C], attempt int, key string, saved json.RawMessage, cause any) {
	id := r.nextID(inv.state, inv.name)
	if inv.spawned {
		id += "/" + url.PathEscape(key)
	}

	r.services = append(r.services, service[S, C]{id: id, inv: inv, acquired: false, attempt: attempt, saved: saved, cause: cause})
	r.effects = append(r.effects, r.serviceEffect(id, inv))
}

// serviceEffect is the effect that starts a run of inv, carrying the
// deadline of the budgets around it.
func (r *run[S, C]) serviceEffect(id string, inv *invocation[S, C]) Effect[C] {
	// A run that is being started again carries the progress it recorded
	// and the event it was started for.
	var (
		saved json.RawMessage
		cause any
	)

	// A service or acquisition runs inside every held resource of its
	// state and the states around it.
	var needs []string
	for _, s := range r.services {
		if s.id == id {
			saved, cause = s.saved, s.cause
		}

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
		return Effect[C]{Kind: EffectAcquire, ID: id, State: inv.state.name, Name: inv.name, Delay: 0, At: r.deadlineFor(inv.state), Jitter: 0, Context: r.ctx, Event: nil, From: nil, To: nil, Needs: needs, Saved: saved}
	}

	return Effect[C]{Kind: EffectStartInvocation, ID: id, State: inv.state.name, Name: inv.name, Delay: 0, At: r.deadlineFor(inv.state), Jitter: 0, Context: r.ctx, Event: cause, From: nil, To: nil, Needs: needs, Saved: saved}
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
	set := &nodeSet[S, C]{in: make([]bool, len(r.m.ordered)+1), list: nil}

	for _, t := range ts {
		if t.target == nil {
			continue
		}

		dom := r.domain(t)

		// Every target is in place before the states around them are
		// added, so a region one target names is not also given its
		// default entry on behalf of another.
		for _, target := range t.targets {
			r.addDescendants(set, target)
		}

		for _, target := range t.targets {
			for _, n := range r.effectiveTargets(target) {
				r.addAncestors(set, n, dom)
			}
		}

		// The domain stays active throughout, so it is never entered.
		// When it is a parallel state its other regions were exited
		// and need their default entry.
		set.in[dom.order] = false
		if !r.reentersRegion(t) {
			r.fillRegions(set, dom)
		}
	}

	out := set.list[:0]
	for _, n := range set.list {
		if set.in[n.order] {
			out = append(out, n)
		}
	}
	slices.SortFunc(out, byOrder[S, C])

	return out
}

// effectiveTargets resolves a history state to the states it stands for.
func (r *run[S, C]) effectiveTargets(n *node[S, C]) []*node[S, C] {
	if n.kind != kindHistory {
		return n.self
	}

	if recorded, ok := r.history[n]; ok {
		return recorded
	}

	switch {
	case n.defaultNode != nil:
		return n.defaultNodes
	case n.parent.kind == kindParallel:
		return n.parent.states()
	default:
		return []*node[S, C]{n.parent.initial}
	}
}

// addDescendants adds n and the states entered along with it by default.
func (r *run[S, C]) addDescendants(set *nodeSet[S, C], n *node[S, C]) {
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

	set.add(n)

	if n.kind == kindState && n.initial != nil {
		r.addDescendants(set, n.initial)
	}

	r.fillRegions(set, n)
}

// addAncestors adds the states between n and anc, exclusive of both. It adds
// nothing when n is anc itself, as it is for a transition that targets its
// own source.
func (r *run[S, C]) addAncestors(set *nodeSet[S, C], n, anc *node[S, C]) {
	if !n.below(anc) {
		return
	}

	for a := n.parent; a != anc; a = a.parent {
		set.add(a)
		r.fillRegions(set, a)
	}
}

// fillRegions gives every region of parallel state p that has nothing in set
// its default entry.
func (r *run[S, C]) fillRegions(set *nodeSet[S, C], p *node[S, C]) {
	if p.kind != kindParallel {
		return
	}

	for _, region := range p.states() {
		covered := false
		for _, s := range set.list {
			covered = covered || (set.in[s.order] && s.within(region))
		}
		if !covered {
			r.addDescendants(set, region)
		}
	}
}

// nodeSet is a set of states being collected: in says which are members,
// by position in document order, and list holds every state ever added.
type nodeSet[S comparable, C any] struct {
	in   []bool
	list []*node[S, C]
}

func (s *nodeSet[S, C]) add(n *node[S, C]) {
	if !s.in[n.order] {
		s.in[n.order] = true
		s.list = append(s.list, n)
	}
}

// settle takes eventless transitions and processes queued internal events
// until nothing more can happen, then starts the services of the states that
// ended up active.
func (r *run[S, C]) settle() {
	rested := false

	for steps := 0; !r.done; steps++ {
		if steps > r.m.maxSteps {
			panic(fmt.Sprintf("sc: machine %q does not settle within %d steps: its Always transitions or raised events form a cycle, last processing %T in states %v", r.m.id, r.m.maxSteps, r.cause, r.activeNames()))
		}

		// An eventless transition that changed nothing, neither the active
		// states nor the context, would be taken again for ever. It is
		// taken once, and eventless transitions rest until something else
		// changes.
		always := on[S, C](triggerAlways)
		always.event = r.cause
		if ts := r.selectTransitions(always); len(ts) > 0 && !rested {
			// Guards see the event whatever its type. Actions carry
			// it only when the chart names its type, because an
			// effect with any other event could not be stored.
			if _, named := r.m.eventNames[reflect.TypeOf(always.event)]; !named {
				always.event = nil
			}

			active, ctx := r.active(), r.ctx
			r.microstep(ts, always)
			rested = slices.Equal(active, r.active()) && reflect.DeepEqual(ctx, r.ctx)

			continue
		}

		if len(r.queue) == 0 {
			break
		}

		stim := r.queue[0]
		r.queue = r.queue[1:]
		r.noteCause(stim)

		if ts := r.selectTransitions(stim); len(ts) > 0 {
			r.microstep(ts, stim)
			rested = false
		}
	}

	if r.done {
		r.complete()

		for _, t := range r.timers {
			r.effects = append(r.effects, r.bare(EffectCancelTimer, t.id, t.state(), ""))
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
		r.deliver()
	}

	r.sends = nil
	r.dropShortLivedTimers()
}

// outgoing is an event a transition sent to a service, waiting for the step
// to settle.
type outgoing struct {
	service string
	keyed   bool
	key     string
	event   any

	// leaving lists the runs the event is for that the same step stops:
	// they are told before they are stopped.
	leaving []string
}

// matches reports whether the event is for the given run.
func (o outgoing) matches(id, name string, resource bool) bool {
	return !resource && name == o.service && (!o.keyed || keyOf(id) == o.key)
}

// send queues an event for a service, noting the runs it is for that this
// step is stopping, or is about to as their state is exited.
func (r *run[S, C]) send(o outgoing) {
	for _, ended := range r.ended {
		if o.matches(ended.id, ended.inv.name, ended.inv.resource != nil) {
			o.leaving = append(o.leaving, ended.id)
		}
	}
	for _, running := range r.services {
		if o.matches(running.id, running.inv.name, running.inv.resource != nil) {
			o.leaving = append(o.leaving, running.id)
		}
	}

	r.sends = append(r.sends, o)
}

// deliver turns the events sent during the step into effects for the
// services that are running now that it has settled: those that were
// running before and still are, and those the step started. A service the
// step stopped is sent what was meant for it before it is stopped.
func (r *run[S, C]) deliver() {
	for _, send := range r.sends {
		for _, running := range r.services {
			if !send.matches(running.id, running.inv.name, running.inv.resource != nil) {
				continue
			}

			e := r.bare(EffectSend, running.id, running.inv.state, running.inv.name)
			e.Event = send.event
			r.effects = append(r.effects, e)
		}

		// A run the step stopped hears the event first: the send goes in
		// just before its stop.
		for _, id := range send.leaving {
			stop := slices.IndexFunc(r.effects, func(e Effect[C]) bool { return e.Kind == EffectStopInvocation && e.ID == id })
			if stop < 0 {
				continue
			}

			e := r.effects[stop]
			e.Kind, e.Event = EffectSend, send.event
			r.effects = slices.Insert(r.effects, stop, e)
		}
	}
}

// complete runs what the active states, innermost first, and then the chart
// itself do on exit, for a machine that has just finished. The states stay
// in the snapshot, which goes on saying where the machine ended.
func (r *run[S, C]) complete() {
	names := r.activeNames()
	move := Move[S]{Source: r.m.root.id, From: r.m.stateIDs(names), To: r.m.stateIDs(names), Event: r.cause}

	leaving := slices.Clone(r.active())
	slices.Reverse(leaving)

	for _, n := range append(leaving, r.m.root) {
		for _, assign := range n.exitAssigns {
			r.ctx = assign.fn(r.ctx)
		}

		for _, a := range n.exit {
			r.effects = append(r.effects, Effect[C]{Kind: EffectAction, ID: "", State: n.name, Name: a.name, Delay: 0, At: time.Time{}, Jitter: 0, Context: r.ctx, Event: nil, From: names, To: names, Needs: nil, Saved: nil})
		}

		for k, name := range n.exitRuns {
			move.Source = n.id
			if cond := n.exitConds[k]; cond == nil || cond(move) {
				e := r.chartActionEffect(n, name, r.namedCause())
				e.From, e.To = names, names
				r.effects = append(r.effects, e)
			}
		}

		r.message(n, nil, n.exitEmits)
	}
}

// startServices starts the services of entered states that are still active
// and not already running one.
func (r *run[S, C]) startServices() {
	slices.SortFunc(r.entered, byOrder[S, C])

	for _, n := range slices.Compact(r.entered) {
		if !r.config[n.order] {
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
			if inv.resource == nil && !inv.spawned && !r.running(inv) {
				r.startService(inv)
			}
		}
	}

	r.entered = nil

	// Each resource acquired in this call lets the next one declared on
	// its state start, if the state is still active.
	for _, got := range r.acquired {
		// The state may have been left and entered again since, in which
		// case its resources are being acquired from the first again.
		held := slices.ContainsFunc(r.services, func(s service[S, C]) bool { return s.inv == got && s.acquired })
		if !r.config[got.state.order] || !held {
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

	// A service started and stopped within the same call never runs
	// either: its start, its stop and anything sent to it are dropped.
	begun := map[string]bool{}
	ended := map[string]bool{}

	for _, e := range r.effects {
		switch {
		case e.Kind == EffectStartInvocation:
			begun[e.ID] = true
		case e.Kind == EffectStopInvocation && begun[e.ID]:
			ended[e.ID] = true
		}
	}

	r.effects = slices.DeleteFunc(r.effects, func(e Effect[C]) bool {
		switch e.Kind {
		case EffectStartTimer, EffectCancelTimer:
			return cancelled[e.ID]
		case EffectStartInvocation, EffectStopInvocation, EffectSend:
			return ended[e.ID]
		case EffectAction, EffectAcquire, EffectRelease, EffectEmit:
		}

		return false
	})
}

// spawnable returns the spawned services named name whose states are
// active.
func (r *run[S, C]) spawnable(name string) []*invocation[S, C] {
	var out []*invocation[S, C]
	for _, n := range append([]*node[S, C]{r.m.root}, r.active()...) {
		for _, inv := range n.invocations {
			if inv.spawned && inv.name == name {
				out = append(out, inv)
			}
		}
	}

	return out
}

// keyOf is SpawnKey for a run known to be spawned, and "" for any other.
func keyOf(id string) string {
	key, _ := SpawnKey(id)
	return key
}

// SpawnKey returns the key of the spawned service run with the given ID,
// the one its [Transition.Spawn] gave it, and reports whether the ID is a
// spawned run's at all. A transition reads it from the ID of the [Done],
// [Failed] or [Emitted] event it handles, and a service from
// [RunID].
func SpawnKey(id string) (string, bool) {
	slash := strings.LastIndex(id, "/")
	if slash < 0 || slash < strings.LastIndex(id, "#") {
		return "", false
	}

	key, err := url.PathUnescape(id[slash+1:])
	if err != nil {
		return "", false
	}

	return key, true
}

// inert reports whether ev, arriving at now, certainly changes nothing in
// snap: no timer is due, and no active state has a transition that could be
// taken, either for an event of its type or without an event. It errs on
// the side of false; a false answer only costs the full step.
func (m *Machine[S, C]) inert(snap Snapshot[S, C], ev any, now time.Time) bool {
	if snap.machine != m.id || snap.hash != m.hash {
		return false
	}

	for _, t := range snap.timers {
		if !t.due.Add(time.Duration(t.jitter * float64(t.delay))).After(now) {
			return false
		}
	}

	var kind reflect.Type
	if ev != nil {
		switch ev.(type) {
		case TimerFired, Emitted, Checkpointed, acquisition, invocationResult:
			return false
		}
		kind = reflect.TypeOf(ev)
	}

	if m.root.reacts(kind) {
		return false
	}

	for _, n := range snap.nodes {
		if n.reacts(kind) {
			return false
		}
	}

	return true
}

// at returns the state a stored timer or service belongs to.
func (m *Machine[S, C]) at(state S, root bool) *node[S, C] {
	if root {
		return m.root
	}

	return m.nodes[state]
}

// outputs gathers what the regions of parallel state p report, by region
// name: the output of the final state each ended in, nil where that state
// declares none, and for a region that is itself parallel the outputs of
// its own regions.
func (m *Machine[S, C]) outputs(p *node[S, C], config []bool, c C) map[string]any {
	out := make(map[string]any, len(p.states()))
	for _, region := range p.states() {
		out[region.name] = m.outputOf(region, config, c)
	}

	return out
}

// outputOf is what one completed state reports.
func (m *Machine[S, C]) outputOf(n *node[S, C], config []bool, c C) any {
	switch n.kind {
	case kindParallel:
		return m.outputs(n, config, c)
	case kindFinal:
		if n.outputValue != nil {
			return n.outputValue(c)
		}

		return nil
	case kindState, kindHistory:
	}

	for _, child := range n.children {
		if child.kind == kindFinal && config[child.order] && child.outputValue != nil {
			return child.outputValue(c)
		}
	}

	return nil
}

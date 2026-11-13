package sc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"reflect"
)

// Machine is an immutable statechart over state identifiers S and context C.
// It is safe for concurrent use.
type Machine[S comparable, C any] struct {
	// id names the chart in snapshots and exports.
	id string

	// version is the caller-declared chart version.
	version int

	// hash fingerprints the chart's structure and names.
	hash string

	// migrate upgrades snapshots taken by another version of the chart.
	migrate func(RawSnapshot) (RawSnapshot, error)

	// root is the implicit state containing every top-level state.
	root *node[S, C]

	// nodes indexes states by identifier.
	nodes map[S]*node[S, C]

	// names indexes states by the text form of their identifier.
	names map[string]*node[S, C]

	// ordered lists states in document order.
	ordered []*node[S, C]

	// eventNames maps each event type the chart handles to its name.
	eventNames map[reflect.Type]string

	// eventTypes is the inverse of eventNames.
	eventTypes map[string]reflect.Type

	// actions are the chart actions declared with Builder.Action, with
	// their implementations; nil for one not yet bound.
	actions map[string]chartAction[S, C]

	// bound holds the service and resource implementations given to
	// Bind, by name.
	bound map[string]boundService[C]

	// boundReleases holds the resource release functions given to Bind.
	boundReleases map[string]func(context.Context, any, Leave[S])

	// messaging is set when a transition sends to a service or emits.
	messaging bool
}

// Messaging reports whether the chart sends events to its services, with
// [Transition.SendTo], or to whoever runs it, with [Transition.Emit]. A
// driver that cannot deliver such events checks it before running the
// chart.
func (m *Machine[S, C]) Messaging() bool { return m.messaging }

// options holds what the Option values passed to New configure.
type options struct {
	version int
	migrate func(RawSnapshot) (RawSnapshot, error)
}

// Option configures a [Machine] at construction.
type Option func(*options)

// Version declares the chart's version. Snapshots record it, and
// [Machine.Restore] runs the [Migrate] hook on snapshots that carry a
// different one. Raise it whenever the chart changes in a way that existing
// snapshots must be adapted to. The default version is zero.
func Version(v int) Option {
	return func(o *options) { o.version = v }
}

// Migrate sets the hook that upgrades a snapshot taken by another version of
// the chart. It receives the stored snapshot, with the version it was taken
// at, and returns one that is valid for the current chart. Active may list
// only leaf states; their ancestors are filled in.
func Migrate(fn func(old RawSnapshot) (RawSnapshot, error)) Option {
	return func(o *options) { o.migrate = fn }
}

// New builds a machine from the states declared by def. It returns every
// declaration error it finds, joined: unknown targets, missing initial
// states, unreachable states, duplicate names, and transitions that do
// nothing.
func New[S comparable, C any](id string, def func(b *Builder[S, C]), opts ...Option) (*Machine[S, C], error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	root := new(node[S, C])
	root.name = id
	root.kind = kindState
	root.root = true

	b := &build[S, C]{
		root:     root,
		nodes:    map[S]*node[S, C]{},
		ordered:  nil,
		errs:     nil,
		actions:  map[string]chartAction[S, C]{},
		kinds:    map[string]bool{},
		raises:   map[string]bool{},
		cancels:  nil,
		sends:    nil,
		emits:    false,
		releases: nil,
	}
	def(&Builder[S, C]{b: b, parent: root})

	m := &Machine[S, C]{
		id:            id,
		version:       o.version,
		hash:          "",
		migrate:       o.migrate,
		root:          root,
		nodes:         b.nodes,
		names:         map[string]*node[S, C]{},
		ordered:       b.ordered,
		eventNames:    map[reflect.Type]string{},
		eventTypes:    map[string]reflect.Type{},
		actions:       b.actions,
		bound:         map[string]boundService[C]{},
		boundReleases: map[string]func(context.Context, any, Leave[S]){},
		messaging:     false,
	}

	if id == "" {
		b.errorf("machine id is empty")
	}

	m.resolve(b)
	for _, name := range b.cancels {
		if !b.raises[name] {
			b.errorf("CancelRaise names %q, which no RaiseAfter declares", name)
		}
	}
	for _, name := range b.sends {
		if resource, ok := b.kinds[name]; !ok || resource {
			b.errorf("SendTo names %q, which is not a service of the chart", name)
		}
	}
	for _, name := range b.releases {
		if resource, ok := b.kinds[name]; !ok || !resource {
			b.errorf("Release names %q, which is not a resource of the chart", name)
		}
	}
	m.messaging = len(b.sends) > 0 || b.emits
	m.registerEvents(b)
	if len(b.errs) == 0 {
		m.checkReachable(b)
	}

	if len(b.errs) > 0 {
		return nil, fmt.Errorf("statechart %q: %w", id, errors.Join(b.errs...))
	}

	h := sha256.New()
	m.fingerprint(h, root)
	m.hash = hex.EncodeToString(h.Sum(nil))

	return m, nil
}

// ID returns the machine's identifier.
func (m *Machine[S, C]) ID() string { return m.id }

// resolve links identifiers to nodes and checks each state's declaration.
func (m *Machine[S, C]) resolve(b *build[S, C]) {
	for _, n := range m.ordered {
		if other, ok := m.names[n.name]; ok && other != n {
			b.errorf("state %q: two state identifiers share this text form", n.name)
		}
		m.names[n.name] = n
	}

	for _, n := range append([]*node[S, C]{m.root}, m.ordered...) {
		m.resolveStructure(b, n)
		m.resolveBehaviour(b, n)
	}
}

func (m *Machine[S, C]) resolveStructure(b *build[S, C], n *node[S, C]) {
	states := n.states()

	switch {
	case n.kind == kindParallel && len(states) == 0:
		b.errorf("parallel state %q: has no regions", n.name)
	case n.kind == kindState && len(states) > 0 && n.initialID == nil:
		b.errorf("state %q: has child states but no initial state", n.name)
	case n.kind == kindState && len(states) == 0 && n.root:
		b.errorf("machine declares no states")
	case n.initialID != nil:
		init, ok := m.nodes[*n.initialID]
		if !ok || init.parent != n || init.kind == kindHistory {
			b.errorf("state %q: initial state %q is not one of its child states", n.name, fmt.Sprint(*n.initialID))
			break
		}
		n.initial = init
	}

	if n.kind == kindFinal && n.parent.kind == kindParallel {
		b.errorf("final state %q: declared directly inside parallel state %q; put it inside a region", n.name, n.parent.name)
	}

	if n.kind != kindHistory {
		return
	}

	if n.defaultID == nil {
		return
	}

	def, ok := m.nodes[*n.defaultID]
	if !ok || def.kind == kindHistory || !def.below(n.parent) {
		b.errorf("history state %q: default %q is not a state inside %q", n.name, fmt.Sprint(*n.defaultID), n.parent.name)
		return
	}
	n.defaultNode = def
}

func (m *Machine[S, C]) resolveBehaviour(b *build[S, C], n *node[S, C]) {
	actions := map[string]bool{}
	claim := func(name string) {
		if name == "" {
			b.errorf("state %q: an action has no name", n.name)
			return
		}
		if actions[name] {
			b.errorf("state %q: more than one action is named %q", n.name, name)
		}
		if _, ok := m.actions[name]; ok {
			b.errorf("state %q: action %q has the name of a chart action", n.name, name)
		}
		actions[name] = true
	}
	known := func(name string) {
		if _, ok := m.actions[name]; !ok {
			b.errorf("state %q: runs chart action %q, which is not declared", n.name, name)
		}
	}

	for _, name := range n.entryRuns {
		known(name)
	}
	for _, name := range n.exitRuns {
		known(name)
	}

	for _, a := range n.entry {
		claim(a.name)
	}
	for _, a := range n.exit {
		claim(a.name)
	}

	services := map[string]bool{}
	for _, inv := range n.invocations {
		if inv.name == "" || services[inv.name] {
			b.errorf("state %q: service name %q is empty or declared twice", n.name, inv.name)
		}
		services[inv.name] = true

		// Implementations are bound by name, so one name cannot stand
		// for a service in one state and a resource in another.
		if resource, ok := b.kinds[inv.name]; ok && resource != (inv.resource != nil) {
			b.errorf("state %q: %q names both a service and a resource in the chart", n.name, inv.name)
		}
		b.kinds[inv.name] = inv.resource != nil

		routed := false
		for _, t := range n.transitions {
			routed = routed || (t.trigger == triggerInvokeError && t.invocation == inv)
		}
		if !routed {
			b.errorf("state %q: service %q has no OnError transition", n.name, inv.name)
		}
	}

	for _, t := range n.transitions {
		if t.trigger == triggerAfter && (t.delayFn != nil || t.deadlineFn != nil) && t.delayName == "" {
			b.errorf("state %q: a delay has no name", n.name)
		}

		t.inNodes = m.resolveConditions(b, n, "WhenIn", t.inIDs)
		t.notInNodes = m.resolveConditions(b, n, "WhenNotIn", t.notInIDs)

		for _, g := range t.guards {
			if g.name == "" {
				b.errorf("state %q: a guard has no name", n.name)
			}
		}

		for _, s := range t.steps {
			switch {
			case s.kind == stepDo:
				claim(s.name)
			case s.kind == stepRun:
				known(s.name)
			case s.name == "":
				b.errorf("state %q: a transition %s has no name", n.name, s.kind)
			}
		}

		if t.stay && (t.targetID != nil || len(t.steps) > 0) {
			b.errorf("state %q: a transition marked Stay has a target or steps", n.name)
		}

		if t.targetID == nil {
			if len(t.steps) == 0 && !t.stay {
				b.errorf("state %q: a transition has neither a target nor any steps; mark it Stay to drop its event on purpose", n.name)
			}
			if t.reenter {
				b.errorf("state %q: a transition without a target cannot re-enter", n.name)
			}
			continue
		}

		target, ok := m.nodes[*t.targetID]
		if !ok {
			b.errorf("state %q: transition targets undeclared state %q", n.name, fmt.Sprint(*t.targetID))
			continue
		}
		t.target = target
	}
}

// EventName returns the name ev is persisted and exported under: the name
// the chart registered for its type, or the type's own name for an event
// the chart does not handle.
func (m *Machine[S, C]) EventName(ev any) string {
	t := reflect.TypeOf(ev)
	if t == nil {
		return "<nil>"
	}

	if name, ok := m.eventNames[t]; ok {
		return name
	}

	return eventTypeName(t)
}

// resolveConditions resolves the states a WhenIn or WhenNotIn names.
func (m *Machine[S, C]) resolveConditions(b *build[S, C], n *node[S, C], method string, ids []S) []*node[S, C] {
	var nodes []*node[S, C]
	for _, id := range ids {
		in, ok := m.nodes[id]
		if !ok || in.kind == kindHistory {
			b.errorf("state %q: %s names %q, which is not a state of the chart", n.name, method, fmt.Sprint(id))
			continue
		}
		nodes = append(nodes, in)
	}

	return nodes
}

// eventNamer lets an event type choose the name it is exported and persisted
// under. Without it the Go type name is used.
type eventNamer interface {
	EventName() string
}

// registerEvents names every event type the chart can carry in an effect.
func (m *Machine[S, C]) registerEvents(b *build[S, C]) {
	register := func(t reflect.Type, name string) {
		if existing, ok := m.eventTypes[name]; ok && existing != t {
			b.errorf("event name %q is used by both %s and %s", name, existing, t)
			return
		}
		m.eventNames[t] = name
		m.eventTypes[name] = t
	}

	register(reflect.TypeFor[Failed](), "sc.failed")

	for _, n := range m.ordered {
		for _, inv := range n.invocations {
			if inv.resource != nil {
				register(inv.doneType, "sc.acquired:"+inv.doneType.String())
				continue
			}

			register(inv.doneType, "sc.done:"+inv.doneType.String())
		}

		for _, t := range n.transitions {
			if t.trigger == triggerEvent && !t.wildcard {
				register(t.eventType, eventTypeName(t.eventType))
			}
		}
	}
}

func eventTypeName(t reflect.Type) string {
	var zero reflect.Value
	if t.Kind() == reflect.Pointer {
		zero = reflect.New(t.Elem())
	} else {
		zero = reflect.Zero(t)
	}

	if namer, ok := zero.Interface().(eventNamer); ok {
		return namer.EventName()
	}

	if name := t.Name(); name != "" {
		return name
	}

	return t.String()
}

// checkReachable reports states no transition can ever enter. It ignores
// guards, so it finds only states that are structurally unreachable.
func (m *Machine[S, C]) checkReachable(b *build[S, C]) {
	reached := map[*node[S, C]]bool{m.root: true}

	var mark func(n *node[S, C])
	mark = func(n *node[S, C]) {
		if n == nil || reached[n] {
			return
		}
		reached[n] = true
		mark(n.parent)

		switch n.kind {
		case kindState:
			mark(n.initial)
		case kindParallel:
			for _, c := range n.states() {
				mark(c)
			}
		case kindHistory:
			// A history state can restore anything its parent could
			// have been in, which the fixpoint below already covers.
			// What it adds is its default.
			if n.defaultNode != nil {
				mark(n.defaultNode)
			}
		case kindFinal:
		}
	}

	mark(m.root.initial)

	for changed := true; changed; {
		before := len(reached)
		for _, n := range m.ordered {
			if !reached[n] {
				continue
			}
			for _, t := range n.transitions {
				mark(t.target)
			}
		}
		changed = len(reached) != before
	}

	for _, n := range m.ordered {
		if !reached[n] && n.kind != kindHistory {
			b.errorf("state %q: unreachable", n.name)
		}
	}
}

// fingerprint writes a canonical description of n and its descendants. Two
// charts with the same fingerprint accept each other's snapshots.
func (m *Machine[S, C]) fingerprint(w io.Writer, n *node[S, C]) {
	write := func(format string, args ...any) {
		// A hash.Hash never returns an error from Write.
		_, _ = fmt.Fprintf(w, format, args...)
	}

	write("state %q %s %s", n.name, n.kind, n.historyKind)
	if n.initial != nil {
		write(" initial=%q", n.initial.name)
	}
	if n.defaultNode != nil {
		write(" default=%q", n.defaultNode.name)
	}
	if n.budget != 0 {
		write(" budget=%d", n.budget)
	}
	for _, tag := range n.tags {
		write(" tag=%q", tag)
	}
	write("\n")

	for _, a := range n.entry {
		write("entry %q\n", a.name)
	}
	for _, a := range n.exit {
		write("exit %q\n", a.name)
	}
	for k, name := range n.entryRuns {
		write("entry run %q if=%t\n", name, n.entryConds[k] != nil)
	}
	for k, name := range n.exitRuns {
		write("exit run %q if=%t\n", name, n.exitConds[k] != nil)
	}
	for _, inv := range n.invocations {
		write("invoke %q %s resource=%t", inv.name, inv.doneType, inv.resource != nil)
		if p := inv.retry; p != nil {
			write(" retry=%d first=%d cap=%d jitter=%v if=%t", p.Max, p.First, p.Cap, p.Jitter, p.If != nil)
		}
		write("\n")
	}

	for _, t := range n.transitions {
		write("on %s", t.trigger)
		if t.eventType != nil {
			write(" event=%q", m.eventNames[t.eventType])
		}
		if t.wildcard {
			write(" any")
		}
		if t.invocation != nil {
			write(" service=%q", t.invocation.name)
		}
		if t.trigger == triggerAfter {
			write(" delay=%d", t.delay)
		}
		if t.delayName != "" {
			write(" computed=%q deadline=%t", t.delayName, t.deadlineFn != nil)
		}
		if t.jitter != 0 {
			write(" jitter=%v", t.jitter)
		}
		for _, in := range t.inNodes {
			write(" in=%q", in.name)
		}
		for _, out := range t.notInNodes {
			write(" notin=%q", out.name)
		}
		for _, g := range t.guards {
			write(" guard=%q", g.name)
		}
		for k, s := range t.steps {
			write(" %s=%q", s.kind, s.name)
			if t.runConds[k] != nil {
				write(" if")
			}
			if d, ok := t.raiseDelays[k]; ok {
				write(" after=%d", d)
			}
			if to, ok := t.sendTargets[k]; ok {
				write(" to=%q", to)
			}
		}
		if t.target != nil {
			write(" target=%q reenter=%t", t.target.name, t.reenter)
		}
		if t.stay {
			write(" stay")
		}
		write("\n")
	}

	for _, c := range n.children {
		m.fingerprint(w, c)
	}
	write("end\n")
}

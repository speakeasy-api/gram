package sc

import (
	"strconv"
	"strings"
)

// Definition describes a chart in the shape of an XState machine
// configuration. Marshalled to JSON it can be loaded by tools that read that
// format, with guards, actions, and services referred to by name.
//
// JSON objects are unordered, so the export does not preserve the
// declaration order of states. Region priority, which follows declaration
// order, is therefore not part of the export.
type Definition struct {
	// ID is the machine ID on the root and the state name elsewhere.
	// Transition targets refer to it as "#ID".
	ID string `json:"id"`

	// Type is "parallel", "final", or "history". It is empty for atomic
	// and compound states.
	Type string `json:"type,omitempty"`

	// History is "shallow" or "deep" on history states.
	History string `json:"history,omitempty"`

	// Target is the default target of a history state.
	Target string `json:"target,omitempty"`

	// Initial is the key of the initial child state.
	Initial string `json:"initial,omitempty"`

	// Entry lists the entry action names.
	Entry []string `json:"entry,omitempty"`

	// Exit lists the exit action names.
	Exit []string `json:"exit,omitempty"`

	// On maps event names to their transitions.
	On map[string][]TransitionDefinition `json:"on,omitempty"`

	// Always lists the eventless transitions.
	Always []TransitionDefinition `json:"always,omitempty"`

	// After maps delays to their transitions. A fixed delay is keyed by its
	// length in milliseconds, and a computed delay or deadline by its name,
	// as XState keys a named delay.
	After map[string][]TransitionDefinition `json:"after,omitempty"`

	// OnDone lists the transitions taken when the state completes.
	OnDone []TransitionDefinition `json:"onDone,omitempty"`

	// Invoke lists the services that run while the state is active.
	Invoke []InvokeDefinition `json:"invoke,omitempty"`

	// States maps child state names to their definitions.
	States map[string]*Definition `json:"states,omitempty"`
}

// TransitionDefinition describes one transition.
type TransitionDefinition struct {
	// Target is "#" followed by the target state's ID. It is empty for a
	// transition without a target.
	Target string `json:"target,omitempty"`

	// Guard names the transition's conditions, joined with &&: each guard
	// by its name, a [Transition.WhenIn] state as in(#state), and a
	// [Transition.WhenNotIn] state as !in(#state). XState has no string
	// form for this, so a tool reading the export sees it as one named
	// guard.
	Guard string `json:"guard,omitempty"`

	// Actions lists the assignment, action, and raise names in order.
	Actions []string `json:"actions,omitempty"`

	// Reenter is set when the transition exits and re-enters its source.
	Reenter bool `json:"reenter,omitempty"`
}

// InvokeDefinition describes one invoked service.
type InvokeDefinition struct {
	// ID is the service name.
	ID string `json:"id"`

	// Src is the service name, which is what the implementation is
	// registered under.
	Src string `json:"src"`

	// OnDone lists the transitions taken when the service succeeds.
	OnDone []TransitionDefinition `json:"onDone,omitempty"`

	// OnError lists the transitions taken when the service fails.
	OnError []TransitionDefinition `json:"onError,omitempty"`
}

// Definition returns a description of the chart for export.
//
// The format has no place for a condition on a single action, so a chart
// action run conditionally, with RunIf, OnEntryIf or OnExitIf, is listed
// like one that always runs.
func (m *Machine[S, C]) Definition() *Definition {
	return m.define(m.root)
}

func (m *Machine[S, C]) define(n *node[S, C]) *Definition {
	def := &Definition{
		ID:      n.name,
		Type:    "",
		History: string(n.historyKind),
		Target:  "",
		Initial: "",
		Entry:   nil,
		Exit:    nil,
		On:      nil,
		Always:  nil,
		After:   nil,
		OnDone:  nil,
		Invoke:  nil,
		States:  nil,
	}

	if n.kind != kindState {
		def.Type = string(n.kind)
	}
	if n.defaultNode != nil {
		def.Target = "#" + n.defaultNode.name
	}
	if n.initial != nil {
		def.Initial = n.initial.name
	}

	for _, a := range n.entry {
		def.Entry = append(def.Entry, a.name)
	}
	for _, a := range n.exit {
		def.Exit = append(def.Exit, a.name)
	}
	def.Entry = append(def.Entry, n.entryRuns...)
	def.Exit = append(def.Exit, n.exitRuns...)

	invokes := map[*invocation[S, C]]int{}
	for _, inv := range n.invocations {
		invokes[inv] = len(def.Invoke)
		def.Invoke = append(def.Invoke, InvokeDefinition{ID: inv.name, Src: inv.name, OnDone: nil, OnError: nil})
	}

	for _, t := range n.transitions {
		td := TransitionDefinition{Target: "", Guard: "", Actions: nil, Reenter: t.reenter}
		if t.target != nil {
			td.Target = "#" + t.target.name
		}
		var conditions []string
		for _, g := range t.guards {
			conditions = append(conditions, g.name)
		}
		for _, in := range t.inNodes {
			conditions = append(conditions, "in(#"+in.name+")")
		}
		for _, out := range t.notInNodes {
			conditions = append(conditions, "!in(#"+out.name+")")
		}
		td.Guard = strings.Join(conditions, " && ")
		for _, s := range t.steps {
			td.Actions = append(td.Actions, s.name)
		}

		switch t.trigger {
		case triggerEvent:
			if def.On == nil {
				def.On = map[string][]TransitionDefinition{}
			}
			name := m.eventNames[t.eventType]
			def.On[name] = append(def.On[name], td)
		case triggerAlways:
			def.Always = append(def.Always, td)
		case triggerAfter:
			if def.After == nil {
				def.After = map[string][]TransitionDefinition{}
			}
			key := strconv.FormatInt(t.delay.Milliseconds(), 10)
			if t.delayName != "" {
				key = t.delayName
			}
			def.After[key] = append(def.After[key], td)
		case triggerStateDone:
			def.OnDone = append(def.OnDone, td)
		case triggerInvokeDone, triggerAcquired:
			inv := &def.Invoke[invokes[t.invocation]]
			inv.OnDone = append(inv.OnDone, td)
		case triggerInvokeError:
			inv := &def.Invoke[invokes[t.invocation]]
			inv.OnError = append(inv.OnError, td)
		}
	}

	for _, c := range n.children {
		if def.States == nil {
			def.States = map[string]*Definition{}
		}
		def.States[c.name] = m.define(c)
	}

	return def
}

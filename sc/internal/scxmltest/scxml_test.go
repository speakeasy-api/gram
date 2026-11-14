// Package scxmltest runs sc against the SCXML conformance fixtures of the
// SCION test framework: charts written in SCXML, each with a script of
// events and the states that must be active after every one. They are an
// oracle for the core's semantics that owes nothing to this repository.
//
// Only fixtures that need no scripting are included: no conditions, data
// model or send. The loader turns each chart into an sc chart, mapping
// SCXML's default external transitions onto [sc.Transition.Reenter], which
// keeps SCXML's rule for what a transition exits.
package scxmltest

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// unsupported lists the fixtures sc cannot express, each with the feature it
// lacks. They are skipped, and the list is the record of those gaps.
var unsupported = map[string]string{}

// deviations lists the fixtures where sc deliberately behaves differently
// from SCXML, each with the reason. A listed fixture that passes fails the
// test, so the list cannot go stale.
var deviations = map[string]string{}

// errUnsupported marks a chart that uses something sc has no form for.
var errUnsupported = errors.New("not expressible in sc")

// element is an XML element with its attributes and child elements.
type element struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []element  `xml:",any"`
}

func (e element) attr(name string) (string, bool) {
	for _, a := range e.Attrs {
		if a.Name.Local == name {
			return a.Value, true
		}
	}

	return "", false
}

// state is one SCXML state. entry and exit list its executable content:
// the name of each raised event, or "" for a log.
type state struct {
	id       string
	kind     string
	children []*state
	initial  string
	deep     bool
	target   string
	entry    []string
	exit     []string
	arrows   []arrow
}

// arrow is one SCXML transition.
type arrow struct {
	events   []string
	targets  []string
	internal bool

	// content lists the executable content: the name of each raised event,
	// or "" for a log.
	content []string
}

// event is the one Go type every SCXML event is sent as.
type event struct {
	Name string
}

func (s *state) compound() bool {
	return slices.ContainsFunc(s.children, func(c *state) bool { return c.kind != "history" })
}

// parse reads the states under e.
func parse(e element, anon *int) (*state, error) {
	s := &state{id: "", kind: e.XMLName.Local, children: nil, initial: "", deep: false, target: "", entry: nil, exit: nil, arrows: nil}

	s.id, _ = e.attr("id")
	if s.id == "" {
		*anon++
		s.id = fmt.Sprintf("$anonymous%d", *anon)
	}

	s.initial, _ = e.attr("initial")
	if kind, _ := e.attr("type"); kind == "deep" {
		s.deep = true
	}

	for _, child := range e.Children {
		switch child.XMLName.Local {
		case "state", "parallel", "final", "history":
			sub, err := parse(child, anon)
			if err != nil {
				return nil, err
			}
			s.children = append(s.children, sub)
		case "initial":
			for _, t := range child.Children {
				s.initial, _ = t.attr("target")
				if len(t.Children) > 0 {
					return nil, fmt.Errorf("%w: content in an initial transition", errUnsupported)
				}
			}
		case "onentry", "onexit":
			for _, c := range child.Children {
				raised, _ := c.attr("event")

				if child.XMLName.Local == "onentry" {
					s.entry = append(s.entry, raised)
				} else {
					s.exit = append(s.exit, raised)
				}
			}
		case "transition":
			a := arrow{events: nil, targets: nil, internal: false, content: nil}

			if names, ok := child.attr("event"); ok {
				a.events = strings.Fields(names)
			}
			if targets, ok := child.attr("target"); ok {
				a.targets = strings.Fields(targets)
			}
			if kind, _ := child.attr("type"); kind == "internal" {
				a.internal = true
			}
			for _, c := range child.Children {
				name, _ := c.attr("event")
				a.content = append(a.content, name)
			}

			if s.kind == "history" {
				if len(a.targets) != 1 {
					return nil, fmt.Errorf("%w: a history default with %d targets", errUnsupported, len(a.targets))
				}
				s.target = a.targets[0]

				continue
			}

			s.arrows = append(s.arrows, a)
		}
	}

	return s, nil
}

// matches reports whether an event named name matches one of the SCXML
// event descriptors: the name itself, any prefix of its dot-separated
// tokens, or "*".
func matches(descriptors []string, name string) bool {
	for _, d := range descriptors {
		d = strings.TrimSuffix(strings.TrimSuffix(d, "*"), ".")
		if d == "" || d == name || strings.HasPrefix(name, d+".") {
			return true
		}
	}

	return false
}

// chart is an SCXML document loaded as an sc machine.
type chart struct {
	m      *sc.Machine[string, struct{}]
	atomic map[string]bool
}

// load builds the sc machine for the SCXML document at path.
func load(path string) (*chart, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read chart: %w", err)
	}

	var doc element
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse chart: %w", err)
	}

	anon := 0
	root, err := parse(doc, &anon)
	if err != nil {
		return nil, err
	}

	c := &chart{m: nil, atomic: map[string]bool{}}

	var problems []error
	c.m, err = sc.New("scxml", func(b *sc.Builder[string, struct{}]) {
		problems = append(problems, c.declare(b, root)...)
	}, sc.AllowUnreachable(), sc.StrictSCXML())
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	if err != nil {
		return nil, fmt.Errorf("build chart: %w", err)
	}

	return c, nil
}

// declare declares the children of parent, and its initial state.
func (c *chart) declare(b *sc.Builder[string, struct{}], parent *state) []error {
	var problems []error

	if parent.kind != "parallel" && parent.compound() {
		initial := parent.initial
		if initial == "" {
			initial = parent.children[slices.IndexFunc(parent.children, func(s *state) bool { return s.kind != "history" })].id
		}

		if !slices.ContainsFunc(parent.children, func(s *state) bool { return s.id == initial }) {
			return []error{fmt.Errorf("%w: an initial state that is not a direct child", errUnsupported)}
		}

		b.Initial(initial)
	}

	for _, s := range parent.children {
		nested := func(b *sc.Builder[string, struct{}]) { problems = append(problems, c.declare(b, s)...) }

		switch s.kind {
		case "history":
			kind := sc.Shallow
			if s.deep {
				kind = sc.Deep
			}

			h := b.History(s.id, kind)
			if s.target != "" {
				h.Default(s.target)
			}
		case "final":
			c.atomic[s.id] = true

			f := b.Final(s.id)
			for i, raised := range s.entry {
				if raised != "" {
					problems = append(problems, fmt.Errorf("%w: an event raised on entering a final state", errUnsupported))
					continue
				}

				f.Entry(fmt.Sprintf("entry%d", i), func(context.Context, struct{}) {})
			}
		case "parallel":
			problems = append(problems, c.behave(b.Parallel(s.id, nested), s)...)
		default:
			if !s.compound() {
				c.atomic[s.id] = true
				problems = append(problems, c.behave(b.State(s.id), s)...)

				continue
			}

			problems = append(problems, c.behave(b.State(s.id, nested), s)...)
		}
	}

	return problems
}

// behave declares the actions and transitions of s.
func (c *chart) behave(sb *sc.StateBuilder[string, struct{}], s *state) []error {
	var problems []error

	for i, raised := range s.entry {
		if raised != "" {
			sb.RaiseOnEntry(fmt.Sprintf("entry%d", i), func(struct{}) any { return event{Name: raised} })
			continue
		}

		sb.Entry(fmt.Sprintf("entry%d", i), func(context.Context, struct{}) {})
	}
	for i, raised := range s.exit {
		if raised != "" {
			sb.RaiseOnExit(fmt.Sprintf("exit%d", i), func(struct{}) any { return event{Name: raised} })
			continue
		}

		sb.Exit(fmt.Sprintf("exit%d", i), func(context.Context, struct{}) {})
	}

	for i, a := range s.arrows {
		switch {
		case slices.ContainsFunc(a.events, func(name string) bool { return strings.HasPrefix(name, "done.") }):
			problems = append(problems, fmt.Errorf("%w: a transition on a done event by name", errUnsupported))
		case len(a.events) == 0:
			finish(sb.Always(), s, a, i)
		default:
			t := sb.On[event]().When("named", func(_ struct{}, ev event) bool { return matches(a.events, ev.Name) })
			finish(t, s, a, i)
		}
	}

	return problems
}

// finish gives a transition its content and target.
func finish[Ev any](t *sc.Transition[string, struct{}, Ev], s *state, a arrow, index int) {
	for i, raised := range a.content {
		if raised == "" {
			t.Do(fmt.Sprintf("log%d.%d", index, i), func(context.Context, struct{}, Ev) {})
			continue
		}

		t.Raise(fmt.Sprintf("raise%d.%d", index, i), func(struct{}, Ev) any { return event{Name: raised} })
	}

	if len(a.targets) == 0 {
		if len(a.content) == 0 {
			t.Stay()
		}

		return
	}

	t.To(a.targets[0], a.targets[1:]...)

	// An SCXML transition is external unless it says otherwise: it leaves
	// its source even for a target inside it. An internal one needs a
	// compound source and a target properly inside it.
	if !a.internal || s.kind != "state" || !s.compound() || slices.ContainsFunc(a.targets, func(id string) bool { return !s.contains(id) }) {
		t.Reenter()
	}
}

// contains reports whether id names a proper descendant of s.
func (s *state) contains(id string) bool {
	for _, child := range s.children {
		if child.id == id || child.contains(id) {
			return true
		}
	}

	return false
}

// leaves returns the active atomic states, sorted, which is how the
// fixtures describe a configuration.
func (c *chart) leaves(snap sc.Snapshot[string, struct{}]) []string {
	var out []string
	for _, id := range snap.Active() {
		if c.atomic[id] {
			out = append(out, id)
		}
	}
	slices.Sort(out)

	return out
}

// script is what a fixture expects of its chart.
type script struct {
	Initial []string `json:"initialConfiguration"`
	Events  []struct {
		Event struct {
			Name string `json:"name"`
		} `json:"event"`
		Next []string `json:"nextConfiguration"`
	} `json:"events"`
}

// run plays the fixture's events and returns the first mismatch, or "".
func run(c *chart, want script) string {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	snap, _ := c.m.Start(struct{}{}, now)
	if got := c.leaves(snap); !slices.Equal(got, sorted(want.Initial)) {
		return fmt.Sprintf("initially: want %v, got %v", sorted(want.Initial), got)
	}

	for i, step := range want.Events {
		snap, _ = c.m.Transition(snap, event{Name: step.Event.Name}, now)
		if got := c.leaves(snap); !slices.Equal(got, sorted(step.Next)) {
			return fmt.Sprintf("after event %d (%s): want %v, got %v", i+1, step.Event.Name, sorted(step.Next), got)
		}
	}

	return ""
}

func sorted(ids []string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)

	return out
}

func TestSCIONFixtures(t *testing.T) {
	t.Parallel()

	charts, err := filepath.Glob(filepath.Join("testdata", "scion", "*", "*.scxml"))
	require.NoError(t, err)
	require.NotEmpty(t, charts)

	for _, path := range charts {
		name := filepath.ToSlash(strings.TrimSuffix(strings.TrimPrefix(path, filepath.Join("testdata", "scion")+string(filepath.Separator)), ".scxml"))

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(strings.TrimSuffix(path, ".scxml") + ".json")
			require.NoError(t, err)

			var want script
			require.NoError(t, json.Unmarshal(data, &want))

			c, err := load(path)
			if reason, listed := unsupported[name]; listed {
				require.ErrorIs(t, err, errUnsupported, "listed as unsupported: %s", reason)
				t.Skip("sc cannot express this chart: " + reason)
			}
			require.NoError(t, err)

			mismatch := run(c, want)
			if reason, listed := deviations[name]; listed {
				require.NotEmpty(t, mismatch, "listed as a deviation but passes: %s", reason)
				t.Skip("sc deliberately differs from SCXML: " + reason)
			}
			require.Empty(t, mismatch)
		})
	}
}

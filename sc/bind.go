package sc

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"
)

// ErrNotBound is returned when a service or chart action that a chart
// declared with [StateBuilder.Requires] or [Builder.RequiresAction] runs
// without an implementation. Drivers deliver it to the chart as the
// service's failure.
var ErrNotBound = errors.New("service or action has no implementation")

// boundService is an implementation given to Bind.
type boundService[C any] struct {
	doneType reflect.Type
	run      func(ctx context.Context, c C, id string) iter.Seq[any]
}

// Binder gives implementations to the services and chart actions of a
// chart, inside [Machine.Bind].
type Binder[S comparable, C any] struct {
	m        *Machine[S, C]
	services map[string]boundService[C]
	actions  map[string]chartAction[S, C]
	errs     []error
}

// Service binds the service named name to fn. Every state that declares a
// service by that name, with [StateBuilder.Requires] or
// [StateBuilder.Invoke], runs fn; for one declared with Invoke this replaces
// its implementation, which is how a test substitutes a fake. R must be the
// result type the chart declared.
func (b *Binder[S, C]) Service[R any](name string, fn func(context.Context, C) (R, error)) {
	b.bind(name, reflect.TypeFor[Done[R]](), adaptService(fn))
}

// Stream binds the stream service named name to fn, as Service does for
// a service that returns one result.
func (b *Binder[S, C]) Stream(name string, fn func(context.Context, C) iter.Seq2[any, error]) {
	b.bind(name, reflect.TypeFor[Done[struct{}]](), adaptStream(fn))
}

// Action binds the chart action named name to fn, replacing any
// implementation it had.
func (b *Binder[S, C]) Action(name string, fn func(ctx context.Context, c C, mv Move[S])) {
	if _, ok := b.m.actions[name]; !ok {
		b.errs = append(b.errs, fmt.Errorf("chart action %q is not declared", name))
		return
	}

	b.actions[name] = fn
}

func (b *Binder[S, C]) bind(name string, doneType reflect.Type, run func(context.Context, C, string) iter.Seq[any]) {
	found := false
	for _, n := range b.m.ordered {
		for _, inv := range n.invocations {
			if inv.name != name {
				continue
			}

			found = true
			if inv.doneType != doneType {
				b.errs = append(b.errs, fmt.Errorf("service %q in state %q reports %s, not %s", name, n.name, inv.doneType, doneType))
			}
		}
	}

	if !found {
		b.errs = append(b.errs, fmt.Errorf("service %q is not declared", name))
		return
	}

	b.services[name] = boundService[C]{doneType: doneType, run: run}
}

// Bind returns a copy of the machine with the implementations def gives.
// The copy shares the chart, so snapshots and effects of either work with
// the other. Binding the same machine several ways, for example once per
// tenant with that tenant's credentials or once with fakes in a test, is
// cheap. Bind returns every binding error at once: an undeclared name, or a
// result type that does not match the declaration.
func (m *Machine[S, C]) Bind(def func(b *Binder[S, C])) (*Machine[S, C], error) {
	b := &Binder[S, C]{m: m, services: map[string]boundService[C]{}, actions: map[string]chartAction[S, C]{}, errs: nil}
	def(b)

	if len(b.errs) > 0 {
		return nil, fmt.Errorf("bind statechart %q: %w", m.id, errors.Join(b.errs...))
	}

	bound := *m
	bound.bound = make(map[string]boundService[C], len(m.bound)+len(b.services))
	maps.Copy(bound.bound, m.bound)
	maps.Copy(bound.bound, b.services)

	bound.actions = make(map[string]chartAction[S, C], len(m.actions))
	maps.Copy(bound.actions, m.actions)
	maps.Copy(bound.actions, b.actions)

	return &bound, nil
}

// Unbound lists the services and chart actions that have no implementation,
// sorted by name. A driver built from a machine with unbound entries fails
// them when they run; checking Unbound at start-up finds that sooner.
func (m *Machine[S, C]) Unbound() []string {
	var missing []string
	for _, n := range m.ordered {
		for _, inv := range n.invocations {
			if m.serviceRunner(inv) == nil && !slices.Contains(missing, "service "+inv.name) {
				missing = append(missing, "service "+inv.name)
			}
		}
	}

	for name, fn := range m.actions {
		if fn == nil {
			missing = append(missing, "action "+name)
		}
	}

	slices.Sort(missing)

	return missing
}

// serviceRunner returns the implementation a service runs with: a bound one
// if Bind gave one, otherwise the one it was declared with.
func (m *Machine[S, C]) serviceRunner(inv *invocation[S, C]) func(context.Context, C, string) iter.Seq[any] {
	if b, ok := m.bound[inv.name]; ok {
		return b.run
	}

	return inv.run
}

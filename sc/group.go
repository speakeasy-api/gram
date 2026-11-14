package sc

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sync"
)

// ErrNoInstance is returned by [Group.Send] for a key the group holds no
// instance for.
var ErrNoInstance = errors.New("no instance for key")

// Group holds many [Instance] values of one machine, keyed by K: one per
// host, per grant, per connection slot. It creates instances when asked,
// routes events to them by key, reads across all of them, and closes and
// forgets the ones a caller no longer needs. It is safe for concurrent use.
//
// A group does not decide when an instance should exist. A caller that caps
// how many instances may run checks [Group.Count] before [Group.Start];
// both happen under the caller's control, so a poll loop that owns the group
// can treat them as one decision.
type Group[K comparable, S comparable, C any] struct {
	m    *Machine[S, C]
	opts []RunOption

	mu sync.Mutex

	// instances holds the group's instances by key, guarded by mu.
	instances map[K]*Instance[S, C]

	// starting holds, for each key whose instance is being created, a
	// channel closed when that ends, guarded by mu.
	starting map[K]chan struct{}
}

// NewGroup returns an empty group whose instances run m with opts, such as
// shared hooks or a shared [LimitServices] semaphore.
func NewGroup[K comparable, S comparable, C any](m *Machine[S, C], opts ...RunOption) *Group[K, S, C] {
	return &Group[K, S, C]{m: m, opts: opts, mu: sync.Mutex{}, instances: map[K]*Instance[S, C]{}, starting: map[K]chan struct{}{}}
}

// Start returns the instance for key, creating it with context c when the
// group has none. It reports whether it created one, which it has not when
// it returns an error. Creating an instance carries out the effects of
// entering its initial states, using ctx for the actions among them.
//
// The group is not locked while the instance is created, so a slow initial
// action holds up only callers starting the same key, which wait for it and
// then share its instance. ctx bounds that wait.
func (g *Group[K, S, C]) Start(ctx context.Context, key K, c C) (*Instance[S, C], bool, error) {
	for {
		inst, pending, reserved := g.reserve(key)
		if inst != nil {
			return inst, false, nil
		}

		if !reserved {
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return nil, false, fmt.Errorf("start instance for %v: %w", key, context.Cause(ctx))
			}
		}

		inst, err := g.m.Instance(ctx, c, g.opts...)
		g.settle(key, inst)

		if err != nil {
			return nil, false, fmt.Errorf("start instance for %v: %w", key, err)
		}

		return inst, true, nil
	}
}

// Resume adds an instance for key that continues from snap, as after a
// restart, replacing none: it fails if the group already holds one or is
// starting one.
func (g *Group[K, S, C]) Resume(ctx context.Context, key K, snap Snapshot[S, C]) (*Instance[S, C], error) {
	if _, _, reserved := g.reserve(key); !reserved {
		return nil, fmt.Errorf("resume instance for %v: the group already holds one", key)
	}

	inst, err := g.m.InstanceFrom(ctx, snap, g.opts...)
	g.settle(key, inst)

	if err != nil {
		return nil, fmt.Errorf("resume instance for %v: %w", key, err)
	}

	return inst, nil
}

// reserve claims key for an instance about to be created. It returns the
// instance the group already holds, or the channel closed when the creation
// another caller has under way ends, or reports that the key is now the
// caller's to fill with settle.
func (g *Group[K, S, C]) reserve(key K) (*Instance[S, C], <-chan struct{}, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if inst, ok := g.instances[key]; ok {
		return inst, nil, false
	}

	if pending, ok := g.starting[key]; ok {
		return nil, pending, false
	}

	g.starting[key] = make(chan struct{})

	return nil, nil, true
}

// settle ends the reservation of key, keeping inst unless creating it
// failed, and wakes the callers waiting for it.
func (g *Group[K, S, C]) settle(key K, inst *Instance[S, C]) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if inst != nil {
		g.instances[key] = inst
	}

	close(g.starting[key])
	delete(g.starting, key)
}

// Get returns the instance for key.
func (g *Group[K, S, C]) Get(key K) (*Instance[S, C], bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	inst, ok := g.instances[key]

	return inst, ok
}

// Send sends ev to the instance for key, as [Instance.Send] does. It fails
// with [ErrNoInstance] when the group has none.
func (g *Group[K, S, C]) Send(ctx context.Context, key K, ev any) (Step[S, C], error) {
	inst, ok := g.Get(key)
	if !ok {
		return Step[S, C]{}, fmt.Errorf("send to %v: %w", key, ErrNoInstance)
	}

	return inst.Send(ctx, ev)
}

// Each yields every instance's key and current snapshot, in no particular
// order. Instances started or evicted while it runs may or may not be
// yielded.
func (g *Group[K, S, C]) Each() iter.Seq2[K, Snapshot[S, C]] {
	return func(yield func(K, Snapshot[S, C]) bool) {
		for _, key := range g.Keys() {
			inst, ok := g.Get(key)
			if !ok {
				continue
			}

			if !yield(key, inst.Snapshot()) {
				return
			}
		}
	}
}

// Keys returns the keys the group holds instances for, in no particular
// order.
func (g *Group[K, S, C]) Keys() []K {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Collect(maps.Keys(g.instances))
}

// Count returns how many instances' current snapshots satisfy cond.
func (g *Group[K, S, C]) Count(cond func(Snapshot[S, C]) bool) int {
	n := 0
	for _, snap := range g.Each() {
		if cond(snap) {
			n++
		}
	}

	return n
}

// Remove closes the instance for key and forgets it, returning its final
// snapshot so the caller can persist it. It reports false when the group
// holds none.
func (g *Group[K, S, C]) Remove(key K) (Snapshot[S, C], bool) {
	g.mu.Lock()
	inst, ok := g.instances[key]
	delete(g.instances, key)
	g.mu.Unlock()

	if !ok {
		var none Snapshot[S, C]
		return none, false
	}

	inst.Close()

	return inst.Snapshot(), true
}

// Evict closes and forgets every instance whose current snapshot satisfies
// cond, such as those that are done or idle, and returns their keys. An
// instance that moves on, or whose key is given to another instance, between
// the check and the removal is left alone.
func (g *Group[K, S, C]) Evict(cond func(Snapshot[S, C]) bool) []K {
	var evicted []K
	for _, key := range g.Keys() {
		inst, ok := g.Get(key)
		if !ok {
			continue
		}

		snap := inst.Snapshot()
		if !cond(snap) {
			continue
		}

		// cond runs outside the lock, so the match is confirmed under it:
		// the key still names this instance, and the instance is still at
		// the snapshot cond saw.
		g.mu.Lock()
		same := g.instances[key] == inst && inst.Snapshot().seq == snap.seq
		if same {
			delete(g.instances, key)
		}
		g.mu.Unlock()

		if same {
			inst.Close()
			evicted = append(evicted, key)
		}
	}

	return evicted
}

// Close closes every instance the group holds when it is called. It does
// not stop the group being used: an instance a concurrent Start or Resume
// adds stays in the group, so stop starting instances before closing it.
func (g *Group[K, S, C]) Close() {
	for _, key := range g.Keys() {
		g.Remove(key)
	}
}

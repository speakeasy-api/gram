package sc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// ErrResourceUnavailable is the failure of a service that could not run
// because a resource it runs inside could not be acquired again after a
// restart.
var ErrResourceUnavailable = errors.New("a resource the service runs inside is not held")

// resourceTable holds the values of the resources one driver's instance has
// acquired. It is safe for concurrent use.
type resourceTable[S comparable, C any] struct {
	m     *Machine[S, C]
	hooks *Hooks[S, C]

	mu sync.Mutex

	// slots holds every acquisition that has begun and not been released,
	// by its effect's ID.
	slots map[string]*resourceSlot[C]

	// held lists the slots whose values are held, in acquisition order.
	held []*resourceSlot[C]

	// closed is set once the driver is shutting down.
	closed bool
}

// resourceSlot is one acquisition.
type resourceSlot[C any] struct {
	// acquire is the effect that began the acquisition.
	acquire Effect[C]

	// key identifies the resource to its handle.
	key string

	// settled is closed once the acquisition has ended either way.
	settled chan struct{}

	// value is the acquired value; ok is set while it is held.
	value any
	ok    bool

	// abandoned is set when a release arrived before the value did;
	// release and closed say how to release the value when it arrives.
	abandoned bool
	release   Effect[C]
	closed    bool
}

func newResourceTable[S comparable, C any](m *Machine[S, C], hooks *Hooks[S, C]) *resourceTable[S, C] {
	return &resourceTable[S, C]{m: m, hooks: hooks, mu: sync.Mutex{}, slots: map[string]*resourceSlot[C]{}, held: nil, closed: false}
}

// attach returns ctx carrying the table, for Resource.From.
func (t *resourceTable[S, C]) attach(ctx context.Context) context.Context {
	return context.WithValue(ctx, heldResources{}, t.lookup)
}

// lookup returns the held value of the resource with the given key.
func (t *resourceTable[S, C]) lookup(key string) (any, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, slot := range slices.Backward(t.held) {
		if slot.key == key {
			return slot.value, true
		}
	}

	return nil, false
}

// begin records that the acquisition e asks for has started.
func (t *resourceTable[S, C]) begin(e Effect[C]) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var none Effect[C]
	t.slots[e.ID] = &resourceSlot[C]{
		acquire:   e,
		key:       e.State + "\x00" + e.Name,
		settled:   make(chan struct{}),
		value:     nil,
		ok:        false,
		abandoned: t.closed,
		release:   none,
		closed:    t.closed,
	}
}

// store keeps the value of an acquisition that succeeded. It reports false
// when the value was released at once because its state had been left, or
// the driver closed, while it was being acquired; the acquisition's event
// must then not reach the chart.
func (t *resourceTable[S, C]) store(ctx context.Context, id string, value any) bool {
	t.mu.Lock()
	slot, ok := t.slots[id]
	if !ok {
		t.mu.Unlock()
		return false
	}

	if slot.abandoned {
		delete(t.slots, id)
		t.mu.Unlock()
		t.run(ctx, slot, value)
		close(slot.settled)

		return false
	}

	slot.value, slot.ok = value, true
	t.held = append(t.held, slot)
	close(slot.settled)
	t.mu.Unlock()

	return true
}

// settle marks an acquisition that ended without a value.
func (t *resourceTable[S, C]) settle(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	slot, ok := t.slots[id]
	if !ok || slot.ok {
		return
	}

	delete(t.slots, id)
	select {
	case <-slot.settled:
	default:
		close(slot.settled)
	}
}

// releaseFor carries out the release effect e. A value still being acquired
// is released when it arrives.
func (t *resourceTable[S, C]) releaseFor(ctx context.Context, e Effect[C]) {
	t.mu.Lock()
	slot, ok := t.slots[e.ID]
	if !ok {
		t.mu.Unlock()
		return
	}

	if !slot.ok {
		slot.abandoned, slot.release = true, e
		t.mu.Unlock()

		return
	}

	delete(t.slots, e.ID)
	t.held = slices.DeleteFunc(t.held, func(s *resourceSlot[C]) bool { return s == slot })
	slot.release = e
	t.mu.Unlock()

	t.run(ctx, slot, slot.value)
}

// abandon marks the table closed, so every acquisition still in flight
// releases its value when it arrives.
func (t *resourceTable[S, C]) abandon() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closed = true
	for _, slot := range t.slots {
		if !slot.ok && !slot.abandoned {
			slot.abandoned, slot.closed = true, true
		}
	}
}

// releaseAll releases every held value, the last acquired first, because
// the driver is closing.
func (t *resourceTable[S, C]) releaseAll(ctx context.Context) {
	t.mu.Lock()
	held := t.held
	t.held = nil
	for _, slot := range held {
		delete(t.slots, slot.acquire.ID)
		slot.closed = true
	}
	t.mu.Unlock()

	for _, slot := range slices.Backward(held) {
		t.run(ctx, slot, slot.value)
	}
}

// wait blocks until every acquisition in ids has ended, and fails when one
// of them holds no value.
func (t *resourceTable[S, C]) wait(ctx context.Context, ids []string) error {
	for _, id := range ids {
		t.mu.Lock()
		slot, ok := t.slots[id]
		t.mu.Unlock()
		if !ok {
			return ErrResourceUnavailable
		}

		select {
		case <-slot.settled:
		case <-ctx.Done():
			return fmt.Errorf("wait for resource: %w", context.Cause(ctx))
		}

		t.mu.Lock()
		held := slot.ok
		t.mu.Unlock()
		if !held {
			return ErrResourceUnavailable
		}
	}

	return nil
}

// run calls the chart's release function for slot and reports it to the
// hooks.
func (t *resourceTable[S, C]) run(ctx context.Context, slot *resourceSlot[C], value any) {
	e := slot.release
	if slot.closed {
		e = slot.acquire
	}
	e.Kind = EffectRelease

	ctx = context.WithoutCancel(ctx)
	if t.hooks != nil && t.hooks.EffectStarted != nil {
		if started := t.hooks.EffectStarted(ctx, e); started != nil {
			ctx = started
		}
	}

	begin := time.Now()
	err := t.m.release(ctx, e, value, slot.closed)

	if t.hooks != nil && t.hooks.EffectFinished != nil {
		t.hooks.EffectFinished(ctx, e, time.Since(begin), err)
	}
}

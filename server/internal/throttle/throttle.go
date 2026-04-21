package throttle

import (
	"sync"
	"time"
)

// entry tracks per-key throttle state.
type entry[V any] struct {
	pending bool
	latest  V
	timer   *time.Timer
}

// Throttle provides per-key leading-edge throttling with a trailing fire.
// The first call for a key executes immediately (Do returns true). Subsequent
// calls within the cooldown window are suppressed. When the window expires,
// if any calls were suppressed, the onTrailing callback fires once with the
// most recent value.
type Throttle[K comparable, V any] struct {
	Cooldown   time.Duration
	keyFn      func(V) K
	onTrailing func(V) error

	mu      sync.Mutex
	entries map[K]*entry[V]
}

// New creates a throttle. keyFn extracts the throttle key from the value.
// onTrailing is called when the cooldown expires and calls were suppressed
// during the window.
func New[K comparable, V any](cooldown time.Duration, keyFn func(V) K, onTrailing func(V) error) *Throttle[K, V] {
	return &Throttle[K, V]{
		Cooldown:   cooldown,
		keyFn:      keyFn,
		onTrailing: onTrailing,
		mu:         sync.Mutex{},
		entries:    make(map[K]*entry[V]),
	}
}

// Do reports whether the caller should execute. Returns true on the leading
// edge (first call for this key, or first call after cooldown expires).
// Returns false when the call is suppressed (inside cooldown window).
func (t *Throttle[K, V]) Do(v V) bool {
	key := t.keyFn(v)

	t.mu.Lock()
	defer t.mu.Unlock()

	e, ok := t.entries[key]
	if !ok {
		var zero V
		e = &entry[V]{pending: false, latest: zero, timer: nil}
		t.entries[key] = e
	}

	if e.timer == nil {
		e.pending = false
		e.timer = time.AfterFunc(t.Cooldown, func() {
			t.onExpired(key)
		})
		return true
	}

	e.pending = true
	e.latest = v
	return false
}

func (t *Throttle[K, V]) onExpired(key K) {
	t.mu.Lock()
	e, ok := t.entries[key]
	if !ok {
		t.mu.Unlock()
		return
	}

	if e.pending {
		latest := e.latest
		e.pending = false
		e.timer.Reset(t.Cooldown)
		t.mu.Unlock()
		_ = t.onTrailing(latest)
	} else {
		e.timer = nil
		delete(t.entries, key)
		t.mu.Unlock()
	}
}

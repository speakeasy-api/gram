package throttle

import (
	"sync"
	"time"
)

// entry tracks per-key throttle state.
type entry struct {
	mu      sync.Mutex
	pending bool
	timer   *time.Timer
}

// Throttle provides per-key leading-edge throttling with a trailing fire.
// The first call for a key executes immediately (Do returns true). Subsequent
// calls within the cooldown window are suppressed. When the window expires,
// if any calls were suppressed, the onTrailing callback fires once.
type Throttle[K comparable, V any] struct {
	Cooldown   time.Duration
	keyFn      func(V) K
	onTrailing func(V) error
	entries    sync.Map // K → *entry
}

// New creates a throttle. keyFn extracts the throttle key from the value.
// onTrailing is called (in a goroutine) when the cooldown expires and calls
// were suppressed during the window.
func New[K comparable, V any](cooldown time.Duration, keyFn func(V) K, onTrailing func(V) error) *Throttle[K, V] {
	return &Throttle[K, V]{
		Cooldown:   cooldown,
		keyFn:      keyFn,
		onTrailing: onTrailing,
	}
}

// Do reports whether the caller should execute. Returns true on the leading
// edge (first call for this key, or first call after cooldown expires).
// Returns false when the call is suppressed (inside cooldown window).
func (t *Throttle[K, V]) Do(v V) bool {
	key := t.keyFn(v)

	val, _ := t.entries.LoadOrStore(key, &entry{})
	e := val.(*entry)

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.timer == nil {
		e.pending = false
		e.timer = time.AfterFunc(t.Cooldown, func() {
			t.onExpired(key, v)
		})
		return true
	}

	e.pending = true
	return false
}

func (t *Throttle[K, V]) onExpired(key K, v V) {
	val, ok := t.entries.Load(key)
	if !ok {
		return
	}
	e := val.(*entry)

	e.mu.Lock()
	pending := e.pending
	e.pending = false
	if pending {
		e.timer.Reset(t.Cooldown)
	} else {
		e.timer = nil
	}
	e.mu.Unlock()

	if pending {
		_ = t.onTrailing(v)
	}
}

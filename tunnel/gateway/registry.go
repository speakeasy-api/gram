package gateway

import (
	"sync"
	"sync/atomic"

	"github.com/hashicorp/yamux"
)

// registry is the per-pod, in-memory map of tunnelID -> live yamux sessions.
// Multiple agents may present the same tunnel key (customer-side HA); the
// gateway accepts all and round-robins substreams across them, so duplicate
// registration is defined behavior, not a conflict.
type registry struct {
	mu       sync.RWMutex
	sessions map[string][]*sessEntry
	rr       map[string]*uint64 // round-robin cursor per tunnel
}

type sessEntry struct {
	id      string
	session *yamux.Session
}

func newRegistry() *registry {
	return &registry{
		sessions: make(map[string][]*sessEntry),
		rr:       make(map[string]*uint64),
	}
}

// add registers a session and returns a remove func to call on disconnect.
func (r *registry) add(tunnelID, sessionID string, s *yamux.Session) func() {
	entry := &sessEntry{id: sessionID, session: s}
	r.mu.Lock()
	r.sessions[tunnelID] = append(r.sessions[tunnelID], entry)
	if _, ok := r.rr[tunnelID]; !ok {
		var c uint64
		r.rr[tunnelID] = &c
	}
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		list := r.sessions[tunnelID]
		for i, e := range list {
			if e == entry {
				r.sessions[tunnelID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(r.sessions[tunnelID]) == 0 {
			delete(r.sessions, tunnelID)
			delete(r.rr, tunnelID)
		}
	}
}

// pick returns one live session for the tunnel, round-robining across agents
// and skipping sessions that have already gone away.
func (r *registry) pick(tunnelID string) (*yamux.Session, bool) {
	r.mu.RLock()
	list := r.sessions[tunnelID]
	cursor := r.rr[tunnelID]
	r.mu.RUnlock()
	if len(list) == 0 {
		return nil, false
	}
	start := int(atomic.AddUint64(cursor, 1))
	for i := range list {
		s := list[(start+i)%len(list)].session
		if !s.IsClosed() {
			return s, true
		}
	}
	return nil, false
}

// kill closes every session for a tunnel (revocation). Returns count killed.
func (r *registry) kill(tunnelID string) int {
	r.mu.Lock()
	list := r.sessions[tunnelID]
	delete(r.sessions, tunnelID)
	delete(r.rr, tunnelID)
	r.mu.Unlock()
	for _, e := range list {
		_ = e.session.Close()
	}
	return len(list)
}

// activeSessions returns the total number of registered sessions (for metrics).
func (r *registry) activeSessions() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, list := range r.sessions {
		n += len(list)
	}
	return n
}

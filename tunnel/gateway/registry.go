package gateway

import (
	"sync"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/speakeasy-api/gram/tunnel/route"
)

const consumerSessionTTL = 5 * time.Minute

// registry is the per-pod, in-memory map of tunnelID -> live yamux sessions.
// Multiple agents may present the same tunnel key (customer-side HA); the
// gateway accepts all and round-robins substreams across them, so duplicate
// registration is defined behavior, not a conflict.
type registry struct {
	mu       sync.RWMutex
	sessions map[string][]*sessEntry
	rr       map[string]uint64 // round-robin cursor per tunnel
}

type sessEntry struct {
	id               string
	session          *yamux.Session
	connection       route.Connection
	activeSubstreams int
	consumerSessions map[string]time.Time
}

func newRegistry() *registry {
	return &registry{
		sessions: make(map[string][]*sessEntry),
		rr:       make(map[string]uint64),
	}
}

// add registers a session and returns a remove func to call on disconnect.
func (r *registry) add(tunnelID, sessionID string, s *yamux.Session, connection route.Connection) func() {
	entry := &sessEntry{
		id:               sessionID,
		session:          s,
		connection:       connection,
		activeSubstreams: 0,
		consumerSessions: make(map[string]time.Time),
	}
	r.mu.Lock()
	r.sessions[tunnelID] = append(r.sessions[tunnelID], entry)
	if _, ok := r.rr[tunnelID]; !ok {
		r.rr[tunnelID] = 0
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

func (r *registry) tunnelSessionCount(tunnelID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions[tunnelID])
}

func (r *registry) connections(tunnelID string, heartbeatAt time.Time) []route.Connection {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]route.Connection, 0, len(r.sessions[tunnelID]))
	for _, entry := range r.sessions[tunnelID] {
		if entry.session.IsClosed() {
			continue
		}
		connection := entry.connection
		connection.LastHeartbeatAt = heartbeatAt
		connection.ActiveSubstreams = entry.activeSubstreams
		connection.ActiveConsumerSessions = pruneConsumerSessions(entry, heartbeatAt)
		result = append(result, connection)
	}
	return result
}

// beginForward returns one live session for the tunnel, round-robining across
// agents and accounting for the forwarded request in the management snapshot.
func (r *registry) beginForward(tunnelID, consumerSession string, now time.Time) (*sessEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	list := r.sessions[tunnelID]
	if len(list) == 0 {
		return nil, false
	}
	start := int(r.rr[tunnelID] % uint64(len(list)))
	r.rr[tunnelID]++
	for i := range list {
		entry := list[(start+i)%len(list)]
		if entry.session.IsClosed() {
			continue
		}
		entry.activeSubstreams++
		if consumerSession != "" {
			if entry.consumerSessions == nil {
				entry.consumerSessions = make(map[string]time.Time)
			}
			entry.consumerSessions[consumerSession] = now.Add(consumerSessionTTL)
		}
		entry.connection.ActiveSubstreams = entry.activeSubstreams
		entry.connection.ActiveConsumerSessions = pruneConsumerSessions(entry, now)
		return entry, true
	}
	return nil, false
}

func (r *registry) finishForward(entry *sessEntry, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry.activeSubstreams > 0 {
		entry.activeSubstreams--
	}
	entry.connection.ActiveSubstreams = entry.activeSubstreams
	entry.connection.ActiveConsumerSessions = pruneConsumerSessions(entry, now)
}

func pruneConsumerSessions(entry *sessEntry, now time.Time) int {
	for consumerSession, expiresAt := range entry.consumerSessions {
		if now.After(expiresAt) {
			delete(entry.consumerSessions, consumerSession)
		}
	}
	return len(entry.consumerSessions)
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

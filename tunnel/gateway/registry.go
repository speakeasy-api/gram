package gateway

import (
	"net/http"
	"sync"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

// consumerSessionTTL controls idle consumer-session accounting, not yamux session
// lifetime. Expiry drops local affinity/counting state for consumers that have not
// sent a request within this window; their next request re-pins via rendezvous.
const consumerSessionTTL = 5 * time.Minute

// Multiple agents may share a tunnel key. Stable consumer keys stick to one live session;
// requests without a consumer key round-robin across live sessions.
type registry struct {
	mu       sync.RWMutex
	sessions map[string][]*sessEntry
	rr       map[string]uint64 // round-robin cursor per tunnel
}

type sessEntry struct {
	id      string
	session *yamux.Session
	// proxy is the session-scoped reverse proxy reused across forwards; its
	// transport dials a fresh yamux substream per request, so sharing the
	// instance changes no semantics and avoids a per-forward allocation.
	proxy            http.Handler
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

func (r *registry) add(tunnelID, sessionID string, s *yamux.Session, proxy http.Handler, connection route.Connection) func() {
	entry := &sessEntry{
		id:               sessionID,
		session:          s,
		proxy:            proxy,
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

// forwardFailure distinguishes why beginForward could not reserve a session:
// the tunnel has no live sessions at all (stale route — safe to unpublish) vs
// every live session is at its substream cap (healthy but busy — the route
// must stay published).
type forwardFailure int

const (
	forwardReserved forwardFailure = iota
	forwardNoSession
	forwardBusy
)

// beginForward reserves one live session and updates snapshot counters.
//
// exactSessionID, when non-empty, restricts the reservation to that one agent
// session: session-bearing anonymous MCP traffic must land on the exact agent
// whose backend minted the MCP session, so a missing or closed match is
// forwardNoSession (never a spill to a sibling agent) and a match at its
// substream cap is forwardBusy.
func (r *registry) beginForward(tunnelID, consumerSession, exactSessionID string, now time.Time, maxStreamsPerSession int) (*sessEntry, forwardFailure) {
	r.mu.Lock()
	defer r.mu.Unlock()

	list := r.sessions[tunnelID]
	if len(list) == 0 {
		return nil, forwardNoSession
	}
	sawBusy := false
	reserve := func(entry *sessEntry) (*sessEntry, bool) {
		if entry.session.IsClosed() {
			return nil, false
		}
		if maxStreamsPerSession > 0 && entry.activeSubstreams >= maxStreamsPerSession {
			sawBusy = true
			return nil, false
		}
		entry.activeSubstreams++
		if consumerSession != "" {
			if entry.consumerSessions == nil {
				entry.consumerSessions = make(map[string]time.Time)
			}
			entry.consumerSessions[consumerSession] = now.Add(consumerSessionTTL)
		}
		return entry, true
	}
	failure := func() forwardFailure {
		if sawBusy {
			return forwardBusy
		}
		return forwardNoSession
	}

	if exactSessionID != "" {
		for _, entry := range list {
			if entry.id != exactSessionID {
				continue
			}
			if reserved, ok := reserve(entry); ok {
				return reserved, forwardReserved
			}
			return nil, failure()
		}
		return nil, forwardNoSession
	}

	if consumerSession != "" {
		entryByID := make(map[string]*sessEntry, len(list))
		candidates := make([]string, 0, len(list))
		for _, entry := range list {
			entryByID[entry.id] = entry
			candidates = append(candidates, entry.id)
		}
		for _, sessionID := range wire.RendezvousOrder(consumerSession, candidates) {
			if entry, ok := reserve(entryByID[sessionID]); ok {
				return entry, forwardReserved
			}
		}
		return nil, failure()
	}

	start := int(r.rr[tunnelID] % uint64(len(list)))
	r.rr[tunnelID]++
	for i := range list {
		entry := list[(start+i)%len(list)]
		if reserved, ok := reserve(entry); ok {
			return reserved, forwardReserved
		}
	}
	return nil, failure()
}

// finishForward releases the substream slot. Snapshot counters are derived
// from the live fields inside connections(); mirroring them onto
// entry.connection here would be dead stores.
func (r *registry) finishForward(entry *sessEntry, _ time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry.activeSubstreams > 0 {
		entry.activeSubstreams--
	}
}

func pruneConsumerSessions(entry *sessEntry, now time.Time) int {
	for consumerSession, expiresAt := range entry.consumerSessions {
		if now.After(expiresAt) {
			delete(entry.consumerSessions, consumerSession)
		}
	}
	return len(entry.consumerSessions)
}

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

func (r *registry) activeSessions() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, list := range r.sessions {
		n += len(list)
	}
	return n
}

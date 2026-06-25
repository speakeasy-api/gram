// Package route is the tunnelID -> owning-gateway-pod address cache that lets a
// separate gram-server pod find the gateway holding a given tunnel's session.
// Per the design this is a TTL-heartbeated cache (Postgres is the durable truth
// in prod; this POC skips the DB and uses only Redis or an in-memory map).
package route

import (
	"context"
	"sync"
	"time"
)

// Store maps a tunnel ID to the internal address (host:port) of the gateway pod
// currently holding its session.
type Store interface {
	// Publish (re)writes the route with a TTL; gateways refresh on a heartbeat.
	Publish(ctx context.Context, tunnelID, addr string, ttl time.Duration) error
	// Lookup returns the gateway address for a tunnel, or ("", false) if absent.
	Lookup(ctx context.Context, tunnelID string) (string, bool, error)
	// Delete removes the route (on disconnect/drain).
	Delete(ctx context.Context, tunnelID string) error
}

// Connection is the live tunnel session shape cached for the management API.
// It intentionally mirrors server/internal/mv.TunnelledMcpConnectionCache
// without importing server packages into the standalone tunnel binaries.
type Connection struct {
	SessionID              string            `json:"session_id"`
	ServiceID              string            `json:"service_id"`
	ServiceSlug            string            `json:"service_slug"`
	ServiceVersion         string            `json:"service_version"`
	AgentVersion           string            `json:"agent_version"`
	ConnectedAt            time.Time         `json:"connected_at"`
	LastHeartbeatAt        time.Time         `json:"last_heartbeat_at"`
	RemoteAddr             string            `json:"remote_addr"`
	ActiveSubstreams       int               `json:"active_substreams"`
	ActiveConsumerSessions int               `json:"active_consumer_sessions"`
	Metadata               map[string]string `json:"metadata"`
}

// ConnectionSnapshotStore is implemented by stores that can publish the live
// connection cache read by /rpc/tunnelledMcp.*.
type ConnectionSnapshotStore interface {
	PublishConnections(ctx context.Context, tunnelID string, connections []Connection, ttl time.Duration) error
	DeleteConnections(ctx context.Context, tunnelID string) error
}

// Memory is a process-local Store. Sufficient when the gateway and the serve
// path share a process, or for single-replica local runs.
type Memory struct {
	mu     sync.RWMutex
	routes map[string]memEntry
}

type memEntry struct {
	addr      string
	expiresAt time.Time
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{routes: make(map[string]memEntry)} }

func (m *Memory) Publish(_ context.Context, tunnelID, addr string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes[tunnelID] = memEntry{addr: addr, expiresAt: time.Now().Add(ttl)}
	return nil
}

func (m *Memory) Lookup(_ context.Context, tunnelID string) (string, bool, error) {
	m.mu.RLock()
	e, ok := m.routes[tunnelID]
	m.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return "", false, nil
	}
	return e.addr, true, nil
}

func (m *Memory) Delete(_ context.Context, tunnelID string) error {
	m.mu.Lock()
	delete(m.routes, tunnelID)
	m.mu.Unlock()
	return nil
}

var _ Store = (*Memory)(nil)

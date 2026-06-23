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

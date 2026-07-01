// Package route stores live tunnelID -> gateway address mappings and connection snapshots.
package route

import (
	"context"
	"sync"
	"time"
)

const (
	tunnelRoutesKeyPrefix      = "tunnel_routes:"
	tunnelConnectionsKeyPrefix = "tunnel_connections:"
)

// RouteKey returns the Redis key for a tunnel's live gateway route.
func RouteKey(tunnelID string) string {
	return tunnelRoutesKeyPrefix + tunnelID
}

// ConnectionKey returns the Redis key for a tunnel's live connection snapshot.
func ConnectionKey(tunnelID string) string {
	return tunnelConnectionsKeyPrefix + tunnelID
}

// Store maps tunnel IDs to the gateway pod currently holding each session.
type Store interface {
	Publish(ctx context.Context, tunnelID, addr string, ttl time.Duration) error
	Lookup(ctx context.Context, tunnelID string) (string, bool, error)
	Delete(ctx context.Context, tunnelID string) error
}

// Connection mirrors the management cache shape without importing server packages.
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

type ConnectionSnapshotStore interface {
	PublishConnections(ctx context.Context, tunnelID string, connections []Connection, ttl time.Duration) error
	DeleteConnections(ctx context.Context, tunnelID string) error
}

// RouteTable is an in-memory Store implementation for local development and tests.
type RouteTable struct {
	mu     sync.RWMutex
	routes map[string]memEntry
}

type memEntry struct {
	addr      string
	expiresAt time.Time
}

// NewRouteTable creates an empty in-memory route table.
func NewRouteTable() *RouteTable { return &RouteTable{routes: make(map[string]memEntry)} }

func (m *RouteTable) Publish(_ context.Context, tunnelID, addr string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes[tunnelID] = memEntry{addr: addr, expiresAt: time.Now().Add(ttl)}
	return nil
}

func (m *RouteTable) Lookup(_ context.Context, tunnelID string) (string, bool, error) {
	m.mu.RLock()
	e, ok := m.routes[tunnelID]
	m.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return "", false, nil
	}
	return e.addr, true, nil
}

func (m *RouteTable) Delete(_ context.Context, tunnelID string) error {
	m.mu.Lock()
	delete(m.routes, tunnelID)
	m.mu.Unlock()
	return nil
}

var _ Store = (*RouteTable)(nil)

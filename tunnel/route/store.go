// Package route stores live tunnel gateway owners and connection snapshots.
package route

import (
	"context"
	"sort"
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

// Store tracks live gateway owners for each tunnel.
type Store interface {
	Publish(ctx context.Context, tunnelID, addr string, ttl time.Duration) error
	Candidates(ctx context.Context, tunnelID string) ([]string, error)
	Unpublish(ctx context.Context, tunnelID, addr string) error
	Delete(ctx context.Context, tunnelID string) error
}

// Connection mirrors the management cache shape without importing server packages.
type Connection struct {
	GatewaySessionID       string            `json:"gateway_session_id"`
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
	PublishConnections(ctx context.Context, tunnelID, owner string, connections []Connection, ttl time.Duration) error
	Connections(ctx context.Context, tunnelID string) ([]Connection, error)
	DeleteConnectionOwner(ctx context.Context, tunnelID, owner string) error
	DeleteConnections(ctx context.Context, tunnelID string) error
}

type RuntimeStore interface {
	Store
	ConnectionSnapshotStore
}

// RouteTable is an in-memory Store implementation for local development and tests.
type RouteTable struct {
	mu     sync.RWMutex
	routes map[string]map[string]time.Time
}

// NewRouteTable creates an empty in-memory route table.
func NewRouteTable() *RouteTable { return &RouteTable{routes: make(map[string]map[string]time.Time)} }

func (m *RouteTable) Publish(_ context.Context, tunnelID, addr string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.routes[tunnelID] == nil {
		m.routes[tunnelID] = make(map[string]time.Time)
	}
	m.routes[tunnelID][addr] = time.Now().Add(ttl)
	return nil
}

func (m *RouteTable) Candidates(_ context.Context, tunnelID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	owners := m.routes[tunnelID]
	if len(owners) == 0 {
		return nil, nil
	}
	candidates := make([]string, 0, len(owners))
	for addr, expiresAt := range owners {
		if now.After(expiresAt) {
			delete(owners, addr)
			continue
		}
		candidates = append(candidates, addr)
	}
	if len(owners) == 0 {
		delete(m.routes, tunnelID)
	}
	sort.Strings(candidates)
	return candidates, nil
}

func (m *RouteTable) Unpublish(_ context.Context, tunnelID, addr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.routes[tunnelID], addr)
	if len(m.routes[tunnelID]) == 0 {
		delete(m.routes, tunnelID)
	}
	return nil
}

func (m *RouteTable) Delete(_ context.Context, tunnelID string) error {
	m.mu.Lock()
	delete(m.routes, tunnelID)
	m.mu.Unlock()
	return nil
}

var _ Store = (*RouteTable)(nil)

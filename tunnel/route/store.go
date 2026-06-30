// Package route stores live tunnelID -> gateway address mappings and connection snapshots.
package route

import (
	"context"
	"sync"
	"time"
)

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

type Memory struct {
	mu     sync.RWMutex
	routes map[string]memEntry
}

type memEntry struct {
	addr      string
	expiresAt time.Time
}

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

package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

const (
	mcpSessionPrefix = "mcp-session:"
	mcpSessionTTL    = 30 * time.Minute
)

// mcpSessionStore tracks initialized MCP sessions using the cache layer.
// Sessions are created on successful initialize requests and validated on
// subsequent requests to ensure the client is using a known session.
type mcpSessionStore struct {
	cache cache.Cache
}

func newMCPSessionStore(c cache.Cache) *mcpSessionStore {
	return &mcpSessionStore{cache: c}
}

func (s *mcpSessionStore) key(sessionID string) string {
	return mcpSessionPrefix + sessionID
}

// Create stores a new session ID with a 30-minute TTL.
func (s *mcpSessionStore) Create(ctx context.Context, sessionID string) error {
	if err := s.cache.Set(ctx, s.key(sessionID), true, mcpSessionTTL); err != nil {
		return fmt.Errorf("create mcp session: %w", err)
	}
	return nil
}

// Validate checks whether the session ID exists in the store.
func (s *mcpSessionStore) Validate(ctx context.Context, sessionID string) bool {
	var exists bool
	if err := s.cache.Get(ctx, s.key(sessionID), &exists); err != nil {
		return false
	}
	return exists
}

// Delete removes a session ID from the store.
func (s *mcpSessionStore) Delete(ctx context.Context, sessionID string) error {
	if err := s.cache.Delete(ctx, s.key(sessionID)); err != nil {
		return fmt.Errorf("delete mcp session: %w", err)
	}
	return nil
}

// Touch refreshes the TTL on an existing session.
func (s *mcpSessionStore) Touch(ctx context.Context, sessionID string) error {
	if err := s.cache.Set(ctx, s.key(sessionID), true, mcpSessionTTL); err != nil {
		return fmt.Errorf("touch mcp session: %w", err)
	}
	return nil
}

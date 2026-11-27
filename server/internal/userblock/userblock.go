// Package userblock reads whether staff blocked a user from the platform
// (users.blacklisted_at). Every authentication path that resolves a user, or a
// token a user authorized, checks it, so a blocked user cannot use the
// dashboard, API keys, MCP servers, the Platform MCP, assistants, or any other
// surface, and cannot create anything.
package userblock

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/users/repo"
)

// cacheTTL bounds how long a request can still pass after staff block a user.
const cacheTTL = 10 * time.Second

// cacheSize bounds how many users' states one server keeps.
const cacheSize = 16384

// Gate reads users' block state, caching it briefly.
type Gate struct {
	queries *repo.Queries
	cache   *expirable.LRU[string, bool]
}

// NewGate returns a gate reading db, or nil when db is nil. A nil gate blocks
// no one, for wiring and tests that run without a database.
func NewGate(db *pgxpool.Pool) *Gate {
	if db == nil {
		return nil
	}
	return &Gate{queries: repo.New(db), cache: expirable.NewLRU[string, bool](cacheSize, nil, cacheTTL)}
}

// Blocked reports whether userID is blocked. An empty user ID is not blocked.
func (g *Gate) Blocked(ctx context.Context, userID string) (bool, error) {
	if g == nil || userID == "" {
		return false, nil
	}
	if blocked, ok := g.cache.Get(userID); ok {
		return blocked, nil
	}
	blocked, err := g.queries.IsUserBlacklisted(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("read user block state: %w", err)
	}
	g.cache.Add(userID, blocked)
	return blocked, nil
}

// Check returns a forbidden error when any of userIDs is blocked, and fails
// closed with an unexpected error when the state cannot be read. Pass every
// user behind a credential: the caller and, for delegated tokens, the user who
// authorized or owns it.
func (g *Gate) Check(ctx context.Context, userIDs ...string) error {
	for _, userID := range userIDs {
		blocked, err := g.Blocked(ctx, userID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "error checking user access")
		}
		if blocked {
			return oops.E(oops.CodeForbidden, nil, "this account is disabled, please reach out to support@speakeasy.com for more information")
		}
	}
	return nil
}

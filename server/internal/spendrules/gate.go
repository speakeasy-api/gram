package spendrules

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// EvaluationInterval is how often the background evaluator recomputes spend
// against every enabled rule and rewrites circuit state.
const EvaluationInterval = 5 * time.Minute

// circuitTTL bounds how long circuit state survives without the evaluator
// rewriting it: roughly two evaluation cycles, so a stalled evaluator fails
// open instead of blocking users on stale data forever.
const circuitTTL = 2 * EvaluationInterval

// Block describes why an actor is currently blocked by a spend rule.
type Block struct {
	RuleURN  string `json:"rule_urn"`
	RuleName string `json:"rule_name"`
	// WindowEnd is when the budget window resets and the block lifts.
	WindowEnd time.Time `json:"window_end"`
}

// BlockSet is the full circuit state for one organization: every identifier
// (Gram user id, normalized email) of every actor currently blocked by a
// spend rule with action=block. The evaluator replaces the set wholesale
// each cycle, so circuits close as soon as a cycle no longer blocks an actor.
type BlockSet map[string]Block

func circuitKey(organizationID string) string {
	return "spend_block:" + organizationID
}

// Gate is the hot-path circuit check consulted by the Claude hooks handlers
// before risk-policy scans. Reads are a single cache GET; every failure mode
// resolves to "not blocked" (fail-open) so a cache outage never denies
// traffic.
type Gate struct {
	logger *slog.Logger
	cache  cache.Cache
}

func NewGate(logger *slog.Logger, cacheImpl cache.Cache) *Gate {
	return &Gate{
		logger: logger.With(attr.SlogComponent("spendrules_gate")),
		cache:  cacheImpl,
	}
}

// CheckBlocked reports whether the given actor is currently blocked by a
// spend rule. Either identifier may be empty. A nil Block means the actor is
// not blocked. Errors are cache infrastructure failures — callers should
// treat them as "not blocked" (fail-open); they are returned for logging.
func (g *Gate) CheckBlocked(ctx context.Context, organizationID, userID, email string) (*Block, error) {
	if organizationID == "" || (userID == "" && email == "") {
		return nil, nil
	}

	var blocks BlockSet
	err := g.cache.Get(ctx, circuitKey(organizationID), &blocks)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read spend block set: %w", err)
	}

	if userID != "" {
		if block, ok := blocks[userID]; ok {
			return &block, nil
		}
	}
	if email != "" {
		if block, ok := blocks[conv.NormalizeEmail(email)]; ok {
			return &block, nil
		}
	}
	return nil, nil
}

// WriteBlockSet replaces the organization's circuit state. An empty set
// deletes the key so the gate's common case stays a cheap miss.
func WriteBlockSet(ctx context.Context, cacheImpl cache.Cache, organizationID string, blocks BlockSet) error {
	key := circuitKey(organizationID)
	if len(blocks) == 0 {
		if err := cacheImpl.Delete(ctx, key); err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
			return fmt.Errorf("clear spend block set: %w", err)
		}
		return nil
	}
	if err := cacheImpl.Set(ctx, key, blocks, circuitTTL); err != nil {
		return fmt.Errorf("write spend block set: %w", err)
	}
	return nil
}

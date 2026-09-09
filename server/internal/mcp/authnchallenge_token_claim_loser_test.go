package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	agents_repo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The winner has already committed, but a stale cache read sends the loser to
// the database claim. No concurrent scheduling or Redis expiry is needed.
func TestHandleToken_AgentRefreshClaimLoserReleasesConnectionBeforeReplay(t *testing.T) {
	t.Parallel()

	for _, fallback := range []string{"immediate_cache_hit", "failure_publication_loses_then_cache_hit"} {
		t.Run(fallback, func(t *testing.T) {
			t.Parallel()
			for _, suspend := range []bool{false, true} {
				name := "live_agent"
				if suspend {
					name = "suspended_agent"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					var replayCache *claimLoserReplayCache
					ctx, ti := newTestMCPServiceWithPoolConfig(
						t, testenv.NewLogger(t), testenv.NewMeterProvider(t),
						&mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{},
						func(delegate cache.Cache) cache.Cache {
							replayCache = &claimLoserReplayCache{Cache: delegate, t: t}
							return replayCache
						},
						func(config *pgxpool.Config) {
							config.MaxConns = 1
							config.MinConns = 0
						},
					)
					require.EqualValues(t, 1, ti.conn.Config().MaxConns)
					fx, agent, refreshToken, _ := seedAgentRefreshSession(t, ctx, ti)
					winner := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
					require.NoError(t, winner.err)
					require.Equal(t, http.StatusOK, winner.code, winner.body)

					replayCache.key, _ = refreshReplayKeys(fx.target.UserSessionIssuerID, refreshToken)
					replayCache.missAfterClaim = fallback == "failure_publication_loses_then_cache_hit"
					replayCache.beforeClaimReplay = func(ctx context.Context) {
						// This hook runs at the FIRST cache Get after ErrNoRows, even
						// when that Get misses. A deferred rollback is too late.
						require.Zero(t, ti.conn.Stat().AcquiredConns(), "lost database claim must release its connection before reading replay cache")
						if suspend {
							_, err := agents_repo.New(ti.conn).SuspendAgent(ctx, agents_repo.SuspendAgentParams{
								OrganizationID: fx.orgID, ID: agent.ID,
							})
							require.NoError(t, err)
						}
					}
					// Bound unexpected pool starvation without using time to steer
					// execution. All interleavings are forced by the cache hooks.
					requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					loser := performRefreshRequest(requestCtx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
					require.NoError(t, loser.err)
					require.NoError(t, requestCtx.Err())
					if suspend {
						require.Equal(t, http.StatusBadRequest, loser.code, loser.body)
						require.Contains(t, loser.body, "agent authorization is no longer valid")
					} else {
						require.Equal(t, http.StatusOK, loser.code, loser.body)
						assertSameTokenPair(t, winner.body, loser.body)
					}
					if replayCache.missAfterClaim {
						require.Equal(t, 3, replayCache.gets)
						require.Equal(t, 1, replayCache.failurePublications)
					} else {
						require.Equal(t, 2, replayCache.gets)
						require.Zero(t, replayCache.failurePublications)
					}
					require.Equal(t, 1, replayCache.leases)
					require.Zero(t, ti.conn.Stat().AcquiredConns())
				})
			}
		})
	}
}

// Armed only after a real rotation has stored a valid, encrypted winner replay.
// Hide that replay on the initial lookup (and optionally after claim loss),
// while preserving it for the real conditional publication to lose against.
// Requests are synchronous, so these counters need no synchronization.
type claimLoserReplayCache struct {
	cache.Cache
	t                   *testing.T
	key                 string
	missAfterClaim      bool
	gets                int
	leases              int
	failurePublications int
	beforeClaimReplay   func(context.Context)
}

func (c *claimLoserReplayCache) Get(ctx context.Context, key string, value any) error {
	if key == c.key {
		c.gets++
		if c.gets == 1 {
			return redisCache.ErrCacheMiss
		}
		if c.gets == 2 {
			c.beforeClaimReplay(ctx)
			if c.missAfterClaim {
				return redisCache.ErrCacheMiss
			}
		}
	}
	if err := c.Cache.Get(ctx, key, value); err != nil {
		return fmt.Errorf("get claim loser replay: %w", err)
	}
	return nil
}

func (c *claimLoserReplayCache) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	acquired, err := acquireLease(ctx, c.Cache, key, owner, ttl)
	if key == "lock:"+c.key {
		require.NoError(c.t, err)
		require.True(c.t, acquired, "claim loser must own the lease to attempt failure publication")
		c.leases++
	}
	return acquired, err
}

func (c *claimLoserReplayCache) ReleaseLeaseIfOwner(ctx context.Context, key, owner string) (bool, error) {
	return releaseLease(ctx, c.Cache, key, owner)
}

func (c *claimLoserReplayCache) SetIfAbsent(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	stored, err := setIfAbsent(ctx, c.Cache, key, value, ttl)
	if key == c.key {
		c.failurePublications++
		require.True(c.t, c.missAfterClaim)
		require.Equal(c.t, 2, c.gets)
		require.NoError(c.t, err)
		require.False(c.t, stored, "failure publication must lose to the cached winner")
	}
	return stored, err
}

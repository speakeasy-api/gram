package spendrules_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/spendrules/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type gateCache struct {
	values map[string]any
	getErr error
}

func newGateCache() *gateCache {
	return &gateCache{values: map[string]any{}, getErr: nil}
}

func (c *gateCache) Get(_ context.Context, key string, value any) error {
	if c.getErr != nil {
		return c.getErr
	}
	raw, ok := c.values[key]
	if !ok {
		return redisCache.ErrCacheMiss
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal gate cache value: %w", err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("unmarshal gate cache value: %w", err)
	}
	return nil
}

func (c *gateCache) GetAndDelete(ctx context.Context, key string, value any) error {
	if err := c.Get(ctx, key, value); err != nil {
		return err
	}
	delete(c.values, key)
	return nil
}

func (c *gateCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	c.values[key] = value
	return nil
}

func (c *gateCache) Add(_ context.Context, key string, _ time.Duration) (bool, error) {
	if _, ok := c.values[key]; ok {
		return false, nil
	}
	c.values[key] = "1"
	return true, nil
}

func (c *gateCache) Update(ctx context.Context, key string, value any) error {
	return c.Set(ctx, key, value, 0)
}

func (c *gateCache) Delete(_ context.Context, key string) error {
	delete(c.values, key)
	return nil
}

func (c *gateCache) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (c *gateCache) ListAppend(_ context.Context, _ string, _ any, _ time.Duration) error {
	return nil
}

func (c *gateCache) ListRange(_ context.Context, _ string, _, _ int64, _ any) error {
	return nil
}

func (c *gateCache) DeleteByPrefix(_ context.Context, prefix string) error {
	for key := range c.values {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(c.values, key)
		}
	}
	return nil
}

func TestGateEvaluatesRuleCELAgainstCachedUsage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	actors := testActors()
	windowEnd := time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC)
	state := spendrules.NewGateState("org_123", actors)
	state.Rules = append(state.Rules, spendrules.GateRule{
		RuleURN:    "spend_rule:engineering:v1",
		RuleName:   "Engineering budget",
		Action:     spendrules.ActionBlock,
		TargetExpr: `department_name == "Engineering"`,
		RuleExpr:   `used_pct >= warn_at_pct`,
		LimitUSD:   100,
		WarnAtPct:  80,
		WindowKind: spendrules.WindowMonthly,
		WindowEnd:  windowEnd,
	})
	state.SetActorWindowSpend("org_123", actors[0], chrepo.ActorWindowSpendRow{
		Email:       "ada@acme.com",
		DailyCost:   0,
		WeeklyCost:  0,
		MonthlyCost: 90,
	})
	require.Contains(t, state.Actors, "org_123:ada@acme.com")

	require.NoError(t, spendrules.WriteGateState(ctx, cacheImpl, "org_123", state))
	gate := spendrules.NewGate(testenv.NewLogger(t), cacheImpl)

	block, err := gate.CheckBlocked(ctx, "org_123", "Ada@Acme.com")
	require.NoError(t, err)
	require.NotNil(t, block)
	require.Equal(t, "spend_rule:engineering:v1", block.RuleURN)
	require.Equal(t, "Engineering budget", block.RuleName)
	require.Equal(t, windowEnd, block.WindowEnd)
}

func TestGateEvaluatesTargetCELBeforeRuleCEL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	actors := testActors()
	state := spendrules.NewGateState("org_123", actors)
	state.Rules = append(state.Rules, spendrules.GateRule{
		RuleURN:    "spend_rule:engineering:v1",
		RuleName:   "Engineering budget",
		Action:     spendrules.ActionBlock,
		TargetExpr: `department_name == "Engineering"`,
		RuleExpr:   `spend_usd >= limit_usd`,
		LimitUSD:   100,
		WarnAtPct:  80,
		WindowKind: spendrules.WindowMonthly,
		WindowEnd:  time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC),
	})
	state.SetActorWindowSpend("org_123", actors[1], chrepo.ActorWindowSpendRow{
		Email:       "sam@acme.com",
		DailyCost:   0,
		WeeklyCost:  0,
		MonthlyCost: 150,
	})

	require.NoError(t, spendrules.WriteGateState(ctx, cacheImpl, "org_123", state))
	gate := spendrules.NewGate(testenv.NewLogger(t), cacheImpl)

	block, err := gate.CheckBlocked(ctx, "org_123", "Sam@Acme.com")
	require.NoError(t, err)
	require.Nil(t, block)
}

func TestGateSkipsUnresolvedIdentity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	cacheImpl.getErr = errors.New("gate must not read the cache for unresolved identities")
	gate := spendrules.NewGate(testenv.NewLogger(t), cacheImpl)

	block, err := gate.CheckBlocked(ctx, "", "ada@acme.com")
	require.NoError(t, err)
	require.Nil(t, block)

	block, err = gate.CheckBlocked(ctx, "org_123", "")
	require.NoError(t, err)
	require.Nil(t, block)
}

func TestGateSurfacesCacheFailuresForFailOpen(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	cacheImpl.getErr = errors.New("redis unavailable")
	gate := spendrules.NewGate(testenv.NewLogger(t), cacheImpl)

	block, err := gate.CheckBlocked(ctx, "org_123", "ada@acme.com")
	require.Error(t, err)
	require.Nil(t, block)
}

func TestGateWriteEmptyStateClearsCache(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	state := spendrules.NewGateState("org_123", testActors())
	state.Rules = append(state.Rules, spendrules.GateRule{
		RuleURN:    "spend_rule:engineering:v1",
		RuleName:   "Engineering budget",
		Action:     spendrules.ActionBlock,
		TargetExpr: `department_name == "Engineering"`,
		RuleExpr:   `spend_usd >= limit_usd`,
		LimitUSD:   100,
		WarnAtPct:  80,
		WindowKind: spendrules.WindowMonthly,
		WindowEnd:  time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC),
	})

	require.NoError(t, spendrules.WriteGateState(ctx, cacheImpl, "org_123", state))
	require.NoError(t, spendrules.WriteGateState(ctx, cacheImpl, "org_123", spendrules.GateState{Rules: nil, Actors: nil}))

	gate := spendrules.NewGate(testenv.NewLogger(t), cacheImpl)
	block, err := gate.CheckBlocked(ctx, "org_123", "ada@acme.com")
	require.NoError(t, err)
	require.Nil(t, block)
}

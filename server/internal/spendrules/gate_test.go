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

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
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

func newTestGate(t *testing.T, cacheImpl *gateCache) *spendrules.Gate {
	t.Helper()

	celEng, err := celenv.New()
	require.NoError(t, err)
	gate, err := spendrules.NewGate(testenv.NewLogger(t), cacheImpl, celEng)
	require.NoError(t, err)
	return gate
}

func TestNewGateRequiresCELEngine(t *testing.T) {
	t.Parallel()

	gate, err := spendrules.NewGate(testenv.NewLogger(t), newGateCache(), nil)
	require.Error(t, err)
	require.Nil(t, gate)
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

func writeGateRules(t *testing.T, ctx context.Context, cacheImpl *gateCache, organizationID string, rules ...spendrules.GateRule) {
	t.Helper()

	require.NoError(t, spendrules.WriteGateRules(ctx, cacheImpl, organizationID, spendrules.GateRules{
		SourceUpdatedAt: time.Now().UTC(),
		Rules:           rules,
	}))
}

func writeGateActor(t *testing.T, ctx context.Context, cacheImpl *gateCache, organizationID string, actor spendrules.Actor, spend chrepo.ActorWindowSpendRow) {
	t.Helper()

	require.NoError(t, spendrules.WriteGateActor(ctx, cacheImpl, organizationID, spendrules.NewGateActor(actor, spend, time.Now().UTC())))
}

func TestGateEvaluatesRuleCELAgainstCachedUsage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	actors := testActors()
	// Future window: the block must fire on cached usage, not be skipped as an
	// already-reset window.
	windowStart := time.Now().UTC().Add(-time.Hour)
	windowEnd := time.Now().UTC().AddDate(0, 0, 7)
	writeGateRules(t, ctx, cacheImpl, "org_123", spendrules.GateRule{
		RuleURN:     "spend_rule:engineering:v1",
		RuleName:    "Engineering budget",
		Action:      spendrules.ActionBlock,
		TargetExpr:  `department_name == "Engineering"`,
		RuleExpr:    `used_pct >= warn_at_pct`,
		LimitUSD:    100,
		WarnAtPct:   80,
		WindowKind:  spendrules.WindowMonthly,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	})
	writeGateActor(t, ctx, cacheImpl, "org_123", actors[0], chrepo.ActorWindowSpendRow{
		Email:       "ada@acme.com",
		DailyCost:   0,
		WeeklyCost:  0,
		MonthlyCost: 90,
	})
	gate := newTestGate(t, cacheImpl)

	block, err := gate.CheckBlocked(ctx, "org_123", "user_ada")
	require.NoError(t, err)
	require.NotNil(t, block)
	require.Equal(t, "spend_rule:engineering:v1", block.RuleURN)
	require.Equal(t, "Engineering budget", block.RuleName)
	require.Equal(t, windowEnd, block.WindowEnd)
}

func TestGateSkipsExpiredWindow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	actors := testActors()
	writeGateRules(t, ctx, cacheImpl, "org_123", spendrules.GateRule{
		RuleURN:     "spend_rule:engineering:v1",
		RuleName:    "Engineering budget",
		Action:      spendrules.ActionBlock,
		TargetExpr:  `department_name == "Engineering"`,
		RuleExpr:    `used_pct >= warn_at_pct`,
		LimitUSD:    100,
		WarnAtPct:   80,
		WindowKind:  spendrules.WindowMonthly,
		WindowStart: time.Now().UTC().Add(-2 * time.Hour),
		// Window already ended: the actor entry's spend belongs to a window that
		// has reset, so the block must lift even though the cached usage breaches.
		WindowEnd: time.Now().UTC().Add(-time.Hour),
	})
	writeGateActor(t, ctx, cacheImpl, "org_123", actors[0], chrepo.ActorWindowSpendRow{
		Email:       "ada@acme.com",
		DailyCost:   0,
		WeeklyCost:  0,
		MonthlyCost: 90,
	})
	gate := newTestGate(t, cacheImpl)

	block, err := gate.CheckBlocked(ctx, "org_123", "user_ada")
	require.NoError(t, err)
	require.Nil(t, block)
}

func TestGateEvaluatesTargetCELBeforeRuleCEL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	actors := testActors()
	writeGateRules(t, ctx, cacheImpl, "org_123", spendrules.GateRule{
		RuleURN:     "spend_rule:engineering:v1",
		RuleName:    "Engineering budget",
		Action:      spendrules.ActionBlock,
		TargetExpr:  `department_name == "Engineering"`,
		RuleExpr:    `spend_usd >= limit_usd`,
		LimitUSD:    100,
		WarnAtPct:   80,
		WindowKind:  spendrules.WindowMonthly,
		WindowStart: time.Now().UTC().Add(-time.Hour),
		WindowEnd:   time.Now().UTC().AddDate(0, 0, 7),
	})
	writeGateActor(t, ctx, cacheImpl, "org_123", actors[1], chrepo.ActorWindowSpendRow{
		Email:       "sam@acme.com",
		DailyCost:   0,
		WeeklyCost:  0,
		MonthlyCost: 150,
	})
	gate := newTestGate(t, cacheImpl)

	block, err := gate.CheckBlocked(ctx, "org_123", "user_sam")
	require.NoError(t, err)
	require.Nil(t, block)
}

func TestGateSkipsActorComputedBeforeWindowStart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	actors := testActors()
	windowStart := time.Now().UTC().Add(time.Hour)
	writeGateRules(t, ctx, cacheImpl, "org_123", spendrules.GateRule{
		RuleURN:     "spend_rule:engineering:v1",
		RuleName:    "Engineering budget",
		Action:      spendrules.ActionBlock,
		TargetExpr:  `department_name == "Engineering"`,
		RuleExpr:    `spend_usd >= limit_usd`,
		LimitUSD:    100,
		WarnAtPct:   80,
		WindowKind:  spendrules.WindowMonthly,
		WindowStart: windowStart,
		WindowEnd:   windowStart.AddDate(0, 1, 0),
	})
	require.NoError(t, spendrules.WriteGateActor(ctx, cacheImpl, "org_123", spendrules.NewGateActor(actors[0], chrepo.ActorWindowSpendRow{
		Email:       "ada@acme.com",
		DailyCost:   0,
		WeeklyCost:  0,
		MonthlyCost: 150,
	}, windowStart.Add(-time.Second))))

	gate := newTestGate(t, cacheImpl)
	block, err := gate.CheckBlocked(ctx, "org_123", "user_ada")
	require.NoError(t, err)
	require.Nil(t, block)
}

func TestGateSkipsUnresolvedIdentity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	cacheImpl.getErr = errors.New("gate must not read the cache for unresolved identities")
	gate := newTestGate(t, cacheImpl)

	block, err := gate.CheckBlocked(ctx, "", "user_ada")
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
	gate := newTestGate(t, cacheImpl)

	block, err := gate.CheckBlocked(ctx, "org_123", "user_ada")
	require.Error(t, err)
	require.Nil(t, block)
}

func TestGateEmptyRulesAllow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeGateActor(t, ctx, cacheImpl, "org_123", testActors()[0], chrepo.ActorWindowSpendRow{
		Email:       "ada@acme.com",
		DailyCost:   0,
		WeeklyCost:  0,
		MonthlyCost: 150,
	})
	writeGateRules(t, ctx, cacheImpl, "org_123")

	gate := newTestGate(t, cacheImpl)
	block, err := gate.CheckBlocked(ctx, "org_123", "user_ada")
	require.NoError(t, err)
	require.Nil(t, block)
}

func TestWriteGateActorNoopCacheDoesNotFail(t *testing.T) {
	t.Parallel()

	actor := testActors()[0]
	err := spendrules.WriteGateActor(t.Context(), cache.NoopCache, "org_123", spendrules.NewGateActor(actor, spendrules.EmptyActorSpend(actor.Email), time.Now().UTC()))
	require.NoError(t, err)
}

func TestWriteGateRulesFallbackSkipsOlderPayload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	newer := spendrules.GateRules{
		SourceUpdatedAt: time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC),
		Rules: []spendrules.GateRule{{
			RuleURN:     "spend_rule:newer:v1",
			RuleName:    "Newer",
			Action:      spendrules.ActionBlock,
			TargetExpr:  `true`,
			RuleExpr:    `spend_usd >= limit_usd`,
			LimitUSD:    100,
			WarnAtPct:   80,
			WindowKind:  spendrules.WindowMonthly,
			WindowStart: time.Now().UTC().Add(-time.Hour),
			WindowEnd:   time.Now().UTC().Add(time.Hour),
		}},
	}
	older := spendrules.GateRules{
		SourceUpdatedAt: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		Rules: []spendrules.GateRule{{
			RuleURN:     "spend_rule:older:v1",
			RuleName:    "Older",
			Action:      spendrules.ActionBlock,
			TargetExpr:  `true`,
			RuleExpr:    `spend_usd >= limit_usd`,
			LimitUSD:    100,
			WarnAtPct:   80,
			WindowKind:  spendrules.WindowMonthly,
			WindowStart: time.Now().UTC().Add(-time.Hour),
			WindowEnd:   time.Now().UTC().Add(time.Hour),
		}},
	}
	require.NoError(t, spendrules.WriteGateRules(ctx, cacheImpl, "org_123", newer))
	require.NoError(t, spendrules.WriteGateRules(ctx, cacheImpl, "org_123", older))

	var cached spendrules.GateRules
	require.NoError(t, cacheImpl.Get(ctx, "spend_gate:rules:org_123", &cached))
	require.Equal(t, "spend_rule:newer:v1", cached.Rules[0].RuleURN)
}

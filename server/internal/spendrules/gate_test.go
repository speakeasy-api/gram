package spendrules_test

import (
	"context"
	"errors"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// memoryCache is a minimal in-memory cache.Cache for gate tests. Only the
// methods the gate and block-set writer use are functional.
type memoryCache struct {
	values map[string]spendrules.BlockSet
	getErr error
}

var _ cache.Cache = (*memoryCache)(nil)

func newMemoryCache() *memoryCache {
	return &memoryCache{values: map[string]spendrules.BlockSet{}, getErr: nil}
}

func (m *memoryCache) Get(_ context.Context, key string, value any) error {
	if m.getErr != nil {
		return m.getErr
	}
	stored, ok := m.values[key]
	if !ok {
		return redisCache.ErrCacheMiss
	}
	target, ok := value.(*spendrules.BlockSet)
	if !ok {
		return errors.New("unexpected target type")
	}
	*target = stored
	return nil
}

func (m *memoryCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	blocks, ok := value.(spendrules.BlockSet)
	if !ok {
		return errors.New("unexpected value type")
	}
	m.values[key] = blocks
	return nil
}

func (m *memoryCache) Delete(_ context.Context, key string) error {
	delete(m.values, key)
	return nil
}

func (m *memoryCache) GetAndDelete(context.Context, string, any) error { return nil }
func (m *memoryCache) Add(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}
func (m *memoryCache) Update(context.Context, string, any) error           { return nil }
func (m *memoryCache) Expire(context.Context, string, time.Duration) error { return nil }
func (m *memoryCache) ListAppend(context.Context, string, any, time.Duration) error {
	return nil
}
func (m *memoryCache) ListRange(context.Context, string, int64, int64, any) error {
	return nil
}
func (m *memoryCache) DeleteByPrefix(context.Context, string) error { return nil }

func TestGateBlockSetRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mem := newMemoryCache()
	gate := spendrules.NewGate(testenv.NewLogger(t), mem)

	windowEnd := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	blocks := spendrules.BlockSet{
		"user_ada":     {RuleURN: "spend_rule:33333333-3333-3333-3333-333333333333:v2", RuleName: "Intern cap", WindowEnd: windowEnd},
		"ada@acme.com": {RuleURN: "spend_rule:33333333-3333-3333-3333-333333333333:v2", RuleName: "Intern cap", WindowEnd: windowEnd},
	}
	require.NoError(t, spendrules.WriteBlockSet(ctx, mem, "org_1", blocks))

	// Blocked by user id.
	block, err := gate.CheckBlocked(ctx, "org_1", "user_ada", "")
	require.NoError(t, err)
	require.NotNil(t, block)
	require.Equal(t, "Intern cap", block.RuleName)
	require.Equal(t, windowEnd, block.WindowEnd.UTC())

	// Blocked by email, normalized case-insensitively.
	block, err = gate.CheckBlocked(ctx, "org_1", "", "ADA@ACME.COM")
	require.NoError(t, err)
	require.NotNil(t, block)

	// Unblocked actor in the same org.
	block, err = gate.CheckBlocked(ctx, "org_1", "user_bea", "bea@acme.com")
	require.NoError(t, err)
	require.Nil(t, block)

	// Another org has no circuit state at all.
	block, err = gate.CheckBlocked(ctx, "org_2", "user_ada", "ada@acme.com")
	require.NoError(t, err)
	require.Nil(t, block)
}

func TestGateWriteEmptyBlockSetClearsState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mem := newMemoryCache()
	gate := spendrules.NewGate(testenv.NewLogger(t), mem)

	require.NoError(t, spendrules.WriteBlockSet(ctx, mem, "org_1", spendrules.BlockSet{
		"user_ada": {RuleURN: "spend_rule:33333333-3333-3333-3333-333333333333:v1", RuleName: "Cap", WindowEnd: time.Now()},
	}))
	require.NoError(t, spendrules.WriteBlockSet(ctx, mem, "org_1", nil))

	block, err := gate.CheckBlocked(ctx, "org_1", "user_ada", "")
	require.NoError(t, err)
	require.Nil(t, block)
}

func TestGateSkipsUnresolvedIdentity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mem := newMemoryCache()
	mem.getErr = errors.New("gate must not read the cache for unresolved identities")
	gate := spendrules.NewGate(testenv.NewLogger(t), mem)

	block, err := gate.CheckBlocked(ctx, "", "user_ada", "ada@acme.com")
	require.NoError(t, err)
	require.Nil(t, block)

	block, err = gate.CheckBlocked(ctx, "org_1", "", "")
	require.NoError(t, err)
	require.Nil(t, block)
}

func TestGateSurfacesCacheFailuresForFailOpen(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mem := newMemoryCache()
	mem.getErr = errors.New("redis unavailable")
	gate := spendrules.NewGate(testenv.NewLogger(t), mem)

	// Infrastructure failures return (nil, err): the caller logs and treats
	// the actor as not blocked.
	block, err := gate.CheckBlocked(ctx, "org_1", "user_ada", "")
	require.Error(t, err)
	require.Nil(t, block)
}

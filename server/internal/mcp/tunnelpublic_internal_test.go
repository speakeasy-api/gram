package mcp

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

func TestTunnelPublicConfigWithDefaults(t *testing.T) {
	t.Parallel()

	t.Run("zero config uses built-in defaults", func(t *testing.T) {
		t.Parallel()
		cfg := TunnelPublicConfig{
			SessionTTL:         0,
			LiveSessionCap:     0,
			InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
			RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
			MaxRequestLifetime: 0,
		}.withDefaults()
		require.Equal(t, ratelimit.PerSecond(50).WithBurst(100), cfg.RequestRate)
		require.Equal(t, ratelimit.PerSecond(5).WithBurst(20), cfg.InitializeRate)
		require.Equal(t, 24*time.Hour, cfg.SessionTTL)
		require.Equal(t, 10000, cfg.LiveSessionCap)
		require.Equal(t, time.Hour, cfg.MaxRequestLifetime)
	})

	t.Run("explicit rate and burst are kept", func(t *testing.T) {
		t.Parallel()
		cfg := TunnelPublicConfig{
			SessionTTL:         0,
			LiveSessionCap:     0,
			InitializeRate:     ratelimit.PerSecond(100).WithBurst(150),
			RequestRate:        ratelimit.PerSecond(1000).WithBurst(3000),
			MaxRequestLifetime: 0,
		}.withDefaults()
		require.Equal(t, ratelimit.PerSecond(1000).WithBurst(3000), cfg.RequestRate)
		require.Equal(t, ratelimit.PerSecond(100).WithBurst(150), cfg.InitializeRate)
	})

	t.Run("rate without burst gets twice the sustained rate", func(t *testing.T) {
		t.Parallel()
		cfg := TunnelPublicConfig{
			SessionTTL:         0,
			LiveSessionCap:     0,
			InitializeRate:     ratelimit.PerSecond(100).WithBurst(0),
			RequestRate:        ratelimit.PerSecond(1000).WithBurst(0),
			MaxRequestLifetime: 0,
		}.withDefaults()
		require.Equal(t, ratelimit.PerSecond(1000).WithBurst(2000), cfg.RequestRate)
		require.Equal(t, ratelimit.PerSecond(100).WithBurst(200), cfg.InitializeRate)
	})

	t.Run("burst without rate falls back to the default", func(t *testing.T) {
		t.Parallel()
		cfg := TunnelPublicConfig{
			SessionTTL:         0,
			LiveSessionCap:     0,
			InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
			RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 99},
			MaxRequestLifetime: 0,
		}.withDefaults()
		require.Equal(t, ratelimit.PerSecond(50).WithBurst(100), cfg.RequestRate)
	})
}

func TestStoredPublicRate(t *testing.T) {
	t.Parallel()

	zero := ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0}
	unset := pgtype.Int4{Int32: 0, Valid: false}

	require.Equal(t, zero, storedPublicRate(unset, unset), "no stored rate means the default")
	require.Equal(t, zero, storedPublicRate(unset, pgtype.Int4{Int32: 450, Valid: true}), "a burst without a rate is ignored")
	require.Equal(t, ratelimit.PerSecond(300).WithBurst(450), storedPublicRate(pgtype.Int4{Int32: 300, Valid: true}, pgtype.Int4{Int32: 450, Valid: true}))
	require.Equal(t, ratelimit.PerSecond(300).WithBurst(600), storedPublicRate(pgtype.Int4{Int32: 300, Valid: true}, unset), "a stored rate without a burst gets twice the rate")
	require.Equal(t, "t@300/450", storedPublicRateKey("t", ratelimit.PerSecond(300).WithBurst(450)))
}

func TestTunnelPublicRuntimeLimiterCache(t *testing.T) {
	t.Parallel()

	rt := &tunnelPublicRuntime{
		cfg:           TunnelPublicConfig{}.withDefaults(),
		sessions:      nil,
		store:         ratelimit.NewRedisStore(nil),
		meterProvider: nil,
		defaults:      tunnelPublicLimiters{request: nil, requestKey: "", initialize: nil, initializeKey: ""},
		limiters:      sync.Map{},
		metrics:       nil,
	}
	rt.meterProvider = testenv.NewMeterProvider(t)

	a := rt.limiter(tunnelPublicRequestLimiterName, ratelimit.PerSecond(300).WithBurst(450))
	b := rt.limiter(tunnelPublicRequestLimiterName, ratelimit.PerSecond(300).WithBurst(450))
	c := rt.limiter(tunnelPublicRequestLimiterName, ratelimit.PerSecond(300).WithBurst(600))
	d := rt.limiter(tunnelPublicInitializeLimiterName, ratelimit.PerSecond(300).WithBurst(450))
	require.Same(t, a, b, "same name and rate reuse one limiter")
	require.NotSame(t, a, c, "a different burst is a different limiter")
	require.NotSame(t, a, d, "a different name is a different limiter")
}

func TestTunnelPublicRuntimeLimitersForServer(t *testing.T) {
	t.Parallel()

	defaults := tunnelPublicLimiters{request: &ratelimit.Limiter{}, requestKey: "", initialize: &ratelimit.Limiter{}, initializeKey: ""}
	rt := &tunnelPublicRuntime{
		cfg:           TunnelPublicConfig{}.withDefaults(),
		sessions:      nil,
		store:         ratelimit.NewRedisStore(nil),
		meterProvider: testenv.NewMeterProvider(t),
		defaults:      defaults,
		limiters:      sync.Map{},
		metrics:       nil,
	}
	unset := pgtype.Int4{Int32: 0, Valid: false}
	id := uuid.New()

	plain := rt.limitersForServer(&tunneledmcprepo.TunneledMcpServer{ID: id, PublicRequestRatePerSecond: unset, PublicRequestBurst: unset})
	require.Same(t, defaults.request, plain.request, "no stored rate keeps the deployment-wide request limiter")
	require.Same(t, defaults.initialize, plain.initialize, "no stored rate keeps the deployment-wide initialize limiter")
	require.Equal(t, id.String(), plain.requestKey)
	require.Equal(t, id.String(), plain.initializeKey)

	stored := rt.limitersForServer(&tunneledmcprepo.TunneledMcpServer{ID: id, PublicRequestRatePerSecond: pgtype.Int4{Int32: 300, Valid: true}, PublicRequestBurst: pgtype.Int4{Int32: 450, Valid: true}})
	require.NotSame(t, defaults.request, stored.request, "a stored rate gets its own request limiter")
	require.NotSame(t, defaults.initialize, stored.initialize, "the stored rate feeds the initialize bucket too")
	require.Equal(t, id.String()+"@300/450", stored.requestKey, "a stored rate meters a rate-suffixed key")
	require.Equal(t, stored.requestKey, stored.initializeKey)
	require.Same(t, stored.request, rt.limitersForServer(&tunneledmcprepo.TunneledMcpServer{ID: id, PublicRequestRatePerSecond: pgtype.Int4{Int32: 300, Valid: true}, PublicRequestBurst: pgtype.Int4{Int32: 450, Valid: true}}).request, "the same stored rate reuses one limiter")
}

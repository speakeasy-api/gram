package redisrole_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/redisrole"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	sessionsKey  = "auth:login_nonce:n"
	evictableKey = "toolset:a:b:1:"
)

func newGuardedClient(t *testing.T, guard *redisrole.Guard, role redisrole.Role) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	client.AddHook(guard.Hook(role))
	return client, mr
}

// roleCommands sends a representative set of commands addressing key and
// returns the error of each, by command name.
func roleCommands(ctx context.Context, client *redis.Client, key string) map[string]error {
	errs := map[string]error{}
	errs["set"] = client.Set(ctx, key, "v", 0).Err()
	errs["get"] = client.Get(ctx, key).Err()
	errs["mget"] = client.MGet(ctx, key).Err()
	errs["eval"] = client.Eval(ctx, "return redis.call('GET', KEYS[1])", []string{key}).Err()
	errs["del"] = client.Del(ctx, key).Err()
	errs["xadd"] = client.XAdd(ctx, &redis.XAddArgs{Stream: key + "stream", Values: []any{"f", "v"}}).Err()
	errs["xread"] = client.XRead(ctx, &redis.XReadArgs{Streams: []string{key + "stream", "0"}, Count: 1, Block: -1}).Err()
	errs["scan"] = client.Scan(ctx, 0, key+"*", 10).Err()
	_, errs["pipeline"] = client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, key, "v", 0)
		pipe.Get(ctx, key)
		return nil
	})
	return errs
}

func TestGuardEnforceRefusesWrongRoleOnSessionsClient(t *testing.T) {
	t.Parallel()

	guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), redisrole.GuardEnforce)
	client, mr := newGuardedClient(t, guard, redisrole.RoleSessions)

	for name, err := range roleCommands(t.Context(), client, evictableKey) {
		require.ErrorIs(t, err, redisrole.ErrWrongRole, "command %s", name)
	}
	require.Empty(t, mr.Keys())

	for name, err := range roleCommands(t.Context(), client, sessionsKey) {
		require.NoError(t, err, "command %s", name)
	}
}

func TestGuardEnforceRefusesWrongRoleOnEvictableClient(t *testing.T) {
	t.Parallel()

	guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), redisrole.GuardEnforce)
	client, mr := newGuardedClient(t, guard, redisrole.RoleEvictable)

	for name, err := range roleCommands(t.Context(), client, sessionsKey) {
		require.ErrorIs(t, err, redisrole.ErrWrongRole, "command %s", name)
	}
	require.Empty(t, mr.Keys())

	for name, err := range roleCommands(t.Context(), client, evictableKey) {
		require.NoError(t, err, "command %s", name)
	}
}

func TestGuardEnforceAllowsCommandsWithoutKeys(t *testing.T) {
	t.Parallel()

	guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), redisrole.GuardEnforce)
	client, _ := newGuardedClient(t, guard, redisrole.RoleEvictable)

	require.NoError(t, client.Ping(t.Context()).Err())
	require.NoError(t, client.Scan(t.Context(), 0, "", 10).Err())
}

func TestGuardReportRunsCommandAndCountsIt(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	guard := redisrole.NewGuard(testenv.NewLogger(t), provider, redisrole.GuardReport)
	client, mr := newGuardedClient(t, guard, redisrole.RoleSessions)

	require.NoError(t, client.Set(t.Context(), evictableKey, "v", 0).Err())
	require.True(t, mr.Exists(evictableKey))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)
	sum, ok := rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	require.Equal(t, int64(1), sum.DataPoints[0].Value)
	require.Equal(t, attribute.NewSet(
		attr.RedisRole(string(redisrole.RoleSessions)),
		attr.RedisKeyPrefix("toolset:"),
		attr.RedisCommand("set"),
	), sum.DataPoints[0].Attributes)
}

func TestParseGuardMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value   string
		want    redisrole.GuardMode
		wantErr bool
	}{
		{value: "", want: redisrole.GuardReport, wantErr: false},
		{value: "report", want: redisrole.GuardReport, wantErr: false},
		{value: "enforce", want: redisrole.GuardEnforce, wantErr: false},
		{value: "off", want: redisrole.GuardReport, wantErr: true},
	}
	for _, tc := range tests {
		t.Run("value="+tc.value, func(t *testing.T) {
			t.Parallel()
			mode, err := redisrole.ParseGuardMode(tc.value)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, mode)

			roundTrip, err := redisrole.ParseGuardMode(mode.String())
			require.NoError(t, err)
			require.Equal(t, mode, roundTrip)
		})
	}
}

func TestZeroGuardModeReports(t *testing.T) {
	t.Parallel()

	var zero redisrole.GuardMode
	require.Equal(t, redisrole.GuardReport, zero)
}

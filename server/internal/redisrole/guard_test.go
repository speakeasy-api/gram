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
	authKey         = "auth:login_nonce:n"
	opsKey          = "hook:pending:s"
	evictableKey    = "toolset:a:b:1:"
	unclassifiedKey = "undeclared-family:x"
)

// sampleKeys is one key of each role.
var sampleKeys = map[redisrole.Role]string{
	redisrole.RoleAuth:      authKey,
	redisrole.RoleOps:       opsKey,
	redisrole.RoleEvictable: evictableKey,
}

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

func TestGuardEnforceRefusesOtherRolesKeys(t *testing.T) {
	t.Parallel()

	for _, role := range redisrole.Roles() {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), redisrole.GuardEnforce)
			client, mr := newGuardedClient(t, guard, role)

			for keyRole, key := range sampleKeys {
				if keyRole == role {
					continue
				}
				for name, err := range roleCommands(t.Context(), client, key) {
					require.ErrorIs(t, err, redisrole.ErrWrongRole, "command %s with a %s key", name, keyRole)
				}
			}
			require.Empty(t, mr.Keys())

			for name, err := range roleCommands(t.Context(), client, sampleKeys[role]) {
				require.NoError(t, err, "command %s", name)
			}
		})
	}
}

func TestGuardEnforceRefusesUnclassifiedKeysOnEveryRole(t *testing.T) {
	t.Parallel()

	for _, role := range redisrole.Roles() {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), redisrole.GuardEnforce)
			client, mr := newGuardedClient(t, guard, role)

			for name, err := range roleCommands(t.Context(), client, unclassifiedKey) {
				require.ErrorIs(t, err, redisrole.ErrWrongRole, "command %s", name)
			}
			require.Empty(t, mr.Keys())
		})
	}
}

func TestGuardEnforceAllowsSwitchKeysOnEveryRole(t *testing.T) {
	t.Parallel()

	for _, role := range redisrole.Roles() {
		guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), redisrole.GuardEnforce)
		client, _ := newGuardedClient(t, guard, role)
		require.NoError(t, client.MGet(t.Context(), "gram:redis-switch:state:auth", "gram:redis-switch:hold").Err(), role)
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
	client, mr := newGuardedClient(t, guard, redisrole.RoleAuth)

	require.NoError(t, client.Set(t.Context(), evictableKey, "v", 0).Err())
	require.True(t, mr.Exists(evictableKey))

	require.Equal(t, []attribute.Set{attribute.NewSet(
		attr.RedisRole(string(redisrole.RoleAuth)),
		attr.RedisKeyPrefix("toolset:"),
		attr.RedisCommand("set"),
	)}, collectWrongRole(t, reader))
}

// TestGuardReportCountsUnclassifiedKeysOnOps pins production's default: an
// unclassified key reaches the ops client and is counted with the
// unclassified label, which is what the alert watches.
func TestGuardReportCountsUnclassifiedKeysOnOps(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	guard := redisrole.NewGuard(testenv.NewLogger(t), provider, redisrole.GuardReport)
	client, mr := newGuardedClient(t, guard, redisrole.RoleOps)

	require.NoError(t, client.Set(t.Context(), unclassifiedKey, "v", 0).Err())
	require.True(t, mr.Exists(unclassifiedKey))

	require.Equal(t, []attribute.Set{attribute.NewSet(
		attr.RedisRole(string(redisrole.RoleOps)),
		attr.RedisKeyPrefix(redisrole.UnclassifiedPrefix),
		attr.RedisCommand("set"),
	)}, collectWrongRole(t, reader))
}

func TestGuardReportCountsNothingForRightRoleKeys(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	guard := redisrole.NewGuard(testenv.NewLogger(t), provider, redisrole.GuardReport)
	client, _ := newGuardedClient(t, guard, redisrole.RoleOps)

	require.NoError(t, client.Set(t.Context(), opsKey, "v", 0).Err())
	require.Empty(t, collectWrongRole(t, reader))
}

// collectWrongRole returns the attribute sets of the wrong-role counter's
// data points.
func collectWrongRole(t *testing.T, reader *sdkmetric.ManualReader) []attribute.Set {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	var sets []attribute.Set
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				require.Equal(t, int64(1), point.Value)
				sets = append(sets, point.Attributes)
			}
		}
	}
	return sets
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

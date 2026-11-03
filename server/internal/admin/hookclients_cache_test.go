package admin

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/deviceagentversions"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHookClientsCacheRefreshWithoutPageVisit(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, rc.Close()) })
	storage := cache.NewRedisCacheAdapter(rc)
	reads := &atomic.Int32{}
	reader := stubHookClientsReader{
		sessions:  []telemetry.HookClientSessionRow{{OrganizationID: "example", Day: "2026-11-03", Machines: 2}},
		inference: []telemetry.InferenceHookTrafficRow{{OrganizationID: "example", Day: "2026-11-03", Users: 1}},
		reads:     reads,
	}
	devices := deviceagentversions.NewStore(rc)
	require.NoError(t, devices.Record(t.Context(), "example", "machine", "1.0.0", time.Now()))
	warmer := NewHookClientsCache(testenv.NewLogger(t), storage, reader, devices)
	require.NoError(t, warmer.Refresh(t.Context()))
	// A different process reads exactly the key warmed by the scheduled worker.
	pageCache := NewHookClientsCache(testenv.NewLogger(t), storage, reader, devices)
	snapshot, cached, err := pageCache.hookClientsSnapshot(t.Context(), hookClientsDefaultWindowDays, false)
	require.NoError(t, err)
	require.True(t, cached)
	require.Equal(t, reader.sessions, snapshot.Sessions)
	require.Equal(t, reader.inference, snapshot.Inference)
	require.NotEmpty(t, snapshot.DeviceAgents)
	require.Equal(t, int32(1), reads.Load())
	server.FastForward(25 * time.Hour)
	_, err = pageCache.hookClientsCache.Get(t.Context(), hookClientsCacheKey(hookClientsDefaultWindowDays))
	require.NoError(t, err, "snapshot survives a delayed nightly run")
	require.NoError(t, warmer.Refresh(t.Context()))
	require.Equal(t, int32(2), reads.Load(), "scheduled refresh bypasses the existing cache")
}

func TestHookClientsCacheRefreshReturnsCacheFailure(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, rc.Close()) })
	server.SetError("cache unavailable")
	warmer := NewHookClientsCache(testenv.NewLogger(t), cache.NewRedisCacheAdapter(rc), stubHookClientsReader{}, nil)
	require.ErrorContains(t, warmer.Refresh(t.Context()), "cache unavailable")
}

func TestHookClientsCacheRefreshPreservesSnapshotOnReadFailure(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, rc.Close()) })
	warmer := NewHookClientsCache(testenv.NewLogger(t), cache.NewRedisCacheAdapter(rc), stubHookClientsReader{}, nil)
	require.NoError(t, warmer.Refresh(t.Context()))
	before, err := warmer.hookClientsCache.Get(t.Context(), hookClientsCacheKey(hookClientsDefaultWindowDays))
	require.NoError(t, err)
	readErr := errors.New("reader unavailable")
	warmer.hookClients = stubHookClientsReader{err: readErr}
	require.ErrorIs(t, warmer.Refresh(t.Context()), readErr)
	after, err := warmer.hookClientsCache.Get(t.Context(), hookClientsCacheKey(hookClientsDefaultWindowDays))
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestHookClientsCacheRefreshPreservesSnapshotOnDeviceReadFailure(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, rc.Close()) })
	devices := deviceagentversions.NewStore(rc)
	require.NoError(t, devices.Record(t.Context(), "example", "machine", "1.0.0", time.Now()))
	warmer := NewHookClientsCache(testenv.NewLogger(t), cache.NewRedisCacheAdapter(rc), stubHookClientsReader{}, devices)
	require.NoError(t, warmer.Refresh(t.Context()))
	before, err := warmer.hookClientsCache.Get(t.Context(), hookClientsCacheKey(hookClientsDefaultWindowDays))
	require.NoError(t, err)
	require.NotEmpty(t, before.DeviceAgents)
	readErr := errors.New("device versions unavailable")
	warmer.deviceAgentVersions = stubDeviceAgentVersions{err: readErr}
	require.ErrorIs(t, warmer.Refresh(t.Context()), readErr)
	after, err := warmer.hookClientsCache.Get(t.Context(), hookClientsCacheKey(hookClientsDefaultWindowDays))
	require.NoError(t, err)
	require.Equal(t, before, after)
	// The same activity can recover on retry without clearing the saved snapshot.
	warmer.deviceAgentVersions = devices
	require.NoError(t, warmer.Refresh(t.Context()))
	recovered, err := warmer.hookClientsCache.Get(t.Context(), hookClientsCacheKey(hookClientsDefaultWindowDays))
	require.NoError(t, err)
	require.Equal(t, before.DeviceAgents, recovered.DeviceAgents)
	require.True(t, recovered.QueriedAt.After(before.QueriedAt))
}

func TestHookClientsCachePageRefreshPreservesSnapshotOnDeviceReadFailure(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, rc.Close()) })
	devices := deviceagentversions.NewStore(rc)
	require.NoError(t, devices.Record(t.Context(), "example", "machine", "1.0.0", time.Now()))
	reader := stubHookClientsReader{
		sessions:  []telemetry.HookClientSessionRow{{OrganizationID: "example", Day: "2026-11-03", Machines: 2}},
		inference: []telemetry.InferenceHookTrafficRow{{OrganizationID: "example", Day: "2026-11-03", Users: 1}},
	}
	warmer := NewHookClientsCache(testenv.NewLogger(t), cache.NewRedisCacheAdapter(rc), reader, devices)
	require.NoError(t, warmer.Refresh(t.Context()))
	before, err := warmer.hookClientsCache.Get(t.Context(), hookClientsCacheKey(hookClientsDefaultWindowDays))
	require.NoError(t, err)
	require.NotEmpty(t, before.DeviceAgents)
	warmer.deviceAgentVersions = stubDeviceAgentVersions{err: errors.New("device versions unavailable")}
	response, cached, err := warmer.hookClientsSnapshot(t.Context(), hookClientsDefaultWindowDays, true)
	require.NoError(t, err)
	require.False(t, cached)
	require.Equal(t, reader.sessions, response.Sessions)
	require.Equal(t, reader.inference, response.Inference)
	require.Empty(t, response.DeviceAgents)
	after, err := warmer.hookClientsCache.Get(t.Context(), hookClientsCacheKey(hookClientsDefaultWindowDays))
	require.NoError(t, err)
	require.Equal(t, before, after)
	warmer.deviceAgentVersions = devices
	recovered, _, err := warmer.hookClientsSnapshot(t.Context(), hookClientsDefaultWindowDays, true)
	require.NoError(t, err)
	after, err = warmer.hookClientsCache.Get(t.Context(), hookClientsCacheKey(hookClientsDefaultWindowDays))
	require.NoError(t, err)
	require.True(t, recovered.QueriedAt.Equal(after.QueriedAt))
	require.Equal(t, recovered.WindowDays, after.WindowDays)
	require.Equal(t, recovered.Sessions, after.Sessions)
	require.Equal(t, recovered.Inference, after.Inference)
	require.Equal(t, recovered.DeviceAgents, after.DeviceAgents)
	require.NotEmpty(t, after.DeviceAgents)
	require.True(t, after.QueriedAt.After(before.QueriedAt))
}

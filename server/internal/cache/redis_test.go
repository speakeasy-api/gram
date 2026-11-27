package cache_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisCache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type conditionalTestObject struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (o conditionalTestObject) CacheKey() string   { return o.Key }
func (o conditionalTestObject) TTL() time.Duration { return time.Minute }

func TestRedisLeaseReleaseRequiresOwner(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	adapter := cache.NewRedisCacheAdapter(client)

	acquired, err := adapter.AcquireLease(t.Context(), "lease", "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	released, err := adapter.ReleaseLeaseIfOwner(t.Context(), "lease", "owner-b")
	require.NoError(t, err)
	require.False(t, released)
	require.True(t, mr.Exists("lease"))
	released, err = adapter.ReleaseLeaseIfOwner(t.Context(), "lease", "owner-a")
	require.NoError(t, err)
	require.True(t, released)
	require.False(t, mr.Exists("lease"))
}

func TestTypedCacheObjectStoreIfAbsentPreservesFirstValue(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	objects := cache.NewTypedObjectCache[conditionalTestObject](testenv.NewLogger(t), cache.NewRedisCacheAdapter(client), cache.SuffixNone)

	stored, err := objects.StoreIfAbsent(t.Context(), conditionalTestObject{Key: "outcome", Value: "success"})
	require.NoError(t, err)
	require.True(t, stored)
	stored, err = objects.StoreIfAbsent(t.Context(), conditionalTestObject{Key: "outcome", Value: "failure"})
	require.NoError(t, err)
	require.False(t, stored)

	got, err := objects.Get(t.Context(), "outcome")
	require.NoError(t, err)
	require.Equal(t, "success", got.Value)
}

func TestTypedCacheObjectGetAndDeleteMarking(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	adapter := cache.NewRedisCacheAdapter(client)
	objects := cache.NewTypedObjectCache[conditionalTestObject](testenv.NewLogger(t), adapter, cache.SuffixNone)

	_, err := objects.GetAndDeleteMarking(t.Context(), "missing", "marker-missing", time.Minute)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss)
	require.False(t, mr.Exists("marker-missing"), "nothing was consumed, so nothing is reserved")

	require.NoError(t, objects.Store(t.Context(), conditionalTestObject{Key: "key", Value: "value"}))
	got, err := objects.GetAndDeleteMarking(t.Context(), "key", "marker", time.Minute)
	require.NoError(t, err)
	require.Equal(t, "value", got.Value)

	_, err = objects.Get(t.Context(), "key")
	require.ErrorIs(t, err, redisCache.ErrCacheMiss, "consumed")
	won, err := adapter.Add(t.Context(), "marker", time.Minute)
	require.NoError(t, err)
	require.False(t, won, "the marker was set with the consume")
}

// listEntry is a list element shaped like the structs real callers append.
type listEntry struct {
	Value string
}

// recordedCommand is one command a client sent and whether Redis ran it.
type recordedCommand struct {
	name string
	args []any
	ok   bool
}

// commandRecorder records every command a client sends and its outcome, with
// the commands of a pipeline or transaction recorded one by one. The
// connection handshake (HELLO, CLIENT SETINFO) is not recorded.
type commandRecorder struct {
	mu       sync.Mutex
	commands []recordedCommand
}

func (h *commandRecorder) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *commandRecorder) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		h.record(cmd, err)
		return err
	}
}

func (h *commandRecorder) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		err := next(ctx, cmds)
		for _, cmd := range cmds {
			h.record(cmd, cmd.Err())
		}
		return err
	}
}

func (h *commandRecorder) record(cmd redis.Cmder, err error) {
	if cmd.Name() == "hello" || cmd.Name() == "client" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commands = append(h.commands, recordedCommand{name: cmd.Name(), args: cmd.Args(), ok: err == nil})
}

func (h *commandRecorder) sent() []recordedCommand {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]recordedCommand(nil), h.commands...)
}

func newListAppendAdapter(t *testing.T) (*cache.RedisCacheAdapter, *miniredis.Miniredis, *commandRecorder) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	recorder := &commandRecorder{mu: sync.Mutex{}, commands: nil}
	client.AddHook(recorder)
	return cache.NewRedisCacheAdapter(client), mr, recorder
}

func TestListAppendSetsExpiryInTheSameCommand(t *testing.T) {
	t.Parallel()

	adapter, mr, recorder := newListAppendAdapter(t)

	require.NoError(t, adapter.ListAppend(t.Context(), "list", listEntry{Value: "first"}, time.Hour))

	require.Equal(t, time.Hour, mr.TTL("list"), "a new list gets its expiry")
	// Script.Run tries EVALSHA first and falls back to EVAL when the script is
	// not loaded yet, so one or two commands are sent, but exactly one runs: a
	// script over the list with both the value and the expiry.
	var ran []recordedCommand
	for _, cmd := range recorder.sent() {
		require.Contains(t, []string{"evalsha", "eval"}, cmd.name, "the append and its expiry go to Redis as one script")
		if cmd.ok {
			ran = append(ran, cmd)
		}
	}
	require.Len(t, ran, 1, "exactly one script runs")
	// EVAL script|sha numkeys key value ttl
	require.Len(t, ran[0].args, 6)
	require.Equal(t, 1, ran[0].args[2])
	require.Equal(t, "list", ran[0].args[3])
	require.Equal(t, time.Hour.Milliseconds(), ran[0].args[5], "the expiry travels with the append")

	var got []listEntry
	require.NoError(t, adapter.ListRange(t.Context(), "list", 0, -1, &got))
	require.Equal(t, []listEntry{{Value: "first"}}, got)
}

func TestListAppendKeepsAnExistingExpiry(t *testing.T) {
	t.Parallel()

	adapter, mr, _ := newListAppendAdapter(t)

	require.NoError(t, adapter.ListAppend(t.Context(), "list", listEntry{Value: "first"}, 10*time.Minute))
	mr.FastForward(4 * time.Minute)
	require.NoError(t, adapter.ListAppend(t.Context(), "list", listEntry{Value: "second"}, time.Hour))

	require.Equal(t, 6*time.Minute, mr.TTL("list"), "appending does not extend the list's life")

	var got []listEntry
	require.NoError(t, adapter.ListRange(t.Context(), "list", 0, -1, &got))
	require.Equal(t, []listEntry{{Value: "first"}, {Value: "second"}}, got)
}

func TestListAppendGivesAListWithoutExpiryOne(t *testing.T) {
	t.Parallel()

	adapter, mr, _ := newListAppendAdapter(t)

	_, err := mr.Push("list", "written-without-expiry")
	require.NoError(t, err)
	require.Zero(t, mr.TTL("list"))

	require.NoError(t, adapter.ListAppend(t.Context(), "list", "next", time.Hour))
	require.Equal(t, time.Hour, mr.TTL("list"))
}

func TestListAppendWithoutTTLSetsNoExpiry(t *testing.T) {
	t.Parallel()

	adapter, mr, _ := newListAppendAdapter(t)

	require.NoError(t, adapter.ListAppend(t.Context(), "list", "first", 0))
	require.True(t, mr.Exists("list"))
	require.Zero(t, mr.TTL("list"))
}

func TestListAppendRoundsASubMillisecondTTLUp(t *testing.T) {
	t.Parallel()

	adapter, mr, _ := newListAppendAdapter(t)

	require.NoError(t, adapter.ListAppend(t.Context(), "list", "first", time.Microsecond))
	require.True(t, mr.Exists("list"), "a tiny lifetime must not delete the list it was meant to expire")
	require.Equal(t, time.Millisecond, mr.TTL("list"))
}

func TestTypedCacheObjectInvalidMarkerLifetimePreservesValue(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	objects := cache.NewTypedObjectCache[conditionalTestObject](testenv.NewLogger(t), cache.NewRedisCacheAdapter(client), cache.SuffixNone)
	require.NoError(t, objects.Store(t.Context(), conditionalTestObject{Key: "key", Value: "value"}))
	for _, ttl := range []time.Duration{-time.Second, 0, time.Microsecond} {
		_, err := objects.GetAndDeleteMarking(t.Context(), "key", "marker", ttl)
		require.ErrorContains(t, err, "at least one millisecond")
		got, err := objects.Get(t.Context(), "key")
		require.NoError(t, err)
		require.Equal(t, "value", got.Value)
		require.False(t, mr.Exists("marker"))
	}
}

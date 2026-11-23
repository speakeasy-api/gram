package chat_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/redisrole"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// splitTurnStreamTimeout bounds the subscribe-publish round trip; miniredis
// answers in microseconds, so this only stops a broken test from hanging.
const splitTurnStreamTimeout = 10 * time.Second

// TestTurnStreamOnEvictableInstanceWritesOnlyEvictableKeys runs publish,
// subscribe (XREAD), replay and reset on an evictable instance whose client
// refuses any declared sessions-role key, then checks that every key left on
// the instance classifies as evictable.
func TestTurnStreamOnEvictableInstanceWritesOnlyEvictableKeys(t *testing.T) {
	t.Parallel()

	evictableRedis := miniredis.RunT(t)
	guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), redisrole.GuardEnforce)
	evictableClient := redis.NewClient(&redis.Options{Addr: evictableRedis.Addr()})
	t.Cleanup(func() { require.NoError(t, evictableClient.Close()) })
	evictableClient.AddHook(guard.Hook(redisrole.RoleEvictable))

	stream := chat.NewTurnStream(redisrole.EvictableClient{Client: evictableClient})
	chatID := uuid.New()

	ctx, cancel := context.WithTimeout(t.Context(), splitTurnStreamTimeout)
	defer cancel()

	frames, err := stream.Subscribe(ctx, chatID, "")
	require.NoError(t, err)

	_, err = stream.Publish(ctx, chatID, chat.TurnFrame{Kind: chat.TurnFrameText, Text: "hello"})
	require.NoError(t, err)
	_, err = stream.Publish(ctx, chatID, chat.TurnFrame{Kind: chat.TurnFrameDone})
	require.NoError(t, err)

	var received []chat.TurnFrameKind
	for frame := range frames {
		received = append(received, frame.Kind)
	}
	require.Equal(t, []chat.TurnFrameKind{chat.TurnFrameText, chat.TurnFrameDone}, received)

	replayed, err := stream.Replay(ctx, chatID, "")
	require.NoError(t, err)
	require.Len(t, replayed, 2)
	require.NoError(t, stream.Reset(ctx, chatID))

	keys := evictableRedis.Keys()
	require.NotEmpty(t, keys)
	for _, key := range keys {
		role, _ := redisrole.Of(key)
		require.Equal(t, redisrole.RoleEvictable, role, "key %q", key)
	}
}

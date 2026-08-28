package sessions_test

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAuthenticateSessionCacheFailureIsUnexpected(t *testing.T) {
	t.Parallel()

	redisClient := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() {
		require.NoError(t, redisClient.Close())
	})
	manager := sessions.NewManager(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		nil,
		redisClient,
		cache.SuffixNone,
		nil,
		nil,
		nil,
	)

	_, err := manager.Authenticate(t.Context(), "session-id")
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.NotErrorIs(t, err, redis.Nil)
}

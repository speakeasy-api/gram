package admin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

func TestSessionTokenRefreshRestartsInactivityTimeout(t *testing.T) {
	t.Parallel()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	store := NewSessionStore(cache.NewTypedObjectCache[Session](nil, cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone), enc)
	sessionID, err := store.Store(t.Context(), StoreParams{
		Email: "staff@example.test", Name: "Staff", OIDCSubject: "staff-subject", HD: testAdminHD,
		AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	key := AdminSessionCacheKey(sessionID) + ":" + string(cache.SuffixNone)
	require.NoError(t, redisClient.Expire(t.Context(), key, time.Hour).Err())
	session, err := store.Get(t.Context(), sessionID)
	require.NoError(t, err)

	_, err = store.UpdateTokens(t.Context(), session, "new-access", "new-refresh", time.Now().Add(time.Hour))
	require.NoError(t, err)

	remaining, err := redisClient.PTTL(t.Context(), key).Result()
	require.NoError(t, err)
	require.Greater(t, remaining, adminSessionTTL-time.Minute)
}

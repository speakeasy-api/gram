package admin

import (
	"net/http"
	"sync/atomic"
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

func TestSessionTTLStopsAtMaximumLifetime(t *testing.T) {
	t.Parallel()
	require.Equal(t, adminSessionTTL, Session{CreatedAt: time.Now()}.TTL())

	nearLimit := Session{CreatedAt: time.Now().Add(time.Hour - adminSessionMaxLifetime)}.TTL()
	require.Greater(t, nearLimit, time.Hour-time.Minute)
	require.LessOrEqual(t, nearLimit, time.Hour)

	require.Equal(t, adminSessionExpiredTTL, Session{CreatedAt: time.Now().Add(-adminSessionMaxLifetime)}.TTL())
	require.Equal(t, adminSessionExpiredTTL, Session{}.TTL())
}

func TestSessionExpired(t *testing.T) {
	t.Parallel()
	now := time.Now()
	require.False(t, Session{CreatedAt: now}.Expired(now))
	require.False(t, Session{CreatedAt: now.Add(time.Minute - adminSessionMaxLifetime)}.Expired(now))
	require.True(t, Session{CreatedAt: now.Add(-adminSessionMaxLifetime)}.Expired(now))
	require.True(t, Session{}.Expired(now))
}

func TestHandleGetSession_RejectsSessionPastMaximumLifetime(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	var userinfoCalls atomic.Int32
	svc := newTestSessionService(t, newTestOIDCClient(t, func(w http.ResponseWriter, _ *http.Request) {
		userinfoCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	sessionID, err := svc.sessions.Store(ctx, StoreParams{
		Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-aged", HD: testAdminHD,
		AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	session, err := svc.sessions.Get(ctx, sessionID)
	require.NoError(t, err)
	session.CreatedAt = time.Now().Add(-adminSessionMaxLifetime)
	require.NoError(t, svc.sessions.cache.Store(ctx, session))

	require.Equal(t, http.StatusUnauthorized, callSessionGet(t, svc, sessionID).Code)

	require.Zero(t, userinfoCalls.Load())
	_, err = svc.sessions.Get(ctx, sessionID)
	require.Error(t, err, "a session past its maximum lifetime must be deleted")
}

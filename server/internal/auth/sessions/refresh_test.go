package sessions

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisCache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type emptyIdentity struct{}

func (emptyIdentity) HasAccessToOrganization(context.Context, string, string) (*Organization, string, bool) {
	return nil, "", false
}
func (emptyIdentity) IsAdmin(context.Context, string) bool { return false }
func (emptyIdentity) GetUserInfo(context.Context, string) (*CachedUserInfo, bool, error) {
	return &CachedUserInfo{}, true, nil
}
func (emptyIdentity) InvalidateUserInfoCache(context.Context, string) error { return nil }

func newRefreshManager(t *testing.T) (*Manager, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	// testenv imports sessions; use a test-scoped logger here to avoid that import cycle.
	return &Manager{redis: client, logger: slog.New(slog.NewTextHandler(t.Output(), nil)), identity: emptyIdentity{}}, mr
}

func createBrowserSession(t *testing.T, manager *Manager, session Session) (string, Session) {
	t.Helper()
	require.NoError(t, manager.StoreSession(t.Context(), session))
	secret, stored, err := manager.CreateRefreshSession(t.Context(), session.SessionID)
	require.NoError(t, err)
	return secret, stored
}

func TestAccessFixedExpiryAndRefreshSecretIsolation(t *testing.T) {
	t.Parallel()
	manager, mr := newRefreshManager(t)
	secret, session := createBrowserSession(t, manager, Session{SessionID: "access", UserID: "user"})
	require.NotEqual(t, session.SessionID, secret)
	require.Len(t, session.RefreshHash, 64)
	for _, key := range mr.Keys() {
		require.NotContains(t, key, secret)
		value, err := mr.Get(key)
		require.NoError(t, err)
		require.NotContains(t, value, secret)
	}
	require.InDelta(t, AccessLifetime, mr.TTL(manager.accessKey(session.SessionID)), float64(time.Second))
	mr.FastForward(9 * time.Minute)
	accessTTL := mr.TTL(manager.accessKey(session.SessionID))
	refreshTTL := mr.TTL(manager.refreshKey(session.RefreshHash))
	for range 3 {
		_, err := manager.Authenticate(t.Context(), session.SessionID)
		require.NoError(t, err)
	}
	require.Equal(t, accessTTL, mr.TTL(manager.accessKey(session.SessionID)))
	require.Equal(t, refreshTTL, mr.TTL(manager.refreshKey(session.RefreshHash)))
	_, err := manager.Authenticate(t.Context(), secret)
	require.Error(t, err)
	require.NoError(t, mr.Set("sessions:v2:legacy:", `{}`))
	_, err = manager.Authenticate(t.Context(), "legacy")
	require.Error(t, err)
	mr.FastForward(time.Minute + time.Second)
	_, err = manager.Authenticate(t.Context(), session.SessionID)
	require.Error(t, err)
	renewed, err := manager.Refresh(t.Context(), secret)
	require.NoError(t, err)
	require.NotEqual(t, session.SessionID, renewed.SessionID)
	require.InDelta(t, AccessLifetime, mr.TTL(manager.accessKey(renewed.SessionID)), float64(time.Second))
	require.InDelta(t, RefreshIdleLifetime, mr.TTL(manager.refreshKey(session.RefreshHash)), float64(time.Second))
}

func TestScopeUpdatePreservesAccessAndRefreshDeadlines(t *testing.T) {
	t.Parallel()
	manager, mr := newRefreshManager(t)
	secret, session := createBrowserSession(t, manager, Session{SessionID: "access", ActiveOrganizationID: "org", SupportOrganizationID: "org", SupportExpiresAt: time.Now().Add(30 * time.Minute)})
	mr.FastForward(time.Minute)
	refreshTTL := mr.TTL(manager.refreshKey(session.RefreshHash))
	accessTTL := mr.TTL(manager.accessKey(session.SessionID))
	changed := session
	changed.ActiveOrganizationID = "demo"
	changed.SupportOrganizationID = ""
	changed.SupportExpiresAt = time.Time{} // Must not escape the support deadline.
	changed.ExpiresAt = time.Now().Add(time.Hour)
	require.NoError(t, manager.UpdateSession(t.Context(), session, changed))
	stored, err := manager.GetSession(t.Context(), session.SessionID)
	require.NoError(t, err)
	require.True(t, session.SupportExpiresAt.Equal(stored.SupportExpiresAt))
	require.True(t, session.ExpiresAt.Equal(stored.ExpiresAt))
	require.Equal(t, refreshTTL, mr.TTL(manager.refreshKey(session.RefreshHash)))
	require.Equal(t, accessTTL, mr.TTL(manager.accessKey(session.SessionID)))
	refreshed, err := manager.Refresh(t.Context(), secret)
	require.NoError(t, err)
	require.Equal(t, accessTTL, mr.TTL(manager.accessKey(session.SessionID)))
	require.Equal(t, "demo", refreshed.ActiveOrganizationID)
	require.Empty(t, refreshed.SupportOrganizationID)
	require.True(t, session.SupportExpiresAt.Equal(refreshed.SupportExpiresAt))
	require.NotEqual(t, session.SessionID, refreshed.SessionID)
	require.True(t, refreshed.ExpiresAt.After(session.ExpiresAt))
	require.LessOrEqual(t, mr.TTL(manager.refreshKey(session.RefreshHash)), 30*time.Minute)
	require.Error(t, manager.UpdateSession(t.Context(), session, changed), "stale scope update must lose")
}

func TestLogoutRevokesRefreshAfterAccessExpires(t *testing.T) {
	t.Parallel()
	manager, mr := newRefreshManager(t)
	secret, session := createBrowserSession(t, manager, Session{SessionID: "access"})
	mr.FastForward(AccessLifetime + time.Second)
	require.NoError(t, manager.Logout(t.Context(), secret, session.SessionID))
	_, err := manager.Refresh(t.Context(), secret)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss)
	require.NoError(t, manager.Logout(t.Context(), secret, session.SessionID))
	require.Error(t, manager.UpdateSession(t.Context(), session, session))
	require.Empty(t, mr.Keys())
}

func TestRefreshIdleExpiryAndSupportCap(t *testing.T) {
	t.Parallel()
	for _, support := range []bool{false, true} {
		manager, mr := newRefreshManager(t)
		initial := Session{SessionID: "access"}
		lifetime := RefreshIdleLifetime
		if support {
			initial.SupportExpiresAt = time.Now().Add(time.Minute)
			lifetime = time.Minute
		}
		secret, session := createBrowserSession(t, manager, initial)
		if support {
			require.LessOrEqual(t, mr.TTL(manager.accessKey(session.SessionID)), time.Minute)
			require.LessOrEqual(t, mr.TTL(manager.refreshKey(session.RefreshHash)), time.Minute)
		}
		mr.FastForward(lifetime + time.Second)
		_, err := manager.Refresh(t.Context(), secret)
		require.ErrorIs(t, err, redisCache.ErrCacheMiss)
	}
}

func TestConcurrentRefreshScopeAndLogoutNeverResurrect(t *testing.T) {
	t.Parallel()
	for range 20 {
		manager, mr := newRefreshManager(t)
		secret, session := createBrowserSession(t, manager, Session{SessionID: "access", ActiveOrganizationID: "before"})
		changed := session
		changed.ActiveOrganizationID = "after"
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Go(func() { <-start; _, _ = manager.Refresh(t.Context(), secret) })
		wg.Go(func() { <-start; _ = manager.UpdateSession(t.Context(), session, changed) })
		var logoutErr error
		wg.Go(func() { <-start; logoutErr = manager.Logout(t.Context(), secret, session.SessionID) })
		close(start)
		wg.Wait()
		require.NoError(t, logoutErr)
		_, err := manager.Refresh(t.Context(), secret)
		require.ErrorIs(t, err, redisCache.ErrCacheMiss)
		require.Empty(t, mr.Keys())
	}
}

func TestLogoutStillClearsAccessWhenRefreshStateIsMissing(t *testing.T) {
	t.Parallel()
	manager, mr := newRefreshManager(t)
	secret, session := createBrowserSession(t, manager, Session{SessionID: "access"})
	mr.Del(manager.refreshKey(session.RefreshHash))
	require.NoError(t, manager.Logout(t.Context(), secret, session.SessionID))
	require.Empty(t, mr.Keys())
}

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

// These helpers are intentionally independent of the in-progress refresh tests.
type overlapRefreshIdentity struct{}

func (overlapRefreshIdentity) HasAccessToOrganization(context.Context, string, string) (*Organization, string, bool) {
	return nil, "", false
}
func (overlapRefreshIdentity) IsAdmin(context.Context, string) bool { return false }
func (overlapRefreshIdentity) GetUserInfo(context.Context, string) (*CachedUserInfo, bool, error) {
	return &CachedUserInfo{}, true, nil
}
func (overlapRefreshIdentity) InvalidateUserInfoCache(context.Context, string) error { return nil }

func overlapRefreshManager(t *testing.T, mr *miniredis.Miniredis) *Manager {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	// testenv imports sessions; use a test-scoped logger here to avoid that import cycle.
	return &Manager{redis: client, logger: slog.New(slog.NewTextHandler(t.Output(), nil)), identity: overlapRefreshIdentity{}}
}

func overlapRefreshSession(t *testing.T, manager *Manager, session Session) (string, Session) {
	t.Helper()
	require.NoError(t, manager.StoreSession(t.Context(), session))
	secret, stored, err := manager.CreateRefreshSession(t.Context(), session.SessionID)
	require.NoError(t, err)
	return secret, stored
}

func TestRefreshAlwaysMintsFixedLifetimeAccess(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	manager := overlapRefreshManager(t, mr)
	secret, original := overlapRefreshSession(t, manager, Session{SessionID: "original", UserID: "user"})
	originalKey := manager.accessKey(original.SessionID)
	originalData, err := mr.Get(originalKey)
	require.NoError(t, err)
	mr.FastForward(9 * time.Minute)
	originalTTL := mr.TTL(originalKey)
	seen := map[string]bool{original.SessionID: true}
	var latest Session
	for range 3 {
		started := time.Now()
		latest, err = manager.Refresh(t.Context(), secret)
		require.NoError(t, err)
		require.False(t, seen[latest.SessionID], "every refresh must mint a distinct ID")
		seen[latest.SessionID] = true
		require.False(t, latest.ExpiresAt.Before(started.Add(AccessLifetime)))
		require.False(t, latest.ExpiresAt.After(time.Now().Add(AccessLifetime)))
		require.InDelta(t, AccessLifetime, mr.TTL(manager.accessKey(latest.SessionID)), float64(time.Second))
		require.Equal(t, originalTTL, mr.TTL(originalKey))
		raw, err := mr.Get(originalKey)
		require.NoError(t, err)
		require.Equal(t, originalData, raw)
		presented, err := manager.GetSession(t.Context(), original.SessionID)
		require.NoError(t, err)
		require.True(t, presented.ExpiresAt.Equal(original.ExpiresAt))
	}

	accessTTL := mr.TTL(manager.accessKey(latest.SessionID))
	idleTTL := mr.TTL(manager.refreshKey(original.RefreshHash))
	for range 3 {
		_, err := manager.Authenticate(t.Context(), latest.SessionID)
		require.NoError(t, err)
	}
	require.Equal(t, accessTTL, mr.TTL(manager.accessKey(latest.SessionID)))
	require.Equal(t, idleTTL, mr.TTL(manager.refreshKey(original.RefreshHash)))
	mr.FastForward(originalTTL + time.Second)
	_, err = manager.GetSession(t.Context(), original.SessionID)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss)
	_, err = manager.Authenticate(t.Context(), latest.SessionID)
	require.NoError(t, err, "new access remains usable after the captured original expires")
}

func TestOverlappingAccessUpdatesAuthoritativeScope(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	manager := overlapRefreshManager(t, mr)
	secret, original := overlapRefreshSession(t, manager, Session{SessionID: "original", ActiveOrganizationID: "before"})
	latest, err := manager.Refresh(t.Context(), secret)
	require.NoError(t, err)
	originalTTL := mr.TTL(manager.accessKey(original.SessionID))
	latestTTL := mr.TTL(manager.accessKey(latest.SessionID))
	familyTTL := mr.TTL(manager.refreshKey(original.RefreshHash))
	changed := original
	changed.ActiveOrganizationID = "after"
	require.NoError(t, manager.UpdateSession(t.Context(), original, changed))
	for _, access := range []Session{original, latest} {
		got, err := manager.GetSession(t.Context(), access.SessionID)
		require.NoError(t, err)
		require.Equal(t, "after", got.ActiveOrganizationID)
		require.Equal(t, access.SessionID, got.SessionID)
		require.True(t, access.ExpiresAt.Equal(got.ExpiresAt))
	}
	family, err := readSession(t.Context(), manager.redis, manager.refreshKey(original.RefreshHash))
	require.NoError(t, err)
	require.Equal(t, latest.SessionID, family.SessionID)
	require.True(t, latest.ExpiresAt.Equal(family.ExpiresAt))
	require.Equal(t, originalTTL, mr.TTL(manager.accessKey(original.SessionID)))
	require.Equal(t, latestTTL, mr.TTL(manager.accessKey(latest.SessionID)))
	require.Equal(t, familyTTL, mr.TTL(manager.refreshKey(original.RefreshHash)))
	require.ErrorIs(t, manager.UpdateSession(t.Context(), original, changed), redisCache.ErrCacheMiss)

	// The latest token's raw access record still says "before". Its projected
	// expected state must nevertheless be accepted for the next scope change.
	expected, err := manager.GetSession(t.Context(), latest.SessionID)
	require.NoError(t, err)
	changed = expected
	changed.ActiveOrganizationID = "final"
	require.NoError(t, manager.UpdateSession(t.Context(), expected, changed))
	got, err := manager.GetSession(t.Context(), original.SessionID)
	require.NoError(t, err)
	require.Equal(t, "final", got.ActiveOrganizationID)
	renewed, err := manager.Refresh(t.Context(), secret)
	require.NoError(t, err)
	require.Equal(t, "final", renewed.ActiveOrganizationID)
}

func TestLogoutOldAccessRevokesEveryOverlap(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	manager := overlapRefreshManager(t, mr)
	secret, original := overlapRefreshSession(t, manager, Session{SessionID: "original"})
	middle, err := manager.Refresh(t.Context(), secret)
	require.NoError(t, err)
	latest, err := manager.Refresh(t.Context(), secret)
	require.NoError(t, err)
	require.NoError(t, manager.Logout(t.Context(), secret, original.SessionID))
	require.False(t, mr.Exists(manager.refreshKey(original.RefreshHash)))
	require.False(t, mr.Exists(manager.accessKey(original.SessionID)), "presented ID must be explicitly deleted")
	require.False(t, mr.Exists(manager.accessKey(latest.SessionID)))
	require.True(t, mr.Exists(manager.accessKey(middle.SessionID)), "unpresented overlap may remain only as an inert expiring record")
	for _, access := range []Session{original, middle, latest} {
		_, err := manager.GetSession(t.Context(), access.SessionID)
		require.ErrorIs(t, err, redisCache.ErrCacheMiss)
		_, err = manager.Authenticate(t.Context(), access.SessionID)
		require.Error(t, err)
	}
	_, err = manager.Refresh(t.Context(), secret)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss)
	require.ErrorIs(t, manager.UpdateSession(t.Context(), middle, middle), redisCache.ErrCacheMiss)
	// This must raw-read and delete even though GetSession now rejects it.
	require.NoError(t, manager.ClearSession(t.Context(), Session{SessionID: middle.SessionID}))
	require.False(t, mr.Exists(manager.accessKey(middle.SessionID)))
	require.NoError(t, manager.Logout(t.Context(), secret, original.SessionID))
}

func TestSupportCapSurvivesOverlapAndScopeChange(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	manager := overlapRefreshManager(t, mr)
	deadline := time.Now().Add(5 * time.Minute)
	secret, original := overlapRefreshSession(t, manager, Session{SessionID: "original", ActiveOrganizationID: "support", SupportOrganizationID: "support", SupportExpiresAt: deadline})
	latest, err := manager.Refresh(t.Context(), secret)
	require.NoError(t, err)
	changed := original
	changed.ActiveOrganizationID = "demo"
	changed.SupportOrganizationID = ""
	changed.SupportExpiresAt = time.Time{}
	changed.ExpiresAt = time.Now().Add(time.Hour)
	require.NoError(t, manager.UpdateSession(t.Context(), original, changed))
	for _, access := range []Session{original, latest} {
		got, err := manager.GetSession(t.Context(), access.SessionID)
		require.NoError(t, err)
		require.Equal(t, "demo", got.ActiveOrganizationID)
		require.Empty(t, got.SupportOrganizationID)
		require.True(t, deadline.Equal(got.SupportExpiresAt))
		require.True(t, deadline.Equal(got.ExpiresAt))
	}
	renewed, err := manager.Refresh(t.Context(), secret)
	require.NoError(t, err)
	require.NotEqual(t, latest.SessionID, renewed.SessionID)
	require.True(t, deadline.Equal(renewed.ExpiresAt))
	require.True(t, deadline.Equal(renewed.SupportExpiresAt))
	require.LessOrEqual(t, mr.TTL(manager.refreshKey(original.RefreshHash)), 5*time.Minute)
}

func TestTwoManagersConcurrentRefreshScopeLogout(t *testing.T) {
	t.Parallel()
	for range 20 {
		mr := miniredis.RunT(t)
		first := overlapRefreshManager(t, mr)
		second := overlapRefreshManager(t, mr)
		secret, original := overlapRefreshSession(t, first, Session{SessionID: "original", ActiveOrganizationID: "before"})
		overlap, err := second.Refresh(t.Context(), secret)
		require.NoError(t, err)
		changed := original
		changed.ActiveOrganizationID = "after"
		var wg sync.WaitGroup
		start := make(chan struct{})
		var renewed [2]Session
		var refreshErr [2]error
		var scopeErr, logoutErr error
		wg.Add(4)
		go func() { defer wg.Done(); <-start; renewed[0], refreshErr[0] = first.Refresh(t.Context(), secret) }()
		go func() { defer wg.Done(); <-start; renewed[1], refreshErr[1] = second.Refresh(t.Context(), secret) }()
		go func() { defer wg.Done(); <-start; scopeErr = first.UpdateSession(t.Context(), original, changed) }()
		go func() { defer wg.Done(); <-start; logoutErr = second.Logout(t.Context(), secret, original.SessionID) }()
		close(start)
		wg.Wait()
		require.NoError(t, logoutErr)
		if scopeErr != nil {
			require.ErrorIs(t, scopeErr, redisCache.ErrCacheMiss)
		}
		accessIDs := []string{original.SessionID, overlap.SessionID}
		for i, err := range refreshErr {
			if err != nil {
				require.ErrorIs(t, err, redisCache.ErrCacheMiss)
			} else {
				accessIDs = append(accessIDs, renewed[i].SessionID)
			}
		}
		require.False(t, mr.Exists(first.refreshKey(original.RefreshHash)))
		require.False(t, mr.Exists(first.accessKey(original.SessionID)))
		for _, manager := range []*Manager{first, second} {
			for _, id := range accessIDs {
				_, err := manager.GetSession(t.Context(), id)
				require.ErrorIs(t, err, redisCache.ErrCacheMiss)
				_, err = manager.Authenticate(t.Context(), id)
				require.Error(t, err)
			}
			_, err := manager.Refresh(t.Context(), secret)
			require.ErrorIs(t, err, redisCache.ErrCacheMiss)
		}
	}
}

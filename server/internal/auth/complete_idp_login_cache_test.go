package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// Only user-info writes fail; reads and sync's cache invalidation still use Redis.
type failingUserInfoSetCache struct {
	cache.Cache
	failedSets int
}

func (c *failingUserInfoSetCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if strings.HasPrefix(key, "userInfo:") {
		c.failedSets++
		return errors.New("user-info SET unavailable")
	}
	return c.Cache.Set(ctx, key, value, ttl)
}

func TestCompleteIDPLogin_MembershipGateIgnoresStaleCacheRepopulation(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		beforeCount int
		afterCount  int
	}{
		{name: "stale negative after import", beforeCount: 0, afterCount: 1},
		{name: "stale positive after revocation", beforeCount: 1, afterCount: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fetcher := memberFetcher()
			ctx, inst, idpUser, gramUserID := newIDPLoginInstance(t, fetcher)
			logger := testenv.NewLogger(t)
			redisClient, err := infra.NewRedisClient(t, 0)
			require.NoError(t, err)
			backingCache := cache.NewRedisCacheAdapter(redisClient)
			suffix := testenv.NewCacheSuffix(t, cache.Suffix("login-cache-repopulation"))
			userInfoCache := cache.NewTypedObjectCache[sessions.CachedUserInfo](logger, backingCache, suffix)
			pylonClient, err := pylon.NewPylon(logger, "")
			require.NoError(t, err)
			resolver := identity.NewResolver(logger, testenv.NewTracerProvider(t), backingCache,
				"", "", nil, fetcher, orgRepo.New(inst.conn), userRepo.New(inst.conn), pylonClient,
				posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", ""), nil, suffix)

			if tt.beforeCount == 1 {
				require.NoError(t, resolver.SyncMembershipsFromWorkOS(ctx, gramUserID, idpLoginWorkosUserID))
				fetcher.members[idpLoginWorkosUserID] = nil
			}

			// Capture the DB snapshot a cache-miss reader would hold before sync.
			stale, err := resolver.BuildUserInfoFromDB(ctx, gramUserID)
			require.NoError(t, err)
			require.Len(t, stale.Organizations, tt.beforeCount)

			login, err := resolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{})
			require.NoError(t, err)
			require.Equal(t, gramUserID, login.UserID)

			// A delayed cache-miss writer finishes after login's invalidation and refresh.
			require.NoError(t, userInfoCache.Store(ctx, *stale))
			cached, hit, err := resolver.GetUserInfo(ctx, gramUserID)
			require.NoError(t, err)
			require.True(t, hit, "the real resolver must read the repopulated stale cache")
			require.Equal(t, stale, cached)
			require.Len(t, cached.Organizations, tt.beforeCount)

			require.NotNil(t, login.UserInfo)
			require.Len(t, login.UserInfo.Organizations, tt.afterCount, "login retains the fresh post-sync snapshot")
			if tt.afterCount == 1 {
				require.Equal(t, idpLoginGramOrgID, login.UserInfo.Organizations[0].ID)
			}
			member, err := resolver.IsOrganizationMember(ctx, idpLoginGramOrgID, gramUserID)
			require.NoError(t, err)
			require.Equal(t, tt.afterCount == 1, member, "the DB membership gate must ignore stale cache repopulation")
		})
	}
}

func TestCompleteIDPLogin_UserInfoCacheWriteFailureIsBestEffort(t *testing.T) {
	t.Parallel()

	fetcher := memberFetcher()
	ctx, inst, idpUser, gramUserID := newIDPLoginInstance(t, fetcher)
	logger := testenv.NewLogger(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	backingCache := cache.NewRedisCacheAdapter(redisClient)
	suffix := testenv.NewCacheSuffix(t, cache.Suffix("login-cache-failure"))
	userInfoCache := cache.NewTypedObjectCache[sessions.CachedUserInfo](logger, backingCache, suffix)
	stale, err := inst.identityResolver.BuildUserInfoFromDB(ctx, gramUserID)
	require.NoError(t, err)
	require.Empty(t, stale.Organizations)
	require.NoError(t, userInfoCache.Store(ctx, *stale))
	_, err = userInfoCache.Get(ctx, sessions.UserInfoCacheKey(gramUserID))
	require.NoError(t, err, "stale user info is cached before login")

	failingCache := &failingUserInfoSetCache{Cache: backingCache}
	pylonClient, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)
	resolver := identity.NewResolver(logger, testenv.NewTracerProvider(t), failingCache,
		"", "", nil, fetcher, orgRepo.New(inst.conn), userRepo.New(inst.conn), pylonClient,
		posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", ""), nil, suffix)

	login, err := resolver.CompleteIDPLogin(ctx, idpUser, identity.IDPLoginOptions{})
	require.Equal(t, 1, failingCache.failedSets, "login attempted to cache fresh user info")
	require.NoError(t, err, "a cache write failure must not fail login")
	require.Equal(t, gramUserID, login.UserID)
	require.NotNil(t, login.UserInfo)
	require.Len(t, login.UserInfo.Organizations, 1)
	require.Equal(t, idpLoginGramOrgID, login.UserInfo.Organizations[0].ID)
	member, err := resolver.IsOrganizationMember(ctx, idpLoginGramOrgID, gramUserID)
	require.NoError(t, err)
	require.True(t, member, "membership sync persisted the fresh membership")
	_, err = userInfoCache.Get(ctx, sessions.UserInfoCacheKey(gramUserID))
	require.Error(t, err, "sync removed stale cache despite fresh cache write failing")
}

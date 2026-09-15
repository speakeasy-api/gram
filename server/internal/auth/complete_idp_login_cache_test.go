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

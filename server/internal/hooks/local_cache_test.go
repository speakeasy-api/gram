package hooks

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestLocalSessionCache_PreservesConditionalWrites(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)
	localCache := NewLocalSessionCache(cache.NewRedisCacheAdapter(ti.redisClient), ti.conn)
	conditional, ok := localCache.(cache.ConditionalCache)
	require.True(t, ok)

	key := "conditional-key:" + uuid.NewString()
	stored, err := conditional.SetIfAbsent(t.Context(), key, "first", time.Minute)
	require.NoError(t, err)
	require.True(t, stored)
	stored, err = conditional.SetIfAbsent(t.Context(), key, "second", time.Minute)
	require.NoError(t, err)
	require.False(t, stored)
	var value string
	require.NoError(t, localCache.Get(t.Context(), key, &value))
	require.Equal(t, "first", value)
}

func TestLocalSessionCache_PreservesLeases(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)
	localCache := NewLocalSessionCache(cache.NewRedisCacheAdapter(ti.redisClient), ti.conn)
	leases, ok := localCache.(cache.LeaseCache)
	require.True(t, ok)

	key := "lease-key:" + uuid.NewString()
	acquired, err := leases.AcquireLease(t.Context(), key, "owner-1", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = leases.AcquireLease(t.Context(), key, "owner-2", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired)
	released, err := leases.ReleaseLeaseIfOwner(t.Context(), key, "owner-2")
	require.NoError(t, err)
	require.False(t, released)
	released, err = leases.ReleaseLeaseIfOwner(t.Context(), key, "owner-1")
	require.NoError(t, err)
	require.True(t, released)
}

func TestLocalSessionCache_FallbackIncludesOrganizationUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	require.NotNil(t, authCtx.ProjectID)

	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, *authCtx.Email)

	localCache := NewLocalSessionCache(cache.NewRedisCacheAdapter(ti.redisClient), ti.conn)
	var metadata SessionMetadata
	require.NoError(t, localCache.Get(ctx, sessionCacheKey(uuid.NewString()), &metadata))

	assert.Equal(t, authCtx.UserID, metadata.UserID)
	assert.Equal(t, *authCtx.Email, metadata.UserEmail)
	assert.Equal(t, authCtx.ActiveOrganizationID, metadata.GramOrgID)
	assert.Equal(t, authCtx.ProjectID.String(), metadata.ProjectID)
}

func TestLocalSessionCache_FallbackUsesAuthContextProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	otherProject, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           "local-cache-target",
		Slug:           "local-cache-target",
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	nextAuthCtx := *authCtx
	nextAuthCtx.ProjectID = &otherProject.ID
	nextAuthCtx.ProjectSlug = &otherProject.Slug
	ctx = contextvalues.SetAuthContext(ctx, &nextAuthCtx)

	localCache := NewLocalSessionCache(cache.NewRedisCacheAdapter(ti.redisClient), ti.conn)
	var metadata SessionMetadata
	require.NoError(t, localCache.Get(ctx, sessionCacheKey(uuid.NewString()), &metadata))

	assert.Equal(t, otherProject.ID.String(), metadata.ProjectID)
	assert.Equal(t, authCtx.ActiveOrganizationID, metadata.GramOrgID)
}

func TestLocalSessionCache_FallbackRequiresAuthContextProject(t *testing.T) {
	t.Parallel()

	_, ti := newTestHooksService(t)

	localCache := NewLocalSessionCache(cache.NewRedisCacheAdapter(ti.redisClient), ti.conn)
	var metadata SessionMetadata
	require.Error(t, localCache.Get(t.Context(), sessionCacheKey(uuid.NewString()), &metadata))
}

func TestLocalSessionCache_FallbackDoesNotUseOtherProjectWhenAuthProjectMissing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	missingProjectID := uuid.New()
	nextAuthCtx := *authCtx
	nextAuthCtx.ProjectID = &missingProjectID
	ctx = contextvalues.SetAuthContext(ctx, &nextAuthCtx)

	localCache := NewLocalSessionCache(cache.NewRedisCacheAdapter(ti.redisClient), ti.conn)
	var metadata SessionMetadata
	require.Error(t, localCache.Get(ctx, sessionCacheKey(uuid.NewString()), &metadata))
}

func TestLocalSessionCache_EnrichesCachedMetadataMissingUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	require.NotNil(t, authCtx.ProjectID)

	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, *authCtx.Email)

	localCache := NewLocalSessionCache(cache.NewRedisCacheAdapter(ti.redisClient), ti.conn)
	sessionID := uuid.NewString()
	require.NoError(t, localCache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     *authCtx.Email,
		ExternalOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}, time.Hour))

	var metadata SessionMetadata
	require.NoError(t, localCache.Get(ctx, sessionCacheKey(sessionID), &metadata))

	assert.Equal(t, authCtx.UserID, metadata.UserID)
	assert.Equal(t, *authCtx.Email, metadata.UserEmail)
	assert.Equal(t, authCtx.ActiveOrganizationID, metadata.GramOrgID)
	assert.Equal(t, authCtx.ProjectID.String(), metadata.ProjectID)
}

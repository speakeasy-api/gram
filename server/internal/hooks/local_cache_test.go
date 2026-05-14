package hooks

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

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
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   *authCtx.Email,
		ClaudeOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}, time.Hour))

	var metadata SessionMetadata
	require.NoError(t, localCache.Get(ctx, sessionCacheKey(sessionID), &metadata))

	assert.Equal(t, authCtx.UserID, metadata.UserID)
	assert.Equal(t, *authCtx.Email, metadata.UserEmail)
	assert.Equal(t, authCtx.ActiveOrganizationID, metadata.GramOrgID)
	assert.Equal(t, authCtx.ProjectID.String(), metadata.ProjectID)
}

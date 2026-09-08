package plugins_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func rotateObservabilityPayload(fate string) *gen.RotateObservabilityCredentialPayload {
	return &gen.RotateObservabilityCredentialPayload{
		PreviousKeyFate:  fate,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}
}

func uuidNullProject(authCtx *contextvalues.AuthContext) uuid.NullUUID {
	return uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}
}

func TestRotateObservabilityCredential_RevokeImmediately(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	before, err := keysrepo.New(ti.conn).ListPluginHooksAPIKeysByProject(ctx, keysrepo.ListPluginHooksAPIKeysByProjectParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuidNullProject(authCtx),
	})
	require.NoError(t, err)
	require.Len(t, before, 1)

	createBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	revokeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)
	require.NotNil(t, result.Key)
	require.NotEmpty(t, *result.Key)
	require.True(t, strings.HasPrefix(*result.Key, result.KeyPrefix))
	require.Equal(t, "revoke_immediately", result.PreviousKeyFate)
	require.Len(t, result.PreviousKeys, 1)
	require.Equal(t, before[0].ID.String(), result.PreviousKeys[0].ID)
	require.Nil(t, result.PreviousKeysExpireAt)
	require.False(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.False(t, *result.MarketplaceUpdateDeferred)

	after, err := keysrepo.New(ti.conn).ListPluginHooksAPIKeysByProject(ctx, keysrepo.ListPluginHooksAPIKeysByProjectParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuidNullProject(authCtx),
	})
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, result.KeyPrefix, after[0].KeyPrefix)
	require.NotEqual(t, before[0].ID, after[0].ID)

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, before[0].KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	createAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	revokeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, createBefore+1, createAfter)
	require.Equal(t, revokeBefore+1, revokeAfter)
}

func TestRotateObservabilityCredential_GraceExpiresPreviousKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	before, err := keysrepo.New(ti.conn).ListPluginHooksAPIKeysByProject(ctx, keysrepo.ListPluginHooksAPIKeysByProjectParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuidNullProject(authCtx),
	})
	require.NoError(t, err)
	require.Len(t, before, 1)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.Equal(t, "grace", result.PreviousKeyFate)
	require.Len(t, result.PreviousKeys, 1)
	require.NotNil(t, result.PreviousKeysExpireAt)
	expireAt, err := time.Parse(time.RFC3339, *result.PreviousKeysExpireAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC().Add(7*24*time.Hour), expireAt, time.Minute)

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, before[0].KeyHash)
	require.NoError(t, err, "previous key must stay valid during the grace window")

	_, err = keysrepo.New(ti.conn).SetAPIKeyExpiresAt(ctx, keysrepo.SetAPIKeyExpiresAtParams{
		ExpiresAt:      conv.ToPGTimestamptz(time.Now().UTC().Add(-time.Second)),
		ID:             before[0].ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, before[0].KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestRotateObservabilityCredential_ForbiddenWithoutOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	ctx = authz.GrantsToContext(ctx, nil)

	_, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestRotateObservabilityCredential_InvalidFate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("never"))
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestRotateObservabilityCredential_DoesNotTouchConsumerKeys(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Rotate Consumer"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "rotate-consumer-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Rotate Consumer Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	distributeTestSkill(t, ctx, ti, plugin.ID, "rotate-consumer-skill")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	keysBefore, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	var mcpKeyID string
	for _, key := range keysBefore {
		if strings.HasPrefix(key.Name, "plugins-mcp-") {
			mcpKeyID = key.ID.String()
		}
	}
	require.NotEmpty(t, mcpKeyID)

	mcpCountBefore := countPluginMCPKeys(keysBefore)

	_, err = ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)

	keysAfter, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	var mcpStillPresent bool
	for _, key := range keysAfter {
		if key.ID.String() == mcpKeyID {
			mcpStillPresent = true
		}
	}
	require.True(t, mcpStillPresent, "consumer MCP keys must survive observability rotation")
	require.Equal(t, mcpCountBefore, countPluginMCPKeys(keysAfter), "rotation must not mint a replacement consumer MCP key")
}

func TestRotateObservabilityCredential_RepublishesMarketplaceWhenEligible(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features.SetFlagPayload(feature.FlagHooksRollout, authCtx.ActiveOrganizationID, []byte(`{"version": 9999}`))

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Rotate Publish"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "rotate-publish-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Rotate Publish Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	distributeTestSkill(t, ctx, ti, plugin.ID, "rotate-publish-skill")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	claudeObservability, _ := orgObservabilitySlugs(t, ctx, ti)
	var hooksBefore struct {
		HooksAPIKey string `json:"hooks_api_key"`
	}
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[claudeObservability+"/speakeasy.json"], &hooksBefore))
	require.NotEmpty(t, hooksBefore.HooksAPIKey)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.True(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.False(t, *result.MarketplaceUpdateDeferred)
	require.NotNil(t, result.Key)
	require.Contains(t, *result.Key, result.KeyPrefix)

	var hooksAfter struct {
		HooksAPIKey string `json:"hooks_api_key"`
	}
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[claudeObservability+"/speakeasy.json"], &hooksAfter))
	require.Equal(t, *result.Key, hooksAfter.HooksAPIKey)
	require.NotEqual(t, hooksBefore.HooksAPIKey, hooksAfter.HooksAPIKey)
}

func TestRotateObservabilityCredential_DefersMarketplaceWhenGated(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Rotate Gated"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "rotate-gated-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Rotate Gated Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	distributeTestSkill(t, ctx, ti, plugin.ID, "rotate-gated-skill")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	claudeObservability, _ := orgObservabilitySlugs(t, ctx, ti)
	var hooksBefore struct {
		HooksAPIKey string `json:"hooks_api_key"`
	}
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[claudeObservability+"/speakeasy.json"], &hooksBefore))

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.False(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.True(t, *result.MarketplaceUpdateDeferred)
	require.NotNil(t, result.Key)
	require.NotEqual(t, hooksBefore.HooksAPIKey, *result.Key)

	var hooksAfter struct {
		HooksAPIKey string `json:"hooks_api_key"`
	}
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[claudeObservability+"/speakeasy.json"], &hooksAfter))
	require.Equal(t, hooksBefore.HooksAPIKey, hooksAfter.HooksAPIKey)
}

func TestRotateObservabilityCredential_NoPreviousKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)
	require.Empty(t, result.PreviousKeys)
	require.NotNil(t, result.Key)
	require.NotEmpty(t, result.KeyPrefix)
}

func countPluginMCPKeys(keys []keysrepo.ApiKey) int {
	count := 0
	for _, key := range keys {
		if strings.HasPrefix(key.Name, "plugins-mcp-") {
			count++
		}
	}
	return count
}

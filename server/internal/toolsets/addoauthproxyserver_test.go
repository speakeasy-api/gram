package toolsets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
)

func TestToolsetsService_AddOAuthProxyServer_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Audit OAuth Proxy Toolset")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachOAuthProxy)
	require.NoError(t, err)

	updated, err := ti.service.AddOAuthProxyServer(ctx, &gen.AddOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		OauthProxyServer: &types.OAuthProxyServerForm{
			Slug:                              types.Slug("audit-oauth-proxy"),
			Audience:                          nil,
			ProviderType:                      "gram",
			AuthorizationEndpoint:             nil,
			TokenEndpoint:                     nil,
			ScopesSupported:                   nil,
			TokenEndpointAuthMethodsSupported: []string{"none"},
			EnvironmentSlug:                   nil,
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.OauthProxyServer)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetAttachOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetAttachOAuthProxy), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, updated.Name, record.SubjectDisplay)
	require.Equal(t, string(updated.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, updated.OauthProxyServer.ID, metadata["oauth_proxy_server_id"])
	require.Equal(t, string(updated.OauthProxyServer.Slug), metadata["oauth_proxy_server_slug"])
	require.InDelta(t, updated.ToolsetVersion, metadata["toolset_version_after"], 0)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_AddOAuthProxyServer_InvalidProvider_NoAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Invalid OAuth Proxy Toolset")
	proxyServer := &types.OAuthProxyServerForm{
		Slug:                              types.Slug("invalid-oauth-proxy"),
		Audience:                          nil,
		ProviderType:                      "invalid",
		AuthorizationEndpoint:             nil,
		TokenEndpoint:                     nil,
		ScopesSupported:                   nil,
		TokenEndpointAuthMethodsSupported: []string{"none"},
		EnvironmentSlug:                   nil,
	}

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachOAuthProxy)
	require.NoError(t, err)

	_, err = ti.service.AddOAuthProxyServer(ctx, &gen.AddOAuthProxyServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		Slug:             toolset.Slug,
		OauthProxyServer: proxyServer,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid provider_type value")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

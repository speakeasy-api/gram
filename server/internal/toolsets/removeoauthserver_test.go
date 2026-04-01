package toolsets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
)

func TestToolsetsService_RemoveOAuthServer_ExternalOAuthAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Detach External OAuth Toolset")
	attached, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{
			Slug: types.Slug("detach-external-oauth"),
			Metadata: map[string]any{
				"issuer":         "https://example.com",
				"token_endpoint": "https://example.com/token",
			},
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, attached)
	require.NotNil(t, attached.ExternalOauthServer)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)

	removed, err := ti.service.RemoveOAuthServer(ctx, &gen.RemoveOAuthServerPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, removed)
	require.Nil(t, removed.ExternalOauthServer)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetDetachExternalOAuth), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, removed.Name, record.SubjectDisplay)
	require.Equal(t, string(removed.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, attached.ExternalOauthServer.ID, metadata["external_oauth_server_id"])
	require.Equal(t, string(attached.ExternalOauthServer.Slug), metadata["external_oauth_server_slug"])
	require.InDelta(t, removed.ToolsetVersion, metadata["toolset_version_after"], 0)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_RemoveOAuthServer_OAuthProxyAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Detach OAuth Proxy Toolset")
	attached, err := ti.service.AddOAuthProxyServer(ctx, &gen.AddOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		OauthProxyServer: &types.OAuthProxyServerForm{
			Slug:                              types.Slug("detach-oauth-proxy"),
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
	require.NotNil(t, attached)
	require.NotNil(t, attached.OauthProxyServer)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachOAuthProxy)
	require.NoError(t, err)

	removed, err := ti.service.RemoveOAuthServer(ctx, &gen.RemoveOAuthServerPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, removed)
	require.Nil(t, removed.OauthProxyServer)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetDetachOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetDetachOAuthProxy), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, removed.Name, record.SubjectDisplay)
	require.Equal(t, string(removed.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, attached.OauthProxyServer.ID, metadata["oauth_proxy_server_id"])
	require.Equal(t, string(attached.OauthProxyServer.Slug), metadata["oauth_proxy_server_slug"])
	require.InDelta(t, removed.ToolsetVersion, metadata["toolset_version_after"], 0)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_RemoveOAuthServer_NoServer_NoAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "No OAuth Server Toolset")

	beforeExternalCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)
	beforeProxyCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachOAuthProxy)
	require.NoError(t, err)

	removed, err := ti.service.RemoveOAuthServer(ctx, &gen.RemoveOAuthServerPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, removed)
	require.Nil(t, removed.ExternalOauthServer)
	require.Nil(t, removed.OauthProxyServer)

	afterExternalCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, beforeExternalCount, afterExternalCount)

	afterProxyCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, beforeProxyCount, afterProxyCount)
}

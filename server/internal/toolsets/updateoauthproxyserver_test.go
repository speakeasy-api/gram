package toolsets_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
)

// createPublicToolsetWithCustomOAuthProxy creates a public toolset and attaches a custom
// OAuth proxy server to it. envSlug and proxySlug must be unique within the test project.
func createPublicToolsetWithCustomOAuthProxy(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	toolsetName, envSlug, proxySlug string,
) *types.Toolset {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	envRepo := environmentsRepo.New(ti.conn)
	_, err := envRepo.CreateEnvironment(ctx, environmentsRepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           envSlug,
		Slug:           envSlug,
		Description:    pgtype.Text{String: "env for oauth proxy update test", Valid: true},
	})
	require.NoError(t, err)

	toolset := createMinimalPublicToolset(t, ctx, ti, toolsetName)

	slug := types.Slug(envSlug)
	attached, err := ti.service.AddOAuthProxyServer(ctx, &gen.AddOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		OauthProxyServer: &types.OAuthProxyServerForm{
			Slug:                              types.Slug(proxySlug),
			Audience:                          new("https://original-audience.example.com"),
			ProviderType:                      "custom",
			AuthorizationEndpoint:             new("https://auth.example.com/authorize"),
			TokenEndpoint:                     new("https://auth.example.com/token"),
			ScopesSupported:                   []string{"read", "write"},
			TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
			EnvironmentSlug:                   &slug,
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, attached)
	require.NotNil(t, attached.OauthProxyServer)

	return attached
}

func TestToolsetsService_UpdateOAuthProxyServer_AudienceOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	attached := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"Audience Only OAuth Proxy Toolset",
		"audience-only-env",
		"audience-only-proxy",
	)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)

	// Update only the audience
	updated, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			Audience:                          new("https://new-audience.example.com"),
			AuthorizationEndpoint:             nil,
			TokenEndpoint:                     nil,
			ScopesSupported:                   nil,
			TokenEndpointAuthMethodsSupported: nil,
			EnvironmentSlug:                   nil,
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.OauthProxyServer)

	// Audience should be updated
	require.NotNil(t, updated.OauthProxyServer.Audience)
	require.Equal(t, "https://new-audience.example.com", *updated.OauthProxyServer.Audience)

	// Provider fields should be unchanged
	require.Len(t, updated.OauthProxyServer.OauthProxyProviders, 1)
	provider := updated.OauthProxyServer.OauthProxyProviders[0]
	require.Equal(t, "https://auth.example.com/authorize", provider.AuthorizationEndpoint)
	require.Equal(t, "https://auth.example.com/token", provider.TokenEndpoint)
	require.Equal(t, []string{"read", "write"}, provider.ScopesSupported)

	// One audit log row should have been added
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetUpdateOAuthProxy), record.Action)
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
}

func TestToolsetsService_UpdateOAuthProxyServer_ScopesAndEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	attached := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"Scopes And Endpoints OAuth Proxy Toolset",
		"scopes-endpoints-env",
		"scopes-endpoints-proxy",
	)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)

	// Update authorization endpoint, token endpoint, and scopes
	updated, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			Audience:                          nil,
			AuthorizationEndpoint:             new("https://new-auth.example.com/authorize"),
			TokenEndpoint:                     new("https://new-auth.example.com/token"),
			ScopesSupported:                   []string{"read", "write", "admin"},
			TokenEndpointAuthMethodsSupported: nil,
			EnvironmentSlug:                   nil,
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.OauthProxyServer)

	// All three provider fields should be updated
	require.Len(t, updated.OauthProxyServer.OauthProxyProviders, 1)
	provider := updated.OauthProxyServer.OauthProxyProviders[0]
	require.Equal(t, "https://new-auth.example.com/authorize", provider.AuthorizationEndpoint)
	require.Equal(t, "https://new-auth.example.com/token", provider.TokenEndpoint)
	require.Equal(t, []string{"read", "write", "admin"}, provider.ScopesSupported)

	// Audience was not provided, should still be the original
	require.NotNil(t, updated.OauthProxyServer.Audience)
	require.Equal(t, "https://original-audience.example.com", *updated.OauthProxyServer.Audience)

	// One audit log row should have been added
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

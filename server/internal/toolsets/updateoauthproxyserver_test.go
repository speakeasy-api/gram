package toolsets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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

func TestToolsetsService_UpdateOAuthProxyServer_NoProxyAttached(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	// Create a public toolset without attaching any OAuth proxy server.
	toolset := createMinimalPublicToolset(t, ctx, ti, "No Proxy Attached Toolset")

	_, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			Audience: new("https://audience.example.com"),
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth proxy server attached to this toolset")

	var oopsErr *oops.ShareableError
	require.True(t, errors.As(err, &oopsErr))
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestToolsetsService_UpdateOAuthProxyServer_GramProviderRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Gram-managed providers require a private toolset.
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Gram Provider Rejected Toolset")

	attached, err := ti.service.AddOAuthProxyServer(ctx, &gen.AddOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		OauthProxyServer: &types.OAuthProxyServerForm{
			Slug:                              types.Slug("gram-provider-proxy"),
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

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)

	_, err = ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			Audience: new("https://audience.example.com"),
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "gram-managed OAuth proxy servers cannot be edited via this endpoint")

	// No audit row should have been written on rejection.
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_UpdateOAuthProxyServer_EmptyFormIsNoOp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	attached := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"Empty Form No-op Toolset",
		"empty-form-env",
		"empty-form-proxy",
	)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)

	// Call with an entirely empty update form — all fields nil.
	result, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		Slug:             attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.OauthProxyServer)

	// The returned toolset should be unchanged.
	require.NotNil(t, result.OauthProxyServer.Audience)
	require.Equal(t, "https://original-audience.example.com", *result.OauthProxyServer.Audience)
	require.Len(t, result.OauthProxyServer.OauthProxyProviders, 1)
	provider := result.OauthProxyServer.OauthProxyProviders[0]
	require.Equal(t, "https://auth.example.com/authorize", provider.AuthorizationEndpoint)
	require.Equal(t, "https://auth.example.com/token", provider.TokenEndpoint)

	// No audit row should have been written.
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_UpdateOAuthProxyServer_InvalidAuthMethodRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	attached := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"Invalid Auth Method Toolset",
		"invalid-auth-method-env",
		"invalid-auth-method-proxy",
	)

	beforeCount, auditErr := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, auditErr)

	_, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			TokenEndpointAuthMethodsSupported: []string{"made_up_method"},
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid token_endpoint_auth_methods_supported value")

	afterCount, auditErr := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, auditErr)
	require.Equal(t, beforeCount, afterCount, "no audit row should be written on validation failure")
}

func TestToolsetsService_UpdateOAuthProxyServer_EmptyEnvironmentSlugRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	attached := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"Empty Env Slug Toolset",
		"empty-env-slug-env",
		"empty-env-slug-proxy",
	)

	beforeCount, auditErr := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, auditErr)

	emptySlug := types.Slug("")
	_, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			EnvironmentSlug: &emptySlug,
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "environment_slug cannot be empty")

	afterCount, auditErr := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, auditErr)
	require.Equal(t, beforeCount, afterCount, "no audit row should be written on validation failure")
}

func TestToolsetsService_UpdateOAuthProxyServer_EnvironmentSlugNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	attached := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"Env Slug Not Found Toolset",
		"env-slug-not-found-env",
		"env-slug-not-found-proxy",
	)

	beforeCount, auditErr := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, auditErr)

	nonExistentSlug := types.Slug("non-existent-env")
	_, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			EnvironmentSlug: &nonExistentSlug,
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "environment not found")

	afterCount, auditErr := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, auditErr)
	require.Equal(t, beforeCount, afterCount, "no audit row should be written on validation failure")
}

func TestToolsetsService_UpdateOAuthProxyServer_ClearScopes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	// The helper creates a proxy with ScopesSupported: ["read", "write"].
	attached := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"Clear Scopes OAuth Proxy Toolset",
		"clear-scopes-env",
		"clear-scopes-proxy",
	)

	require.Len(t, attached.OauthProxyServer.OauthProxyProviders, 1)
	require.Equal(t, []string{"read", "write"}, attached.OauthProxyServer.OauthProxyProviders[0].ScopesSupported)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)

	// Pass an explicit empty (non-nil) slice — COALESCE should pick the empty array
	// over the existing value, clearing the scopes column entirely.
	updated, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			ScopesSupported: []string{},
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.OauthProxyServer)

	// Scopes should be empty, not the original ["read", "write"].
	require.Len(t, updated.OauthProxyServer.OauthProxyProviders, 1)
	provider := updated.OauthProxyServer.OauthProxyProviders[0]
	require.Empty(t, provider.ScopesSupported)

	// One audit row should have been added.
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdateOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateOAuthProxyServer_SoftDeletedProxyRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	// Create a toolset with a custom OAuth proxy, then remove the proxy association.
	// After removal the toolset no longer has an OauthProxyServer, so any subsequent
	// UpdateOAuthProxyServer call should return 404 ("no OAuth proxy server attached").
	attached := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"Soft Deleted Proxy Toolset",
		"soft-deleted-proxy-env",
		"soft-deleted-proxy",
	)

	removed, err := ti.service.RemoveOAuthServer(ctx, &gen.RemoveOAuthServerPayload{
		Slug:             attached.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, removed)
	require.Nil(t, removed.OauthProxyServer)

	_, err = ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			Audience: new("https://new-audience.example.com"),
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth proxy server attached")

	var oopsErr *oops.ShareableError
	require.True(t, errors.As(err, &oopsErr))
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestToolsetsService_UpdateOAuthProxyServer_WrongProjectIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)

	// Create a toolset with OAuth proxy under the current (project A) auth context.
	attached := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"Wrong Project Isolation Toolset",
		"wrong-proj-env",
		"wrong-proj-proxy",
	)

	// Create a second project (project B) in the same organisation.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	projectBSlug := "wrong-proj-isolation-" + uuid.New().String()[:8]
	projectB, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           projectBSlug,
		Slug:           projectBSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	// Switch the auth context to project B.
	authCtxB := *authCtx
	authCtxB.ProjectID = &projectB.ID
	authCtxB.ProjectSlug = &projectB.Slug
	ctxB := contextvalues.SetAuthContext(ctx, &authCtxB)

	// The toolset slug belongs to project A; looking it up from project B should
	// return not-found because mv.DescribeToolset filters by project ID.
	_, err = ti.service.UpdateOAuthProxyServer(ctxB, &gen.UpdateOAuthProxyServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         attached.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			Audience: new("https://new-audience.example.com"),
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.True(t, errors.As(err, &oopsErr))
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

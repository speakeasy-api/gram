package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestToolsetsService_AddExternalOAuthServer_HonorsProjectScopedAuthorization(t *testing.T) {
	t.Parallel()

	projectGrant := func(projectID string, scope authz.Scope) authz.Grant {
		return authz.NewGrantWithSelector(scope, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    projectID,
		})
	}

	for _, tc := range []struct {
		name    string
		grants  func(string) []authz.Grant
		allowed bool
	}{
		{name: "matching project grant", grants: func(projectID string) []authz.Grant {
			return []authz.Grant{projectGrant(projectID, authz.ScopeMCPWrite)}
		}, allowed: true},
		{name: "other project grant", grants: func(string) []authz.Grant {
			return []authz.Grant{projectGrant(uuid.NewString(), authz.ScopeMCPWrite)}
		}},
		{name: "matching project exclusion", grants: func(projectID string) []authz.Grant {
			return []authz.Grant{
				authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
				projectGrant(projectID, authz.ScopeMCPBlockedWrite),
			}
		}},
		{name: "other project exclusion", grants: func(string) []authz.Grant {
			return []authz.Grant{
				authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
				projectGrant(uuid.NewString(), authz.ScopeMCPBlockedWrite),
			}
		}, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestToolsetsService(t)
			ctx = withAccountType(t, ctx, "pro")
			toolset := createMinimalPublicToolset(t, ctx, ti, "Project Authorization External OAuth Toolset")
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			ctx = authztest.WithExactGrants(t, ctx, tc.grants(authCtx.ProjectID.String())...)
			_, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
				Slug: toolset.Slug, ExternalOauthServer: &types.ExternalOAuthServerForm{Metadata: map[string]any{"issuer": "https://example.com"}},
			})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				var oopsErr *oops.ShareableError
				require.ErrorAs(t, err, &oopsErr)
				require.Equal(t, oops.CodeForbidden, oopsErr.Code)
			}
		})
	}
}

func TestToolsetsService_AddExternalOAuthServer_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Audit External OAuth Toolset")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)

	updated, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{
			Slug: externalOAuthSlug("audit-external-oauth"),
			Metadata: map[string]any{
				"issuer":         "https://example.com",
				"token_endpoint": "https://example.com/token",
			},
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.ExternalOauthServer)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetAttachExternalOAuth), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, updated.Name, record.SubjectDisplay)
	require.Equal(t, string(updated.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, updated.ExternalOauthServer.ID, metadata["external_oauth_server_id"])
	require.Equal(t, string(updated.ExternalOauthServer.Slug), metadata["external_oauth_server_slug"])
	require.Equal(t, false, metadata["authorization_server_issuer_set"])
	require.InDelta(t, updated.ToolsetVersion, metadata["toolset_version_after"], 0)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_AddExternalOAuthServer_FreeTierDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Free Tier External OAuth Toolset")
	freeCtx := withAccountType(t, ctx, "free")

	_, err := ti.service.AddExternalOAuthServer(freeCtx, &gen.AddExternalOAuthServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{
			Slug: externalOAuthSlug("free-tier-external-oauth"),
			Metadata: map[string]any{
				"issuer":         "https://example.com",
				"token_endpoint": "https://example.com/token",
			},
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Contains(t, err.Error(), "free accounts cannot add external OAuth servers")
}

func TestToolsetsService_AddExternalOAuthServer_GeneratedSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Generated External OAuth Slug")

	updated, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		Slug:                toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{Metadata: map[string]any{"issuer": "https://auth.example.com"}},
	})
	require.NoError(t, err)
	require.Equal(t, string(toolset.Slug)+"-oauth", string(updated.ExternalOauthServer.Slug))
	require.NotNil(t, updated.ExternalOauthServer.Metadata)
}

func TestToolsetsService_AddExternalOAuthServer_GeneratedSlugCollision(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Collision External OAuth Slug")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	baseSlug := string(toolset.Slug) + "-oauth"
	_, err := oauthrepo.New(ti.conn).CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: *authCtx.ProjectID, Slug: baseSlug, Metadata: []byte(`{}`),
	})
	require.NoError(t, err)

	updated, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		Slug:                toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{Metadata: map[string]any{"issuer": "https://auth.example.com"}},
	})
	require.NoError(t, err)
	require.Regexp(t, "^collision-external-oauth-.+-oauth-[a-z0-9]{5}$", string(updated.ExternalOauthServer.Slug))
	require.NotEqual(t, baseSlug, string(updated.ExternalOauthServer.Slug))
	require.LessOrEqual(t, len(updated.ExternalOauthServer.Slug), 40)
}

func TestToolsetsService_AddExternalOAuthServer_CallerSlugCollision(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPublicToolset(t, ctx, ti, "Caller External OAuth Slug")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	const slug = "caller-external-oauth"
	_, err := oauthrepo.New(ti.conn).CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: *authCtx.ProjectID, Slug: slug, Metadata: []byte(`{}`),
	})
	require.NoError(t, err)

	_, err = ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		Slug:                toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{Slug: externalOAuthSlug(slug), Metadata: map[string]any{"issuer": "https://auth.example.com"}},
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestToolsetsService_AddExternalOAuthServer_PrivateToolset_NoAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Private External OAuth Toolset")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)

	_, err = ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{
			Slug: externalOAuthSlug("private-external-oauth"),
			Metadata: map[string]any{
				"issuer":         "https://example.com",
				"token_endpoint": "https://example.com/token",
			},
		},
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "private MCP servers cannot have external OAuth servers")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetAttachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

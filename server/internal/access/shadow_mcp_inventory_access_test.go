package access

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_AllowShadowMCPInventoryServer_CanonicalizesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)

	first, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerURL:  "HTTPS://Example.COM:443/mcp?token=secret#fragment",
		ServerName: conv.PtrEmpty("Example MCP"),
		Reason:     conv.PtrEmpty("Approved for project"),
	})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/mcp", first.CanonicalServerURL)
	require.Equal(t, "example.com", first.URLHost)
	require.Equal(t, shadowMCPInventoryAccessAllowed, first.Access)
	require.NotNil(t, first.Rule)
	require.Equal(t, shadowMCPRuleAllowed, first.Rule.Disposition)
	require.Equal(t, shadowMCPAccessScopeProject, first.Rule.AccessScope)
	require.Equal(t, projectID, *first.Rule.ProjectID)
	require.Equal(t, "full_url", first.Rule.MatchBreadth)
	require.Equal(t, "https://example.com/mcp", first.Rule.MatchValue)
	require.Equal(t, "Example MCP", first.Rule.DisplayName)

	second, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID: projectID,
		ServerURL: "https://example.com/mcp?another=ignored",
	})
	require.NoError(t, err)
	require.NotNil(t, second.Rule)
	require.Equal(t, first.Rule.ID, second.Rule.ID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_BlockShadowMCPInventoryServer_UpdatesExistingURLRule(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	allowed, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerURL:  "https://switch.example.com/mcp",
		ServerName: conv.PtrEmpty("Switch MCP"),
	})
	require.NoError(t, err)
	require.NotNil(t, allowed.Rule)
	updateBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleUpdate)
	require.NoError(t, err)

	blocked, err := ti.service.BlockShadowMCPInventoryServer(ctx, &gen.BlockShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerURL:  "https://switch.example.com/mcp?ignored=true",
		ServerName: conv.PtrEmpty("Switch MCP"),
		Reason:     conv.PtrEmpty("Block this server"),
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessDenied, blocked.Access)
	require.NotNil(t, blocked.Rule)
	require.Equal(t, allowed.Rule.ID, blocked.Rule.ID)
	require.Equal(t, shadowMCPRuleDenied, blocked.Rule.Disposition)

	updateAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleUpdate)
	require.NoError(t, err)
	require.Equal(t, updateBefore+1, updateAfter)
}

func TestService_ClearShadowMCPInventoryServerAccess_RemovesURLRule(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_ = createShadowMCPAccessRuleForTest(t, ctx, ti, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		AccessScope:  shadowMCPAccessScopeOrganization,
		MatchBreadth: "url_host",
		MatchValue:   "clear.example.com",
		DisplayName:  "Broader deny",
	})
	allowed, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerURL:  "https://clear.example.com/mcp",
		ServerName: conv.PtrEmpty("Clear MCP"),
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessAllowed, allowed.Access)
	deleteBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleDelete)
	require.NoError(t, err)

	cleared, err := ti.service.ClearShadowMCPInventoryServerAccess(ctx, &gen.ClearShadowMCPInventoryServerAccessPayload{
		ProjectID: projectID,
		ServerURL: "https://clear.example.com/mcp?token=ignored",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessNone, cleared.Access)
	require.Nil(t, cleared.Rule)

	clearedAgain, err := ti.service.ClearShadowMCPInventoryServerAccess(ctx, &gen.ClearShadowMCPInventoryServerAccessPayload{
		ProjectID: projectID,
		ServerURL: "https://clear.example.com/mcp",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessNone, clearedAgain.Access)
	deleteAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleDelete)
	require.NoError(t, err)
	require.Equal(t, deleteBefore+1, deleteAfter)
}

func TestService_AllowShadowMCPInventoryServer_RejectsInvalidURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID: authCtx.ProjectID.String(),
		ServerURL: "not a url",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_ShadowMCPInventoryServerAccess_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		run  func(ctx context.Context, ti *testInstance, projectID string) error
	}{
		{
			name: "allow",
			run: func(ctx context.Context, ti *testInstance, projectID string) error {
				_, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
					ProjectID: projectID,
					ServerURL: "https://forbidden.example.com/mcp",
				})
				return err
			},
		},
		{
			name: "block",
			run: func(ctx context.Context, ti *testInstance, projectID string) error {
				_, err := ti.service.BlockShadowMCPInventoryServer(ctx, &gen.BlockShadowMCPInventoryServerPayload{
					ProjectID: projectID,
					ServerURL: "https://forbidden.example.com/mcp",
				})
				return err
			},
		},
		{
			name: "clear",
			run: func(ctx context.Context, ti *testInstance, projectID string) error {
				_, err := ti.service.ClearShadowMCPInventoryServerAccess(ctx, &gen.ClearShadowMCPInventoryServerAccessPayload{
					ProjectID: projectID,
					ServerURL: "https://forbidden.example.com/mcp",
				})
				return err
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestAccessService(t)
			authCtx := testAccessAuthContext(t, ctx)
			ctx = withRBACGrants(t, ctx)

			err := tt.run(ctx, ti, authCtx.ProjectID.String())
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr)
			require.Equal(t, oops.CodeForbidden, oopsErr.Code)
		})
	}
}

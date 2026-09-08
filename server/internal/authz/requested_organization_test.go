package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestRequireUserOrganizationScopeLoadsRequestedOrganizationRoleGrants(t *testing.T) {
	t.Parallel()

	activeOrganizationID := "org_requested_scope_active"
	targetOrganizationID := "org_requested_scope_target"
	userID := "user_requested_scope_member"
	ctx := enterpriseSessionCtxWithOrg(t, activeOrganizationID)
	conn := newTestDB(t)

	seedOrganization(t, ctx, conn, activeOrganizationID)
	seedOrganization(t, ctx, conn, targetOrganizationID)
	seedActiveOrganizationUser(t, ctx, conn, targetOrganizationID, userID)
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, targetOrganizationID))
	seedRoleAssignmentForUser(t, ctx, conn, targetOrganizationID, userID, SystemRoleMember)

	ctx = GrantsToContext(ctx, []Grant{NewGrant(ScopeOrgRead, activeOrganizationID)})
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	require.NoError(t, engine.RequireUserOrganizationScope(ctx, targetOrganizationID, userID, ScopeOrgRead))
}

func TestRequireUserOrganizationScopeRejectsActiveOrganizationGrants(t *testing.T) {
	t.Parallel()

	activeOrganizationID := "org_requested_scope_only_active"
	targetOrganizationID := "org_requested_scope_without_grants"
	userID := "user_requested_scope_without_target_grants"
	ctx := enterpriseSessionCtxWithOrg(t, activeOrganizationID)
	conn := newTestDB(t)

	seedOrganization(t, ctx, conn, activeOrganizationID)
	seedOrganization(t, ctx, conn, targetOrganizationID)
	seedActiveOrganizationUser(t, ctx, conn, targetOrganizationID, userID)
	ctx = GrantsToContext(ctx, []Grant{NewGrant(ScopeOrgAdmin, WildcardResource)})
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	err := engine.RequireUserOrganizationScope(ctx, targetOrganizationID, userID, ScopeOrgRead)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
}

func TestRequireUserOrganizationScopeSkipsSessionlessRequests(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	engine := NewEngine(testenv.NewLogger(t), nil, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	require.NoError(t, engine.RequireUserOrganizationScope(ctx, "org_requested_scope_target", authCtx.UserID, ScopeOrgRead))
}

func TestRequireUserOrganizationScopeRejectsAPIKeys(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.APIKeyID = "api_key_requested_scope"
	authCtx.SessionID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	engine := NewEngine(testenv.NewLogger(t), nil, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	err := engine.RequireUserOrganizationScope(ctx, "org_requested_scope_target", authCtx.UserID, ScopeOrgRead)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
}

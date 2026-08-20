package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var expectedFullAccessScopes = []string{
	string(authz.ScopeOrgRead),
	string(authz.ScopeOrgAdmin),
	string(authz.ScopeProjectRead),
	string(authz.ScopeProjectWrite),
	string(authz.ScopeMCPRead),
	string(authz.ScopeMCPWrite),
	string(authz.ScopeMCPConnect),
	string(authz.ScopeEnvironmentRead),
	string(authz.ScopeEnvironmentWrite),
	string(authz.ScopeSkillRead),
	string(authz.ScopeSkillWrite),
	string(authz.ScopeRiskPolicyEvaluate),
	string(authz.ScopeRiskPolicyBypass),
	string(authz.ScopeRiskPolicyBlock),
	string(authz.ScopeChatRead),
	string(authz.ScopeChatWrite),
}

// TestDemoGrantsMatchEnforcedScopes holds the set ListGrants reports to the
// dashboard against the set authz.Engine.PrepareContext enforces. They are
// produced by different functions on the same condition, and when they drifted
// apart the demo org rendered pages (Costs, Budgets, Organization API Keys)
// whose handlers then returned 403.
func TestDemoGrantsMatchEnforcedScopes(t *testing.T) {
	t.Parallel()

	reported := make([]string, 0, len(userVisibleScopeGrants()))
	for _, grant := range userVisibleScopeGrants() {
		reported = append(reported, grant.Scope)
	}

	enforced := make([]string, 0, len(authz.DemoScopeGrants()))
	for _, grant := range authz.DemoScopeGrants() {
		enforced = append(enforced, string(grant.Scope))
	}

	require.ElementsMatch(t, reported, enforced)
	require.ElementsMatch(t, expectedFullAccessScopes, enforced)
}

func TestService_ListGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", ""))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, mockMember("", "membership_1", "workos_user_member", "custom-builder"))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeProjectRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeRiskPolicyEvaluate, "policy_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, seededRolePrincipal(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "custom-builder"), authz.ScopeMCPConnect, "tool_456")

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, 3)
	byScope := make(map[string]*gen.ListRoleGrant, len(result.Grants))
	for _, grant := range result.Grants {
		byScope[grant.Scope] = grant
	}
	require.Len(t, byScope["project:read"].Selectors, 1)
	require.Equal(t, "project_123", byScope["project:read"].Selectors[0].ResourceID)
	require.Len(t, byScope["mcp:connect"].Selectors, 1)
	require.Equal(t, "tool_456", byScope["mcp:connect"].Selectors[0].ResourceID)
	require.Len(t, byScope["risk_policy:evaluate"].Selectors, 1)
	require.Equal(t, "policy_123", byScope["risk_policy:evaluate"].Selectors[0].ResourceID)
}

func TestService_ListGrants_RoleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", ""))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, mockMember("", "membership_1", "workos_user_member", "custom-builder"))
	rolePrincipal := seededRolePrincipal(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "custom-builder")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, rolePrincipal, authz.ScopeProjectRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, rolePrincipal, authz.ScopeMCPConnect, "tool_456")

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)
	byScope := make(map[string]*gen.ListRoleGrant, len(result.Grants))
	for _, grant := range result.Grants {
		byScope[grant.Scope] = grant
	}
	require.Len(t, byScope["project:read"].Selectors, 1)
	require.Equal(t, "project_123", byScope["project:read"].Selectors[0].ResourceID)
	require.Len(t, byScope["mcp:connect"].Selectors, 1)
	require.Equal(t, "tool_456", byScope["mcp:connect"].Selectors[0].ResourceID)
}

func TestService_ListGrants_NotConnectedLoadsAllUserGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// Remove the org-user relationship created by InitAuthContext so the user
	// is "not connected" from the DB perspective.
	err := orgrepo.New(ti.conn).DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(authCtx.UserID),
	})
	require.NoError(t, err)
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authz.AllUsersPrincipal(), authz.ScopeRiskPolicyEvaluate, "policy-for-everyone")

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, string(authz.ScopeRiskPolicyEvaluate), result.Grants[0].Scope)
	require.Equal(t, "policy-for-everyone", result.Grants[0].Selectors[0].ResourceID)
}

func TestService_ListGrants_InvalidUserPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.UserID = urn.AllUsersPrincipalID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	require.ErrorIs(t, err, authz.ErrPrincipalInvalid)
}

func TestService_ListGrants_WithoutActiveOrganizationReturnsNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.ActiveOrganizationID = ""
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Grants)
}

func TestService_ListGrants_AdminImpersonatingReturnsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// RBAC-enforced org, admin user, admin override set, but NO
	// organization_users row — mirrors real impersonation.
	authCtx.IsAdmin = true
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = contextvalues.SetAdminOverrideInContext(ctx, "customer-org")

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, len(expectedFullAccessScopes))

	byScope := make(map[string]*gen.ListRoleGrant, len(result.Grants))
	for _, grant := range result.Grants {
		byScope[grant.Scope] = grant
	}

	for _, scope := range expectedFullAccessScopes {
		grant, ok := byScope[scope]
		require.True(t, ok)
		require.Nil(t, grant.Selectors)
	}
}

func TestService_ListGrants_NonEnterpriseLoadsEffectiveGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeProjectRead, "project_non_enterprise")

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, string(authz.ScopeProjectRead), result.Grants[0].Scope)
	require.Len(t, result.Grants[0].Selectors, 1)
	require.Equal(t, "project_non_enterprise", result.Grants[0].Selectors[0].ResourceID)
}

func TestService_ListGrants_WithoutSessionReturnsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	authCtx.SessionID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, len(expectedFullAccessScopes))

	byScope := make(map[string]*gen.ListRoleGrant, len(result.Grants))
	for _, grant := range result.Grants {
		byScope[grant.Scope] = grant
	}

	for _, scope := range expectedFullAccessScopes {
		grant, ok := byScope[scope]
		require.True(t, ok)
		require.Nil(t, grant.Selectors)
	}
}

func TestService_ListGrants_NoRoleAssignments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Grants)
}

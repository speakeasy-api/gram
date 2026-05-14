package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
}

func TestService_ListGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", ""))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, mockMember("", "membership_1", "workos_user_member", "custom-builder"))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeProjectRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeMCPConnect, "tool_456")

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

func TestService_ListGrants_RoleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", ""))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, mockMember("", "membership_1", "workos_user_member", "custom-builder"))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeMCPConnect, "tool_456")

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

func TestService_ListGrants_NotConnected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "current user has not joined this organization")
}

func TestService_ListGrants_AdminImpersonatingReturnsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// Enterprise org with RBAC enforced, admin user, admin override set,
	// but NO organization_users row — mirrors real impersonation.
	authCtx.AccountType = "enterprise"
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

func TestService_ListGrants_NonEnterpriseReturnsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	authCtx.AccountType = "pro"
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

func TestService_ListGrants_RBACDisabledReturnsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	ti.service.authz = authz.NewEngine(ti.service.logger, ti.conn, chConn, authztest.RBACAlwaysDisabled, authztest.ChallengeLoggingAlwaysDisabled, ti.roles)

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

func TestService_ListGrants_EnterpriseWithoutSessionReturnsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	authCtx.AccountType = "enterprise"
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
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Grants)
}

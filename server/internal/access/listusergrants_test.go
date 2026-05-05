package access

import (
	"errors"
	"testing"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
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
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeProjectRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeMCPConnect, "tool_456")

	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "workos_user_member", "custom-builder"),
	}, nil).Once()

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

func TestService_ListGrants_MultipleRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-mcp"), authz.ScopeMCPConnect, "tool_456")

	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "workos_user_member", "custom-builder"),
		mockMember(mockidp.MockOrgID, "membership_2", "workos_user_member", "custom-mcp"),
	}, nil).Once()

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
	ti.service.authz = authz.NewEngine(ti.service.logger, ti.conn, authztest.RBACAlwaysDisabled, ti.roles, nil)

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

func TestService_ListGrants_WorkOSMembersFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "member@example.com", "Member User", "workos_user_member", "membership_1")

	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list members from workos")
}

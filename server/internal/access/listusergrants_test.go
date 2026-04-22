package access

import (
	"errors"
	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var expectedFullAccessScopes = []string{
	string(authz.ScopeOrgRead),
	string(authz.ScopeOrgAdmin),
	string(authz.ScopeBuildRead),
	string(authz.ScopeBuildWrite),
	string(authz.ScopeMCPRead),
	string(authz.ScopeMCPWrite),
	string(authz.ScopeMCPConnect),
	string(authz.ScopeRemoteMCPRead),
	string(authz.ScopeRemoteMCPWrite),
	string(authz.ScopeRemoteMCPConnect),
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
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeBuildRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeMCPConnect, "tool_456")

	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "workos_user_member", "custom-builder"),
	}, nil).Once()

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)
	require.Equal(t, "build:read", result.Grants[0].Scope)
	require.Equal(t, []string{"project_123"}, result.Grants[0].Resources)
	require.Equal(t, "mcp:connect", result.Grants[1].Scope)
	require.Equal(t, []string{"tool_456"}, result.Grants[1].Resources)
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
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeBuildRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-mcp"), authz.ScopeMCPConnect, "tool_456")

	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "workos_user_member", "custom-builder"),
		mockMember(mockidp.MockOrgID, "membership_2", "workos_user_member", "custom-mcp"),
	}, nil).Once()

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)
	require.Equal(t, "build:read", result.Grants[0].Scope)
	require.Equal(t, []string{"project_123"}, result.Grants[0].Resources)
	require.Equal(t, "mcp:connect", result.Grants[1].Scope)
	require.Equal(t, []string{"tool_456"}, result.Grants[1].Resources)
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
		require.Nil(t, grant.Resources)
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
	ti.service.authz = authz.NewEngine(ti.service.logger, ti.conn, stubFeatureChecker{enabled: false}, ti.roles, nil)

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
		require.Nil(t, grant.Resources)
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
		require.Nil(t, grant.Resources)
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

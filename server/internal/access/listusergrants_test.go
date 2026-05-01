package access

import (
	"errors"
	"testing"

	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"

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

// ListGrants for an api-key-authenticated request must return the api_key
// principal's grants — not the publisher's (the user that created the key).
// rfc-plugin-scoped-keys.md: a plugin-scoped key calling /rpc/access.listGrants
// should reflect what the key can do, not what the publisher can do.
func TestService_ListGrants_APIKeyPrincipal_ReturnsKeyGrantsNotPublisherGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// Replace the session-style auth ctx with one that looks like an api-key
	// request: APIKeyID set, no SessionID.
	authCtx.AccountType = "enterprise"
	authCtx.APIKeyID = "test_api_key_id"
	authCtx.APIKeySystemManaged = true
	authCtx.SessionID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	// Seed a publisher (user) grant that should NOT show up in the response,
	// and an api_key principal grant that SHOULD.
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "publisher@example.com", "Publisher", "workos_user_publisher", "membership_pub")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeOrgAdmin, "*")

	apiKeyPrincipal := urn.NewPrincipal(urn.PrincipalTypeAPIKey, authCtx.APIKeyID)
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, apiKeyPrincipal, authz.ScopeMCPConnect, "tool_for_key")

	// Drop just the api_key's grant onto the context the way PrepareContext
	// would for a real request.
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPConnect, "tool_for_key"))

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1, "must return only the api_key's grants, not the publisher's")
	require.Equal(t, "mcp:connect", result.Grants[0].Scope)
	require.Len(t, result.Grants[0].Selectors, 1)
	require.Equal(t, "tool_for_key", result.Grants[0].Selectors[0].ResourceID)

	// And explicitly: the publisher's org:admin grant must not appear.
	for _, g := range result.Grants {
		require.NotEqual(t, "org:admin", g.Scope, "publisher's grants must not leak into an api-key listGrants response")
	}
}

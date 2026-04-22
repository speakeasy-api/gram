package access

import (
	"errors"
	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_GetRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_custom", "Custom Builder", "custom-builder", "Can build selected resources"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom-builder"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"),
		mockMember(mockidp.MockOrgID, "membership_3", "user_3", "admin"),
		// user_workos_only has never logged into Gram — should not be counted
		mockMember(mockidp.MockOrgID, "membership_workos_only", "user_workos_only", "custom-builder"),
	}, nil).Once()

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "user2@test.com", "User 2", "user_2", "membership_2")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_3", "user3@test.com", "User 3", "user_3", "membership_3")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeMCPConnect, WildcardResource)

	role, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_custom"})
	require.NoError(t, err)
	require.Equal(t, "role_custom", role.ID)
	require.Equal(t, "Custom Builder", role.Name)
	require.Equal(t, "Can build selected resources", role.Description)
	require.False(t, role.IsSystem)
	require.Equal(t, 2, role.MemberCount)
	require.Equal(t, mockRoleTimestamp, role.CreatedAt)
	require.Equal(t, mockRoleTimestamp, role.UpdatedAt)
	require.Len(t, role.Grants, 2)

	grantsByScope := make(map[string]*gen.RoleGrant, len(role.Grants))
	for _, grant := range role.Grants {
		grantsByScope[grant.Scope] = grant
	}
	require.ElementsMatch(t, []string{"project-1"}, grantsByScope[string(ScopeBuildRead)].Resources)
	require.Nil(t, grantsByScope[string(ScopeMCPConnect)].Resources)
}

func TestService_GetRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{}, nil).Once()

	_, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_GetRole_OrganizationNotLinkedToWorkOS(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	missingLinkOrgID := "org_without_workos_link"
	seedOrganization(t, ctx, ti.conn, missingLinkOrgID)
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  missingLinkOrgID,
		UserID:                authCtx.UserID,
		ExternalUserID:        authCtx.ExternalUserID,
		APIKeyID:              authCtx.APIKeyID,
		SessionID:             authCtx.SessionID,
		ProjectID:             authCtx.ProjectID,
		OrganizationSlug:      authCtx.OrganizationSlug,
		Email:                 authCtx.Email,
		AccountType:           authCtx.AccountType,
		HasActiveSubscription: authCtx.HasActiveSubscription,
		ProjectSlug:           authCtx.ProjectSlug,
		APIKeyScopes:          authCtx.APIKeyScopes,
	})

	_, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_custom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "organization is not linked to WorkOS")
}

func TestService_GetRole_WorkOSListRolesFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_custom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list roles from workos")
}

package access

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	trequire "github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_GetRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_custom", "Custom Builder", "custom-builder", "Can build selected resources"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "custom-builder"),
		mockMember("org_workos_test", "membership_2", "user_2", "custom-builder"),
		mockMember("org_workos_test", "membership_3", "user_3", "admin"),
	}, nil).Once()

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeMCPConnect, WildcardResource)

	role, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_custom"})
	trequire.NoError(t, err)
	trequire.Equal(t, "role_custom", role.ID)
	trequire.Equal(t, "Custom Builder", role.Name)
	trequire.Equal(t, "Can build selected resources", role.Description)
	trequire.False(t, role.IsSystem)
	trequire.Equal(t, 2, role.MemberCount)
	trequire.Equal(t, mockRoleTimestamp, role.CreatedAt)
	trequire.Equal(t, mockRoleTimestamp, role.UpdatedAt)
	trequire.Len(t, role.Grants, 2)

	grantsByScope := make(map[string]*gen.RoleGrant, len(role.Grants))
	for _, grant := range role.Grants {
		grantsByScope[grant.Scope] = grant
	}
	trequire.ElementsMatch(t, []string{"project-1"}, grantsByScope[string(ScopeBuildRead)].Resources)
	trequire.Nil(t, grantsByScope[string(ScopeMCPConnect)].Resources)
}

func TestService_GetRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{}, nil).Once()

	_, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_missing"})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "role not found")
}

func TestService_GetRole_OrganizationNotLinkedToWorkOS(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)

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
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "organization is not linked to WorkOS")
}

func TestService_GetRole_WorkOSListRolesFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_custom"})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "list roles from workos")
}

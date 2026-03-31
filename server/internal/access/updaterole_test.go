package access_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_UpdateRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	name := "Platform Builder"
	description := "Updated description"

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("UpdateRole", mock.Anything, "org_workos_test", "custom-builder", thirdpartyworkos.UpdateRoleOpts{
		Name:        &name,
		Description: &description,
	}).Return(&thirdpartyworkos.Role{
		ID:          "role_custom",
		Name:        name,
		Slug:        "custom-builder",
		Description: description,
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "member"),
		mockMember("org_workos_test", "membership_2", "user_2", "member"),
		mockMember("org_workos_test", "membership_3", "user_3", "custom-builder"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: "org_workos_test",
		RoleSlug:       "custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_2", "custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_2",
		UserID:         "user_2",
		OrganizationID: "org_workos_test",
		RoleSlug:       "custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "custom-builder"),
		mockMember("org_workos_test", "membership_2", "user_2", "custom-builder"),
		mockMember("org_workos_test", "membership_3", "user_3", "custom-builder"),
	}, nil).Once()

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), access.ScopeBuildRead, "project-old")
	role, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:          "role_custom",
		Name:        &name,
		Description: &description,
		Grants: []*gen.RoleGrant{
			{Scope: string(access.ScopeBuildWrite), Resources: []string{"project-1", "project-2"}},
			{Scope: string(access.ScopeMCPConnect), Resources: nil},
		},
		MemberIds: []string{"user_1", "user_2"},
	})
	require.NoError(t, err)
	require.Equal(t, "role_custom", role.ID)
	require.Equal(t, name, role.Name)
	require.Equal(t, description, role.Description)
	require.False(t, role.IsSystem)
	require.Equal(t, 3, role.MemberCount)
	require.Equal(t, mockRoleTimestamp, role.CreatedAt)
	require.Equal(t, mockRoleTimestamp, role.UpdatedAt)
	require.Len(t, role.Grants, 2)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	require.Len(t, grants, 3)
}

func TestService_UpdateRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{}, nil).Once()

	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: "role_missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_UpdateRole_WorkOSUpdateFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("UpdateRole", mock.Anything, "org_workos_test", "custom-builder", thirdpartyworkos.UpdateRoleOpts{}).Return((*thirdpartyworkos.Role)(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: "role_custom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "update role in workos")
}

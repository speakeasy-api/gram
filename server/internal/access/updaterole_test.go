package access_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
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
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "grace@example.com", "Grace", "user_2", "membership_2")
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
		MemberIds: []string{"local_user_1", "local_user_2"},
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
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{}, nil).Once()
	ti.roles.On("UpdateRole", mock.Anything, "org_workos_test", "custom-builder", thirdpartyworkos.UpdateRoleOpts{}).Return((*thirdpartyworkos.Role)(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: "role_custom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "update role in workos")
}

func TestService_UpdateRole_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)

	name := "Audit Builder"
	description := "After description"
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Before Builder", "custom-builder", "Before description"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "custom-builder"),
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
		mockMember("org_workos_test", "membership_1", "user_1", "custom-builder"),
		mockMember("org_workos_test", "membership_2", "user_2", "custom-builder"),
	}, nil).Once()

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), access.ScopeBuildRead, "project-old")

	updated, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:          "role_custom",
		Name:        &name,
		Description: &description,
		Grants: []*gen.RoleGrant{{
			Scope:     string(access.ScopeBuildWrite),
			Resources: []string{"project-1"},
		}},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAccessRoleUpdate), record.Action)
	require.Equal(t, "access_role", record.SubjectType)
	require.Equal(t, updated.Name, record.SubjectDisplay)
	require.Equal(t, "custom-builder", record.SubjectSlug)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Before Builder", beforeSnapshot["Name"])
	require.Equal(t, updated.Name, afterSnapshot["Name"])
	beforeGrants, ok := beforeSnapshot["Grants"].([]any)
	require.True(t, ok)
	require.Len(t, beforeGrants, 1)
	beforeGrant, ok := beforeGrants[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, string(access.ScopeBuildRead), beforeGrant["Scope"])
	beforeResources, ok := beforeGrant["Resources"].([]any)
	require.True(t, ok)
	require.Len(t, beforeResources, 1)
	require.Equal(t, "project-old", beforeResources[0])
	afterGrants, ok := afterSnapshot["Grants"].([]any)
	require.True(t, ok)
	require.Len(t, afterGrants, 1)
	afterGrant, ok := afterGrants[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, string(access.ScopeBuildWrite), afterGrant["Scope"])
	afterResources, ok := afterGrant["Resources"].([]any)
	require.True(t, ok)
	require.Len(t, afterResources, 1)
	require.Equal(t, "project-1", afterResources[0])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

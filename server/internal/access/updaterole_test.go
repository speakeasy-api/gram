package access

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	trequire "github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
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
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)
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

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-old")
	role, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:          "role_custom",
		Name:        &name,
		Description: &description,
		Grants: []*gen.RoleGrant{
			{Scope: string(ScopeBuildWrite), Resources: []string{"project-1", "project-2"}},
			{Scope: string(ScopeMCPConnect), Resources: nil},
		},
		MemberIds: []string{"user_1", "user_2"},
	})
	trequire.NoError(t, err)
	trequire.Equal(t, "role_custom", role.ID)
	trequire.Equal(t, name, role.Name)
	trequire.Equal(t, description, role.Description)
	trequire.False(t, role.IsSystem)
	trequire.Equal(t, 3, role.MemberCount)
	trequire.Equal(t, mockRoleTimestamp, role.CreatedAt)
	trequire.Equal(t, mockRoleTimestamp, role.UpdatedAt)
	trequire.Len(t, role.Grants, 2)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	trequire.Len(t, grants, 3)
}

func TestService_UpdateRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{}, nil).Once()

	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: "role_missing"})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "role not found")
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
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "update role in workos")
}

func TestService_UpdateRole_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	trequire.NoError(t, err)

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

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-old")

	updated, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:          "role_custom",
		Name:        &name,
		Description: &description,
		Grants: []*gen.RoleGrant{{
			Scope:     string(ScopeBuildWrite),
			Resources: []string{"project-1"},
		}},
	})
	trequire.NoError(t, err)
	trequire.NotNil(t, updated)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	trequire.NoError(t, err)
	trequire.Equal(t, string(audit.ActionAccessRoleUpdate), record.Action)
	trequire.Equal(t, "access_role", record.SubjectType)
	trequire.Equal(t, updated.Name, record.SubjectDisplay)
	trequire.Equal(t, "custom-builder", record.SubjectSlug)
	trequire.NotNil(t, record.BeforeSnapshot)
	trequire.NotNil(t, record.AfterSnapshot)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	trequire.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	trequire.NoError(t, err)
	trequire.Equal(t, "Before Builder", beforeSnapshot["Name"])
	trequire.Equal(t, updated.Name, afterSnapshot["Name"])
	beforeGrants, ok := beforeSnapshot["Grants"].([]any)
	trequire.True(t, ok)
	trequire.Len(t, beforeGrants, 1)
	beforeGrant, ok := beforeGrants[0].(map[string]any)
	trequire.True(t, ok)
	trequire.Equal(t, string(ScopeBuildRead), beforeGrant["Scope"])
	beforeResources, ok := beforeGrant["Resources"].([]any)
	trequire.True(t, ok)
	trequire.Len(t, beforeResources, 1)
	trequire.Equal(t, "project-old", beforeResources[0])
	afterGrants, ok := afterSnapshot["Grants"].([]any)
	trequire.True(t, ok)
	trequire.Len(t, afterGrants, 1)
	afterGrant, ok := afterGrants[0].(map[string]any)
	trequire.True(t, ok)
	trequire.Equal(t, string(ScopeBuildWrite), afterGrant["Scope"])
	afterResources, ok := afterGrant["Resources"].([]any)
	trequire.True(t, ok)
	trequire.Len(t, afterResources, 1)
	trequire.Equal(t, "project-1", afterResources[0])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	trequire.NoError(t, err)
	trequire.Equal(t, beforeCount+1, afterCount)
}

package access

import (
	"errors"
	"testing"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
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

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_member", "Member", authz.SystemRoleMember))
	ti.roles.On("UpdateRole", mock.Anything, mockidp.MockOrgID, "custom-builder", thirdpartyworkos.UpdateRoleOpts{
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
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_2", "custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_2",
		UserID:         "user_2",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "user2@test.com", "User 2", "user_2", "membership_2")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_3", "user3@test.com", "User 3", "user_3", "membership_3")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", authz.SystemRoleMember))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember(mockidp.MockOrgID, "membership_2", "user_2", authz.SystemRoleMember))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_3", mockMember(mockidp.MockOrgID, "membership_3", "user_3", "custom-builder"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember(mockidp.MockOrgID, "membership_workos_only", "user_workos_only", "custom-builder"))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-old")
	role, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:          roleID,
		Name:        &name,
		Description: &description,
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeProjectWrite), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}, {ResourceKind: "project", ResourceID: "project-2"}}},
			{Scope: string(authz.ScopeMCPConnect), Selectors: nil},
		},
		MemberIds: []string{"local_user_1", "local_user_2"},
	})
	require.NoError(t, err)
	require.Equal(t, roleID, role.ID)
	require.Equal(t, name, role.Name)
	require.Equal(t, description, role.Description)
	require.False(t, role.IsSystem)
	require.Equal(t, 3, role.MemberCount)
	require.Equal(t, mockRoleTimestamp, role.CreatedAt)
	require.NotEmpty(t, role.UpdatedAt)
	require.NotEqual(t, mockRoleTimestamp, role.UpdatedAt)
	require.Len(t, role.Grants, 2)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	require.Len(t, grants, 3)
}

func TestService_UpdateRole_SystemRole_MemberAssignment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// admin and member are system roles — WorkOS UpdateRole must NOT be called.
	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_member", "Member", authz.SystemRoleMember))
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "admin").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "admin",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "user2@test.com", "User 2", "user_2", "membership_2")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", authz.SystemRoleMember))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember(mockidp.MockOrgID, "membership_2", "user_2", authz.SystemRoleMember))

	role, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:        roleID,
		MemberIds: []string{"local_user_1"},
	})
	require.NoError(t, err)
	require.Equal(t, roleID, role.ID)
	require.True(t, role.IsSystem)
	require.Equal(t, 1, role.MemberCount)

	// WorkOS UpdateRole must NOT have been called for a system role.
	ti.roles.AssertNotCalled(t, "UpdateRole", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestService_UpdateRole_SystemRole_RejectsPropertyChanges(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_member", "Member", authz.SystemRoleMember))

	name := "Custom Name"
	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:   roleID,
		Name: &name,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "system role properties cannot be updated")

	description := "Custom description"
	_, err = ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:          roleID,
		Description: &description,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "system role properties cannot be updated")

	_, err = ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:     roleID,
		Grants: []*gen.RoleGrant{{Scope: string(authz.ScopeProjectRead)}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "system role properties cannot be updated")
}

func TestService_UpdateRole_SystemRole_RejectsNoopUpdate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))

	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID: roleID,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "system role update requires member_ids")
}

func TestService_UpdateRole_SystemRole_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_member", "Member", authz.SystemRoleMember))
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "admin").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "admin",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", authz.SystemRoleMember))

	role, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:        roleID,
		MemberIds: []string{"local_user_1"},
	})
	require.NoError(t, err)
	require.NotNil(t, role)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAccessRoleUpdate), record.Action)
	require.Equal(t, "admin", record.SubjectSlug)
}

func TestService_UpdateRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: "00000000-0000-0000-0000-000000000001"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_UpdateRole_WorkOSUpdateFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"))
	ti.roles.On("UpdateRole", mock.Anything, mockidp.MockOrgID, "custom-builder", thirdpartyworkos.UpdateRoleOpts{}).Return((*thirdpartyworkos.Role)(nil), errors.New("workos unavailable")).Times(3)

	role, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: roleID})
	require.NoError(t, err)
	require.Equal(t, roleID, role.ID)
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
	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Before Builder", "custom-builder", "Before description"))
	ti.roles.On("UpdateRole", mock.Anything, mockidp.MockOrgID, "custom-builder", thirdpartyworkos.UpdateRoleOpts{
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

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "user2@test.com", "User 2", "user_2", "membership_2")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom-builder"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-old")

	updated, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:          roleID,
		Name:        &name,
		Description: &description,
		Grants: []*gen.RoleGrant{{
			Scope:     string(authz.ScopeProjectWrite),
			Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}},
		}},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 2, updated.MemberCount)

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
	require.Equal(t, string(authz.ScopeProjectRead), beforeGrant["Scope"])
	beforeSelectors, ok := beforeGrant["Selectors"].([]any)
	require.True(t, ok)
	require.Len(t, beforeSelectors, 1)
	beforeSel, ok := beforeSelectors[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "project-old", beforeSel["ResourceID"])
	afterGrants, ok := afterSnapshot["Grants"].([]any)
	require.True(t, ok)
	require.Len(t, afterGrants, 1)
	afterGrant, ok := afterGrants[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, string(authz.ScopeProjectWrite), afterGrant["Scope"])
	afterSelectors, ok := afterGrant["Selectors"].([]any)
	require.True(t, ok)
	require.Len(t, afterSelectors, 1)
	afterSel, ok := afterSelectors[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "project-1", afterSel["ResourceID"])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

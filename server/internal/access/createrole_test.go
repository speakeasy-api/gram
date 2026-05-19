package access

import (
	"errors"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_CreateRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        "Custom Builder",
		Slug:        "org-custom-builder",
		Description: "Can build selected resources",
	}).Return(&thirdpartyworkos.Role{
		ID:          "role_1",
		Name:        "Custom Builder",
		Slug:        "org-custom-builder",
		Description: "Can build selected resources",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "org-custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "org-custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_2", "org-custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_2",
		UserID:         "user_2",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "org-custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_member", "Member", authz.SystemRoleMember))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "user2@test.com", "User 2", "user_2", "membership_2")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", authz.SystemRoleMember))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember("", "membership_2", "user_2", authz.SystemRoleMember))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember("", "membership_workos_only", "user_workos_only", authz.SystemRoleMember))

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeProjectRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}, {ResourceKind: "project", ResourceID: "project-2"}}},
			{Scope: string(authz.ScopeMCPConnect), Selectors: nil},
		},
		MemberIds: []string{"local_user_1", "local_user_2"},
	})
	require.NoError(t, err)
	require.Equal(t, "Custom Builder", role.Name)
	require.NotEmpty(t, role.ID)
	require.NotEqual(t, "role_1", role.ID)
	require.Equal(t, "Can build selected resources", role.Description)
	require.False(t, role.IsSystem)
	require.Equal(t, 2, role.MemberCount)
	require.NotEmpty(t, role.CreatedAt)
	require.NotEmpty(t, role.UpdatedAt)
	require.Len(t, role.Grants, 2)
	roundtrip, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: role.ID})
	require.NoError(t, err)
	require.Equal(t, role.ID, roundtrip.ID)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+role.ID))
	require.Len(t, grants, 3)
}

func TestService_CreateRole_WorkOSCreateFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        "Custom Builder",
		Slug:        "org-custom-builder",
		Description: "Can build selected resources",
	}).Return((*thirdpartyworkos.Role)(nil), errors.New("workos unavailable")).Times(3)

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeProjectRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Custom Builder", role.Name)
}

func TestService_CreateRole_WorkOSConflictFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        "Custom Builder",
		Slug:        "org-custom-builder",
		Description: "Can build selected resources",
	}).Return((*thirdpartyworkos.Role)(nil), &thirdpartyworkos.APIError{Method: "POST", Path: "/authorization/organizations/org_workos_test/roles", StatusCode: 409, Body: "role already exists"}).Once()

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeProjectRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Custom Builder", role.Name)
}

func TestService_CreateRole_WorkOSConflictUsesLocalRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_1", "Custom Builder", "org-custom-builder", "Can build selected resources"))
	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        "Custom Builder",
		Slug:        "org-custom-builder",
		Description: "Can build selected resources",
	}).Return((*thirdpartyworkos.Role)(nil), &thirdpartyworkos.APIError{Method: "POST", Path: "/authorization/organizations/org_workos_test/roles", StatusCode: 409, Body: "role already exists"}).Once()

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeProjectRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, roleID, role.ID)
	require.Equal(t, "Custom Builder", role.Name)
}

func TestService_CreateRole_RejectsEmptySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "!!!",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeProjectRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}}},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role name must contain at least one letter or digit")
}

func TestService_CreateRole_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleCreate)
	require.NoError(t, err)

	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        "Audit Builder",
		Slug:        "org-audit-builder",
		Description: "Tracks audit writes",
	}).Return(&thirdpartyworkos.Role{
		ID:          "role_audit",
		Name:        "Audit Builder",
		Slug:        "org-audit-builder",
		Description: "Tracks audit writes",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Once()

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Audit Builder",
		Description: "Tracks audit writes",
		Grants: []*gen.RoleGrant{{
			Scope:     string(authz.ScopeProjectRead),
			Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}},
		}},
	})
	require.NoError(t, err)
	require.NotNil(t, role)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleCreate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAccessRoleCreate), record.Action)
	require.Equal(t, "access_role", record.SubjectType)
	require.Equal(t, "Audit Builder", record.SubjectDisplay)
	require.Equal(t, "org-audit-builder", record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_CreateRole_LocalRoleWriteFailureDoesNotAssignMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	inspectConn, err := pgxpool.New(ctx, ti.conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(inspectConn.Close)

	ti.conn.Close()
	_, err = ti.service.roleMgr.CreateRole(ctx, authCtx.ActiveOrganizationID, mockidp.MockOrgID, accessAuditActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		DisplayName: authCtx.Email,
		Slug:        nil,
	}, &gen.CreateRolePayload{
		Name:        "Broken Builder",
		Description: "Will fail local write",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeProjectRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}}},
		},
		MemberIds: []string{"local_user_1", "local_user_2"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role transaction")

	grants, err := accessrepo.New(inspectConn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, "org-broken-builder").String(),
	})
	require.NoError(t, err)
	require.Empty(t, grants)
}

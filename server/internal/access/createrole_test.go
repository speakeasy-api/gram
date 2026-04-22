package access

import (
	"errors"
	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
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
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "member"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "member"),
		// user_workos_only has never logged into Gram — should not be counted
		mockMember(mockidp.MockOrgID, "membership_workos_only", "user_workos_only", "member"),
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

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "user2@test.com", "User 2", "user_2", "membership_2")

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeBuildRead), Resources: []string{"project-1", "project-2"}},
			{Scope: string(authz.ScopeMCPConnect), Resources: nil},
		},
		MemberIds: []string{"user_1", "user_2"},
	})
	require.NoError(t, err)
	require.Equal(t, "Custom Builder", role.Name)
	require.Equal(t, "Can build selected resources", role.Description)
	require.False(t, role.IsSystem)
	require.Equal(t, 2, role.MemberCount)
	require.Equal(t, mockRoleTimestamp, role.CreatedAt)
	require.Equal(t, mockRoleTimestamp, role.UpdatedAt)
	require.Len(t, role.Grants, 2)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "org-custom-builder"))
	require.Len(t, grants, 3)
}

func TestService_CreateRole_WorkOSCreateFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        "Custom Builder",
		Slug:        "org-custom-builder",
		Description: "Can build selected resources",
	}).Return((*thirdpartyworkos.Role)(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeBuildRead), Resources: []string{"project-1"}},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "create role in workos")
}

func TestService_CreateRole_ContinuesAfterConflictWhenRoleAlreadyExists(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	existingRole := mockRole("role_existing", "Custom Builder", "org-custom-builder", "Can build selected resources")
	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        "Custom Builder",
		Slug:        "org-custom-builder",
		Description: "Can build selected resources",
	}).Return((*thirdpartyworkos.Role)(nil), &thirdpartyworkos.APIError{Method: "POST", Path: "/authorization/organizations/org_workos_test/roles", StatusCode: 409, Body: "role already exists"}).Once()
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{existingRole}, nil).Once()

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeBuildRead), Resources: []string{"project-1"}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "role_existing", role.ID)
	require.Equal(t, "Custom Builder", role.Name)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "org-custom-builder"))
	require.Len(t, grants, 1)
	require.Equal(t, authCtx.ActiveOrganizationID, grants[0].OrganizationID)
}

func TestService_CreateRole_RejectsEmptySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "!!!",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeBuildRead), Resources: []string{"project-1"}},
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
			Scope:     string(authz.ScopeBuildRead),
			Resources: []string{"project-1"},
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

func TestService_CreateRole_GrantSyncFailureDoesNotAssignMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        "Broken Builder",
		Slug:        "org-broken-builder",
		Description: "Will fail grant sync",
	}).Run(func(mock.Arguments) {
		ti.conn.Close()
	}).Return(&thirdpartyworkos.Role{
		ID:          "role_1",
		Name:        "Broken Builder",
		Slug:        "org-broken-builder",
		Description: "Will fail grant sync",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Once()

	inspectConn, err := pgxpool.New(ctx, ti.conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(inspectConn.Close)

	_, err = ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Broken Builder",
		Description: "Will fail grant sync",
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeBuildRead), Resources: []string{"project-1"}},
		},
		MemberIds: []string{"user_1", "user_2"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "sync grants for created role")

	grants, err := accessrepo.New(inspectConn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, "org-broken-builder").String(),
	})
	require.NoError(t, err)
	require.Empty(t, grants)
}

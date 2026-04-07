package access

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	trequire "github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_CreateRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)

	ti.roles.On("CreateRole", mock.Anything, "org_workos_test", thirdpartyworkos.CreateRoleOpts{
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
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "member"),
		mockMember("org_workos_test", "membership_2", "user_2", "member"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "org-custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: "org_workos_test",
		RoleSlug:       "org-custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_2", "org-custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_2",
		UserID:         "user_2",
		OrganizationID: "org_workos_test",
		RoleSlug:       "org-custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(ScopeBuildRead), Resources: []string{"project-1", "project-2"}},
			{Scope: string(ScopeMCPConnect), Resources: nil},
		},
		MemberIds: []string{"user_1", "user_2"},
	})
	trequire.NoError(t, err)
	trequire.Equal(t, "Custom Builder", role.Name)
	trequire.Equal(t, "Can build selected resources", role.Description)
	trequire.False(t, role.IsSystem)
	trequire.Equal(t, 2, role.MemberCount)
	trequire.Equal(t, mockRoleTimestamp, role.CreatedAt)
	trequire.Equal(t, mockRoleTimestamp, role.UpdatedAt)
	trequire.Len(t, role.Grants, 2)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "org-custom-builder"))
	trequire.Len(t, grants, 3)
}

func TestService_CreateRole_WorkOSCreateFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("CreateRole", mock.Anything, "org_workos_test", thirdpartyworkos.CreateRoleOpts{
		Name:        "Custom Builder",
		Slug:        "org-custom-builder",
		Description: "Can build selected resources",
	}).Return((*thirdpartyworkos.Role)(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(ScopeBuildRead), Resources: []string{"project-1"}},
		},
	})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "create role in workos")
}

func TestService_CreateRole_ContinuesAfterConflictWhenRoleAlreadyExists(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)
	existingRole := mockRole("role_existing", "Custom Builder", "org-custom-builder", "Can build selected resources")
	ti.roles.On("CreateRole", mock.Anything, "org_workos_test", thirdpartyworkos.CreateRoleOpts{
		Name:        "Custom Builder",
		Slug:        "org-custom-builder",
		Description: "Can build selected resources",
	}).Return((*thirdpartyworkos.Role)(nil), &thirdpartyworkos.APIError{Method: "POST", Path: "/authorization/organizations/org_workos_test/roles", StatusCode: 409, Body: "role already exists"}).Once()
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{existingRole}, nil).Once()

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(ScopeBuildRead), Resources: []string{"project-1"}},
		},
	})
	trequire.NoError(t, err)
	trequire.Equal(t, "role_existing", role.ID)
	trequire.Equal(t, "Custom Builder", role.Name)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "org-custom-builder"))
	trequire.Len(t, grants, 1)
	trequire.Equal(t, authCtx.ActiveOrganizationID, grants[0].OrganizationID)
}

func TestService_CreateRole_RejectsEmptySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "!!!",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(ScopeBuildRead), Resources: []string{"project-1"}},
		},
	})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "role name must contain at least one letter or digit")
}

func TestService_CreateRole_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleCreate)
	trequire.NoError(t, err)

	ti.roles.On("CreateRole", mock.Anything, "org_workos_test", thirdpartyworkos.CreateRoleOpts{
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
			Scope:     string(ScopeBuildRead),
			Resources: []string{"project-1"},
		}},
	})
	trequire.NoError(t, err)
	trequire.NotNil(t, role)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleCreate)
	trequire.NoError(t, err)
	trequire.Equal(t, string(audit.ActionAccessRoleCreate), record.Action)
	trequire.Equal(t, "access_role", record.SubjectType)
	trequire.Equal(t, "Audit Builder", record.SubjectDisplay)
	trequire.Equal(t, "org-audit-builder", record.SubjectSlug)
	trequire.Nil(t, record.BeforeSnapshot)
	trequire.NotNil(t, record.AfterSnapshot)

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	trequire.NoError(t, err)
	trequire.Equal(t, "Audit Builder", afterSnapshot["Name"])
	grants, ok := afterSnapshot["Grants"].([]any)
	trequire.True(t, ok)
	trequire.Len(t, grants, 1)
	grant, ok := grants[0].(map[string]any)
	trequire.True(t, ok)
	trequire.Equal(t, string(ScopeBuildRead), grant["Scope"])
	resources, ok := grant["Resources"].([]any)
	trequire.True(t, ok)
	trequire.Len(t, resources, 1)
	trequire.Equal(t, "project-1", resources[0])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleCreate)
	trequire.NoError(t, err)
	trequire.Equal(t, beforeCount+1, afterCount)
}

func TestService_CreateRole_GrantSyncFailureDoesNotAssignMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)

	ti.roles.On("CreateRole", mock.Anything, "org_workos_test", thirdpartyworkos.CreateRoleOpts{
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
	trequire.NoError(t, err)
	t.Cleanup(inspectConn.Close)

	_, err = ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Broken Builder",
		Description: "Will fail grant sync",
		Grants: []*gen.RoleGrant{
			{Scope: string(ScopeBuildRead), Resources: []string{"project-1"}},
		},
		MemberIds: []string{"user_1", "user_2"},
	})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "sync grants for created role")

	grants, err := accessrepo.New(inspectConn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, "org-broken-builder").String(),
	})
	trequire.NoError(t, err)
	trequire.Empty(t, grants)
}

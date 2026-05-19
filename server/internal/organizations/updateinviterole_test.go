package organizations_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestService_UpdateInviteRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          "invitee@example.com",
		TokenHash:      "update-role-hash",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		RoleSlug:       conv.ToPGText("member"),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	ti.orgs.On("ListRoles", mock.Anything, mock.Anything).Return([]thirdpartyworkos.Role{
		{ID: "role-admin", Slug: "admin", Name: "Admin"},
	}, nil).Once()

	res, err := ti.service.UpdateInviteRole(ctx, &gen.UpdateInviteRolePayload{
		InvitationID: row.ID.String(),
		RoleID:       "role-admin",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, row.ID.String(), res.ID)
	require.NotNil(t, res.RoleSlug)
	require.Equal(t, "admin", *res.RoleSlug)

	updated, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, row.ID)
	require.NoError(t, err)
	require.True(t, updated.RoleSlug.Valid)
	require.Equal(t, "admin", updated.RoleSlug.String)
}

func TestService_UpdateInviteRole_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          "audit-role@example.com",
		TokenHash:      "audit-update-role-hash",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		RoleSlug:       conv.ToPGText("member"),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	ti.orgs.On("ListRoles", mock.Anything, mock.Anything).Return([]thirdpartyworkos.Role{
		{ID: "role-admin", Slug: "admin", Name: "Admin"},
	}, nil).Once()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationInviteRoleUpdate)
	require.NoError(t, err)

	res, err := ti.service.UpdateInviteRole(ctx, &gen.UpdateInviteRolePayload{
		InvitationID: row.ID.String(),
		RoleID:       "role-admin",
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationInviteRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationInviteRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, "organization_invitation", record.SubjectType)
	require.Equal(t, "audit-role@example.com", record.SubjectDisplay)
	require.Equal(t, "audit-role@example.com", record.SubjectSlug)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "member", beforeSnapshot["RoleSlug"])
	require.Equal(t, "admin", afterSnapshot["RoleSlug"])
}

func TestService_UpdateInviteRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	res, err := ti.service.UpdateInviteRole(ctx, &gen.UpdateInviteRolePayload{
		InvitationID: "00000000-0000-0000-0000-000000000000",
		RoleID:       "role-admin",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	require.Nil(t, res)
}

func TestService_UpdateInviteRole_WrongOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	require.NoError(t, orgrepo.New(ti.conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   "org-other-id",
		Name: "Other Org",
		Slug: "other-org",
	}))

	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: "org-other-id",
		Email:          "victim@example.com",
		TokenHash:      "wrong-org-update-role",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	res, err := ti.service.UpdateInviteRole(ctx, &gen.UpdateInviteRolePayload{
		InvitationID: row.ID.String(),
		RoleID:       "role-admin",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Nil(t, res)
}

func TestService_UpdateInviteRole_UnknownRoleID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          "unknown-role@example.com",
		TokenHash:      "unknown-role-update",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	ti.orgs.On("ListRoles", mock.Anything, mock.Anything).Return([]thirdpartyworkos.Role{
		{ID: "role-member", Slug: "member", Name: "Member"},
	}, nil).Once()

	res, err := ti.service.UpdateInviteRole(ctx, &gen.UpdateInviteRolePayload{
		InvitationID: row.ID.String(),
		RoleID:       "missing-role",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Nil(t, res)
}

func TestService_UpdateInviteRole_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          "rbac@example.com",
		TokenHash:      "rbac-update-role",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	ti.orgs.On("ListRoles", mock.Anything, mock.Anything).Return([]thirdpartyworkos.Role{
		{ID: "role-admin", Slug: "admin", Name: "Admin"},
	}, nil).Once()

	res, err := ti.service.UpdateInviteRole(ctx, &gen.UpdateInviteRolePayload{
		InvitationID: row.ID.String(),
		RoleID:       "role-admin",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.RoleSlug)
	require.Equal(t, "admin", *res.RoleSlug)
}

func TestService_UpdateInviteRole_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)

	res, err := ti.service.UpdateInviteRole(ctx, &gen.UpdateInviteRolePayload{
		InvitationID: "00000000-0000-0000-0000-000000000000",
		RoleID:       "role-admin",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Nil(t, res)
}

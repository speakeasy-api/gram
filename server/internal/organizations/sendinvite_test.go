package organizations_test

import (
	"fmt"
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
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestService_SendInvite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email: "test@example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, invite)
	require.Equal(t, "test@example.com", invite.Email)
	require.Equal(t, "pending", invite.State)
	require.NotNil(t, invite.InviterUserID)
	require.Equal(t, authCtx.UserID, *invite.InviterUserID)
	require.NotEmpty(t, invite.ID)
	require.NotEmpty(t, invite.CreatedAt)
}

func TestService_SendInvite_AllowsTrustedDomainEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	ti.orgs.On("GetOrganizationDomainPolicy", mock.Anything, mock.Anything).Return(&thirdpartyworkos.OrganizationDomainPolicy{
		Domains: []thirdpartyworkos.OrganizationDomain{
			{Domain: "example.com", State: thirdpartyworkos.OrganizationDomainStateVerified},
			{Domain: "Example.org.", State: thirdpartyworkos.OrganizationDomainStateVerified},
		},
	}, nil).Once()

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email: "Test@Example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, invite)
	require.Equal(t, "test@example.com", invite.Email)
}

func TestService_SendInvite_RejectsEmailOutsideTrustedDomains(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	ti.orgs.On("GetOrganizationDomainPolicy", mock.Anything, mock.Anything).Return(&thirdpartyworkos.OrganizationDomainPolicy{
		Domains: []thirdpartyworkos.OrganizationDomain{
			{Domain: "example.com", State: thirdpartyworkos.OrganizationDomainStateVerified},
			{Domain: "Example.org.", State: thirdpartyworkos.OrganizationDomainStateVerified},
		},
	}, nil).Once()

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email: "test@other.com",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "invite email must use one of this organization's trusted domains: example.com, example.org", oopsErr.Error())

	invites, listErr := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, listErr)
	require.Empty(t, invites.Invitations)
}

func TestService_SendInvite_WithRoleID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	roleID := "test-role"

	ti.orgs.On("ListRoles", mock.Anything, mock.Anything).Return([]thirdpartyworkos.Role{
		{ID: "test-role", Slug: "member", Name: "Member"},
	}, nil).Once()

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email:  "test@example.com",
		RoleID: &roleID,
	})
	require.NoError(t, err)
	require.NotNil(t, invite)
	require.Equal(t, "test@example.com", invite.Email)
	require.NotNil(t, invite.RoleSlug)
	require.Equal(t, "member", *invite.RoleSlug)
}

func TestService_SendInvite_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	roleID := "role-member"
	ti.orgs.On("ListRoles", mock.Anything, mock.Anything).Return([]thirdpartyworkos.Role{
		{ID: "role-member", Slug: "member", Name: "Member"},
	}, nil).Once()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationInviteCreate)
	require.NoError(t, err)

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email:  "audit@example.com",
		RoleID: &roleID,
	})
	require.NoError(t, err)
	require.NotNil(t, invite)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationInviteCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationInviteCreate)
	require.NoError(t, err)
	require.Equal(t, "organization_invitation", record.SubjectType)
	require.Equal(t, "audit@example.com", record.SubjectDisplay)
	require.Equal(t, "audit@example.com", record.SubjectSlug)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, "member", metadata["role_slug"])
}

func TestService_SendInvite_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "x@example.com"})
	require.NoError(t, err)
}

func TestService_SendInvite_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "x@example.com"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_SendInvite_DuplicatePendingEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "dup@example.com"})
	require.NoError(t, err)

	// Second invite to same email in same org should fail (partial unique index).
	_, err = ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "dup@example.com"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Equal(t, "an invitation is already pending for this email", oopsErr.Error())
}

func TestService_SendInvite_ExistingMemberEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := userrepo.New(ti.conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID:          "existing-member",
		Email:       "member@example.com",
		DisplayName: "Existing Member",
		PhotoUrl:    conv.ToPGText(""),
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText("existing-member"),
	})
	require.NoError(t, err)

	_, err = ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "member@example.com"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Equal(t, "user is already a member of this organization", oopsErr.Error())
}

func TestService_SendInvite_UnknownRoleID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	roleID := "nonexistent-role"

	ti.orgs.On("ListRoles", mock.Anything, mock.Anything).Return([]thirdpartyworkos.Role{
		{ID: "some-other-role", Slug: "member", Name: "Member"},
	}, nil).Once()

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email:  "test@example.com",
		RoleID: &roleID,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code, "unknown role should return bad request")
}

func TestService_SendInvite_EmailFailureRevokesInvite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithEmail(t)

	ti.loops.On("SendTransactional", mock.Anything, mock.Anything).Return(
		fmt.Errorf("loops API unavailable"),
	).Once()

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "noemail@example.com"})
	require.Error(t, err, "should fail when email delivery fails")

	// The invite should have been revoked so the user can retry.
	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.Empty(t, res.Invitations, "failed-to-send invite should be revoked")
}

func TestService_SendInvite_EmailSuccessReturnsInvite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithEmail(t)

	ti.loops.On("SendTransactional", mock.Anything, mock.Anything).Return(nil).Once()

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "emailok@example.com"})
	require.NoError(t, err)
	require.NotNil(t, invite)
	require.Equal(t, "emailok@example.com", invite.Email)
	require.Equal(t, "pending", invite.State)
}

func TestService_SendInvite_ExpiredInviteAllowsReinvite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create an invitation then expire it.
	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          "reinvite@example.com",
		TokenHash:      "reinvitehash",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)
	err = orgrepo.New(ti.conn).ExpireInvitationForTest(ctx, row.ID)
	require.NoError(t, err)

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "reinvite@example.com"})
	require.NoError(t, err)
	require.NotNil(t, invite)
	require.Equal(t, "reinvite@example.com", invite.Email)
	require.Equal(t, "pending", invite.State)
}

func TestService_SendInvite_ForbiddenWithGrantForDifferentOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, "org_other")})

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "x@example.com"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

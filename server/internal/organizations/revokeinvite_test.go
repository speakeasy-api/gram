package organizations_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
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

func TestService_RevokeInvite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// Create an invitation via SendInvite so we get a valid ID.
	ti.orgs.On("CreatePasswordlessSession", mock.Anything, mock.Anything).Return(&thirdpartyworkos.PasswordlessSession{
		ID: "pwl_1", Link: "https://stub.workos.com/passwordless/1",
	}, nil).Once()

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "test@example.com"})
	require.NoError(t, err)

	err = ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{InvitationID: invite.ID})
	require.NoError(t, err)

	// Verify it no longer appears in pending list.
	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.Empty(t, res.Invitations)
}

func TestService_RevokeInvite_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	err := ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{
		InvitationID: "00000000-0000-0000-0000-000000000000",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_RevokeInvite_WrongOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Insert an invitation for a different org directly in DB.
	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: "org-other-id",
		Email:          "victim@example.com",
		TokenHash:      "otherhash",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	err = ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{InvitationID: row.ID.String()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_RevokeInvite_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	// Create invitation via SendInvite.
	ti.orgs.On("CreatePasswordlessSession", mock.Anything, mock.Anything).Return(&thirdpartyworkos.PasswordlessSession{
		ID: "pwl_2", Link: "https://stub.workos.com/passwordless/2",
	}, nil).Once()
	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "x@example.com"})
	require.NoError(t, err)

	err = ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{InvitationID: invite.ID})
	require.NoError(t, err)
}

func TestService_RevokeInvite_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)

	err := ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{InvitationID: "any-invitation-id"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_RevokeInvite_ForbiddenWithGrantForDifferentOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, "org_other")})

	err := ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{InvitationID: "any-invitation-id"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

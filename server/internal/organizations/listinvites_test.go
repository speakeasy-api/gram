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

func TestService_ListInvites(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// Seed a pending invitation in the DB.
	ti.orgs.On("CreatePasswordlessSession", mock.Anything, mock.Anything).Return(&thirdpartyworkos.PasswordlessSession{
		ID: "pwl_1", Link: "https://stub.workos.com/passwordless/1",
	}, nil).Once()

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "invitee@example.com"})
	require.NoError(t, err)
	require.NotNil(t, invite)

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Invitations, 1)

	inv := res.Invitations[0]
	require.Equal(t, invite.ID, inv.ID)
	require.Equal(t, "invitee@example.com", inv.Email)
	require.Equal(t, "pending", inv.State)
	require.NotNil(t, inv.InviterUserID)
	require.Equal(t, authCtx.UserID, *inv.InviterUserID)
}

func TestService_ListInvites_ExcludesAccepted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create and accept an invitation directly in DB.
	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          "accepted@example.com",
		TokenHash:      "deadbeef",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)
	require.NoError(t, orgrepo.New(ti.conn).AcceptInvitation(ctx, row.ID))

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.Invitations, "accepted invitations should not appear")
}

func TestService_ListInvites_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestService_ListInvites_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Nil(t, res)
}

func TestService_ListInvites_ForbiddenWithGrantForDifferentOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, "org_other")})

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Nil(t, res)
}

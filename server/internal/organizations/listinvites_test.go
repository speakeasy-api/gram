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
	"github.com/stretchr/testify/require"
)

func TestService_ListInvites(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

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
	affected, err := orgrepo.New(ti.conn).AcceptInvitation(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.Invitations, "accepted invitations should not appear")
}

func TestService_ListInvites_ExcludesExpired(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create an invitation, then expire it by setting expires_at to the past.
	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          "expired@example.com",
		TokenHash:      "expiredhash",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	err = orgrepo.New(ti.conn).ExpireInvitationForTest(ctx, row.ID)
	require.NoError(t, err)

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.Invitations, "expired invitations should not appear")
}

func TestAcceptInvitation_RevokedReturnsZeroRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          "race-revoked@example.com",
		TokenHash:      "racerevokedhash",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	// Revoke first.
	err = orgrepo.New(ti.conn).RevokeInvitation(ctx, row.ID)
	require.NoError(t, err)

	// Accept should affect 0 rows.
	affected, err := orgrepo.New(ti.conn).AcceptInvitation(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), affected, "accepting a revoked invite should affect 0 rows")
}

func TestAcceptInvitation_ExpiredReturnsZeroRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	row, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          "race-expired@example.com",
		TokenHash:      "raceexpiredhash",
		InviterUserID:  conv.ToPGText(authCtx.UserID),
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	// Expire it manually.
	err = orgrepo.New(ti.conn).ExpireInvitationForTest(ctx, row.ID)
	require.NoError(t, err)

	// Accept should affect 0 rows because expired.
	affected, err := orgrepo.New(ti.conn).AcceptInvitation(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), affected, "accepting an expired invite should affect 0 rows")
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

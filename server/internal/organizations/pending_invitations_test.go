package organizations_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestAcceptPendingInvitationForUser_JoinsInvitedOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	_, invite := seedInviteCallbackInvite(t, ctx, ti, "platform-mcp-join", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleMember)
	seedInviteeUser(t, ctx, ti)
	require.NoError(t, userrepo.New(ti.conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{ID: "user_01INVITEE", WorkosID: conv.ToPGText("user_01INVITEE")}))
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", authCtx.ActiveOrganizationID, authz.SystemRoleMember).Return("membership_01INVITE", nil).Once()

	pending, err := ti.service.PendingInvitationsForEmail(ctx, "Invitee@Example.test")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, invite.ID.String(), pending[0].ID)
	require.Equal(t, authCtx.ActiveOrganizationID, pending[0].OrganizationID)

	joined, err := ti.service.AcceptPendingInvitationForUser(ctx, "user_01INVITEE", "invitee@example.test", invite.ID.String())
	require.NoError(t, err)
	require.Equal(t, pending[0], joined)

	member, err := orgrepo.New(ti.conn).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: "user_01INVITEE", OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.True(t, member)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)

	pending, err = ti.service.PendingInvitationsForEmail(ctx, "Invitee@Example.test")
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestAcceptPendingInvitationForUser_RefusesEmailOtherThanTheUsers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	_, invite := seedInviteCallbackInviteForEmail(t, ctx, ti, "platform-mcp-mismatch", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleMember, "someone-else@example.test")
	seedInviteeUser(t, ctx, ti)
	require.NoError(t, userrepo.New(ti.conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{ID: "user_01INVITEE", WorkosID: conv.ToPGText("user_01INVITEE")}))

	for _, verifiedEmail := range []string{"", "someone-else@example.test"} {
		_, err := ti.service.AcceptPendingInvitationForUser(ctx, "user_01INVITEE", verifiedEmail, invite.ID.String())
		require.Error(t, err)
	}

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", storedInvite.State)
}

func TestAcceptPendingInvitationForUser_RefusesInvitationForAnotherEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	_, invite := seedInviteCallbackInviteForEmail(t, ctx, ti, "platform-mcp-other", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleMember, "someone-else@example.test")
	seedInviteeUser(t, ctx, ti)
	require.NoError(t, userrepo.New(ti.conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{ID: "user_01INVITEE", WorkosID: conv.ToPGText("user_01INVITEE")}))

	pending, err := ti.service.PendingInvitationsForEmail(ctx, "Invitee@Example.test")
	require.NoError(t, err)
	require.Empty(t, pending)

	_, err = ti.service.AcceptPendingInvitationForUser(ctx, "user_01INVITEE", "invitee@example.test", invite.ID.String())
	require.Error(t, err)

	member, err := orgrepo.New(ti.conn).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: "user_01INVITEE", OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.False(t, member)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", storedInvite.State)
}

package organizations_test

import (
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

	pending, err := ti.service.PendingInvitationsForEmail(ctx, "Invitee@Example.test", "")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, invite.ID.String(), pending[0].ID)
	require.Equal(t, authCtx.ActiveOrganizationID, pending[0].OrganizationID)

	joined, err := ti.service.AcceptPendingInvitationForUser(ctx, "user_01INVITEE", "invitee@example.test", "", invite.ID.String())
	require.NoError(t, err)
	require.Equal(t, pending[0], joined)

	member, err := orgrepo.New(ti.conn).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: "user_01INVITEE", OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.True(t, member)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)

	pending, err = ti.service.PendingInvitationsForEmail(ctx, "Invitee@Example.test", "")
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
		_, err := ti.service.AcceptPendingInvitationForUser(ctx, "user_01INVITEE", verifiedEmail, "", invite.ID.String())
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

	pending, err := ti.service.PendingInvitationsForEmail(ctx, "Invitee@Example.test", "")
	require.NoError(t, err)
	require.Empty(t, pending)

	_, err = ti.service.AcceptPendingInvitationForUser(ctx, "user_01INVITEE", "invitee@example.test", "", invite.ID.String())
	require.Error(t, err)

	member, err := orgrepo.New(ti.conn).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: "user_01INVITEE", OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.False(t, member)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", storedInvite.State)
}

func TestAcceptPendingInvitationForUser_SSOCoveredEmailRequiresSSOSignIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	repo := orgrepo.New(ti.conn)
	org, err := repo.GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.NoError(t, repo.SetSSOEnabled(ctx, orgrepo.SetSSOEnabledParams{Enabled: pgtype.Bool{Bool: true, Valid: true}, WorkosID: org.WorkosID, WorkosLastEventID: conv.ToPGText("event_sso")}))
	require.NoError(t, repo.SetVerifiedDomains(ctx, orgrepo.SetVerifiedDomainsParams{ID: org.ID, VerifiedDomains: []string{"Example.test"}}))

	_, invite := seedInviteCallbackInvite(t, ctx, ti, "platform-mcp-sso", org.ID, authCtx.UserID, authz.SystemRoleMember)
	seedInviteeUser(t, ctx, ti)
	require.NoError(t, userrepo.New(ti.conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{ID: "user_01INVITEE", WorkosID: conv.ToPGText("user_01INVITEE")}))

	pending, err := ti.service.PendingInvitationsForEmail(ctx, "invitee@example.test", "")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.True(t, pending[0].RequiresSSO)

	_, err = ti.service.AcceptPendingInvitationForUser(ctx, "user_01INVITEE", "invitee@example.test", "", invite.ID.String())
	require.ErrorIs(t, err, organizations.ErrInvitationRequiresSSO)
	storedInvite, err := repo.GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", storedInvite.State)

	// An SSO sign-in through another organization's connection does not count.
	_, err = ti.service.AcceptPendingInvitationForUser(ctx, "user_01INVITEE", "invitee@example.test", "org_01OTHER", invite.ID.String())
	require.ErrorIs(t, err, organizations.ErrInvitationRequiresSSO)

	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", org.ID, authz.SystemRoleMember).Return("membership_01INVITE", nil).Once()
	_, err = ti.service.AcceptPendingInvitationForUser(ctx, "user_01INVITEE", "invitee@example.test", org.WorkosID.String, invite.ID.String())
	require.NoError(t, err)
	storedInvite, err = repo.GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
}

func TestAcceptInvitation_RechecksSSOUnderOrganizationLock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	_, invite := seedInviteCallbackInvite(t, ctx, ti, "platform-mcp-sso-race", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleMember)
	seedInviteeUser(t, ctx, ti)

	// The policy sees the organization read under the lock, so SSO turned on
	// after the invitation was listed still refuses the join.
	var lockedOrgID string
	err := ti.service.AcceptInvitationForTest(ctx, invite.ID, authCtx.ActiveOrganizationID, "user_01INVITEE", "user_01INVITEE", "invitee@example.test", func(org orgrepo.OrganizationMetadatum) bool {
		lockedOrgID = org.ID
		return true
	})
	require.ErrorIs(t, err, organizations.ErrInvitationRequiresSSO)
	require.Equal(t, authCtx.ActiveOrganizationID, lockedOrgID)

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", storedInvite.State)
	member, err := orgrepo.New(ti.conn).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: "user_01INVITEE", OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.False(t, member)
}

func TestInviteCallback_DisabledOrganizationGainsNoMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "disabled-org", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleMember)
	seedInviteeUser(t, ctx, ti)
	require.NoError(t, testrepo.New(ti.conn).DisableDelegationOrganizationFixture(ctx, authCtx.ActiveOrganizationID))

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Contains(t, recorder.Header().Get("Location"), "signin_error=")

	member, err := orgrepo.New(ti.conn).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: "user_01INVITEE", OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.False(t, member)
	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", storedInvite.State)
}

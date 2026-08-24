package organizations_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestInviteCallback_AdminInviteNotifiesTrialAdminAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "admin-accepted", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleAdmin)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", authCtx.ActiveOrganizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()
	seedInviteeUser(t, ctx, ti)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	var sessionCookie *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == constants.SessionCookie {
			sessionCookie = cookie
		}
	}
	require.NotNil(t, sessionCookie)
	require.Equal(t, constants.SessionCookieMaxAgeSeconds, sessionCookie.MaxAge)
	require.Equal(t, []adminAddedNotification{{
		organizationID: authCtx.ActiveOrganizationID,
		userID:         "user_01INVITEE",
	}}, ti.trial.adminAdded)

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
}

func TestInviteCallback_MemberInviteDoesNotNotifyTrialAdminAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "member-accepted", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleMember)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", authCtx.ActiveOrganizationID, authz.SystemRoleMember).Return("membership_01INVITE", nil).Once()
	seedInviteeUser(t, ctx, ti)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Empty(t, ti.trial.adminAdded)

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
}

func TestInviteCallback_TrialNotifierFailurePreservesInviteAcceptance(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "admin-notifier-failure", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleAdmin)
	ti.orgs.On("CreateOrganizationMembership", mock.Anything, "user_01INVITEE", authCtx.ActiveOrganizationID, authz.SystemRoleAdmin).Return("membership_01INVITE", nil).Once()
	ti.trial.adminAddedErr = errors.New("notify failed")
	seedInviteeUser(t, ctx, ti)

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Equal(t, []adminAddedNotification{{
		organizationID: authCtx.ActiveOrganizationID,
		userID:         "user_01INVITEE",
	}}, ti.trial.adminAdded)

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", storedInvite.State)
}

func TestInviteCallback_RevokedInviteDoesNotNotifyTrialAdminAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx := requireAuthContext(t, ctx)
	rawToken, invite := seedInviteCallbackInvite(t, ctx, ti, "admin-revoked", authCtx.ActiveOrganizationID, authCtx.UserID, authz.SystemRoleAdmin)
	require.NoError(t, orgrepo.New(ti.conn).RevokeInvitation(ctx, invite.ID))

	recorder := serveInviteCallback(t, ti, rawToken)
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Empty(t, ti.trial.adminAdded)

	storedInvite, err := orgrepo.New(ti.conn).GetInvitationByID(ctx, invite.ID)
	require.NoError(t, err)
	require.Equal(t, "revoked", storedInvite.State)
}

func requireAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	return authCtx
}

func seedInviteCallbackInvite(t *testing.T, ctx context.Context, ti *testInstance, name, organizationID, inviterUserID, roleSlug string) (string, orgrepo.OrganizationInvitation) {
	t.Helper()

	rawToken := "token-" + name
	invite, err := orgrepo.New(ti.conn).CreateInvitation(ctx, orgrepo.CreateInvitationParams{
		OrganizationID: organizationID,
		Email:          "invitee@example.test",
		TokenHash:      inviteTokenHash(rawToken),
		InviterUserID:  conv.ToPGText(inviterUserID),
		RoleSlug:       pgtype.Text{String: roleSlug, Valid: roleSlug != ""},
		ExpiresInDays:  7,
	})
	require.NoError(t, err)
	return rawToken, invite
}

func seedInviteeUser(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()

	require.NoError(t, testrepo.New(ti.conn).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID:          "user_01INVITEE",
		Email:       "invitee@example.test",
		DisplayName: "Invitee",
	}))
}

func serveInviteCallback(t *testing.T, ti *testInstance, rawToken string) *httptest.ResponseRecorder {
	t.Helper()

	mux := goahttp.NewMuxer()
	organizations.Attach(mux, ti.service)
	req := httptest.NewRequest(http.MethodGet, "/rpc/organizations.inviteCallback?invite_token="+rawToken, nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	return recorder
}

func inviteTokenHash(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

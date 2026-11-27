package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestAuthenticate_RejectsBlockedUser(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.UserID = "blocked-session-user"
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))

	for _, session := range []sessions.Session{
		{SessionID: "blocked-user-org-session", UserID: userInfo.UserID, ActiveOrganizationID: userInfo.Organizations[0].ID},
		{SessionID: "blocked-user-orgless-session", UserID: userInfo.UserID},
	} {
		require.NoError(t, instance.sessionManager.StoreSession(ctx, session))
	}
	require.NoError(t, usersrepo.New(instance.conn).SetUserBlacklisted(ctx, usersrepo.SetUserBlacklistedParams{
		Blacklisted: true,
		ID:          userInfo.UserID,
	}))

	for _, sessionID := range []string{"blocked-user-org-session", "blocked-user-orgless-session"} {
		_, err := instance.sessionManager.Authenticate(ctx, sessionID)
		require.Error(t, err, sessionID)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr, sessionID)
		require.Equal(t, oops.CodeForbidden, oopsErr.Code, sessionID)
	}
}

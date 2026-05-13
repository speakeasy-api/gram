package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestAuthenticate_AdminCanAccessNonMemberOrg verifies that an admin user whose
// session points to a customer org they are NOT a member of can still pass
// Authenticate. Without the admin bypass, HasAccessToOrganization (which only
// checks DB membership) would return false and Authenticate would 403.
func TestAuthenticate_AdminCanAccessNonMemberOrg(t *testing.T) {
	t.Parallel()

	userInfo := adminMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)

	require.NoError(t, instance.createTestUser(ctx, userInfo))
	require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))

	// Customer org exists in DB metadata but admin has no membership row.
	customerOrg := MockOrganizationEntry{
		ID:   "customer-org-auth-789",
		Name: "Customer Corp",
		Slug: "customer-corp",
	}
	require.NoError(t, instance.createTestOrganization(ctx, customerOrg, ""))

	session := sessions.Session{
		SessionID:            "admin-nonmember-auth",
		UserID:               userInfo.UserID,
		ActiveOrganizationID: customerOrg.ID,
		WorkOSSessionID:      "workos-sid-admin",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	// Prime the user info cache so IsAdmin returns true during Authenticate.
	// In production this is populated by Callback; here we call it directly.
	_, _, err := instance.identityResolver.GetUserInfo(ctx, userInfo.UserID)
	require.NoError(t, err)

	ctx, err = instance.sessionManager.Authenticate(ctx, session.SessionID)
	require.NoError(t, err, "admin must be able to authenticate into a non-member org")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, customerOrg.ID, authCtx.ActiveOrganizationID)
	require.True(t, authCtx.IsAdmin)
}

// TestAuthenticate_NonAdminCannotAccessNonMemberOrg is the inverse — a regular
// user whose session points to an org they don't belong to must be rejected.
func TestAuthenticate_NonAdminCannotAccessNonMemberOrg(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)

	require.NoError(t, instance.createTestUser(ctx, userInfo))
	require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))

	foreignOrg := MockOrganizationEntry{
		ID:   "foreign-org-no-access",
		Name: "Foreign Corp",
		Slug: "foreign-corp",
	}
	require.NoError(t, instance.createTestOrganization(ctx, foreignOrg, ""))

	session := sessions.Session{
		SessionID:            "nonadmin-foreign-auth",
		UserID:               userInfo.UserID,
		ActiveOrganizationID: foreignOrg.ID,
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	_, err := instance.sessionManager.Authenticate(ctx, session.SessionID)
	require.Error(t, err, "non-admin must not authenticate into a non-member org")

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

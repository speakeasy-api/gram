package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	authRepo "github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// A validated, unexpired support session lets a platform admin access a non-member org.
func TestAuthenticate_SupportAdminCanAccessNonMemberOrg(t *testing.T) {
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
		SessionID:             "admin-nonmember-auth",
		UserID:                userInfo.UserID,
		ActiveOrganizationID:  customerOrg.ID,
		WorkOSSessionID:       "workos-sid-admin",
		SupportOrganizationID: customerOrg.ID,
		SupportExpiresAt:      time.Now().Add(time.Hour),
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	// No manual cache priming — Authenticate must handle a cold cache.
	// HasAccessToOrganization populates the cache, and IsAdmin is checked after.
	ctx, err := instance.sessionManager.Authenticate(ctx, session.SessionID)
	require.NoError(t, err, "admin must be able to authenticate into a non-member org")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, customerOrg.ID, authCtx.ActiveOrganizationID)
	require.True(t, authCtx.IsAdmin)
	require.True(t, contextvalues.IsSupportSession(ctx))
}

func TestAuthenticate_BareAdminCannotAccessNonMemberOrg(t *testing.T) {
	t.Parallel()
	userInfo := adminMockUserInfo()
	userInfo.UserID = "bare-admin-user"
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	foreign := MockOrganizationEntry{ID: "bare-admin-foreign", Name: "Foreign", Slug: "bare-foreign"}
	require.NoError(t, instance.createTestOrganization(ctx, foreign, ""))
	session := sessions.Session{SessionID: "bare-admin-session", UserID: userInfo.UserID, ActiveOrganizationID: foreign.ID}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))
	_, err := instance.sessionManager.Authenticate(ctx, session.SessionID)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestAuthenticate_RejectsInvalidSupportSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		admin                 bool
		activeOrg, supportOrg string
	}{
		{name: "organization mismatch", admin: true, activeOrg: "target-a", supportOrg: "target-b"},
		{name: "non-admin", admin: false, activeOrg: "target-a", supportOrg: "target-a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			userInfo := defaultMockUserInfo()
			userInfo.UserID = "invalid-support-" + tt.name
			userInfo.Email = tt.name + "@example.com"
			userInfo.Admin = tt.admin
			ctx, instance := newTestAuthService(t, userInfo)
			require.NoError(t, instance.createTestUser(ctx, userInfo))
			session := sessions.Session{SessionID: "invalid-" + tt.name, UserID: userInfo.UserID, ActiveOrganizationID: tt.activeOrg, SupportOrganizationID: tt.supportOrg, SupportExpiresAt: time.Now().Add(time.Hour)}
			require.NoError(t, instance.sessionManager.StoreSession(ctx, session))
			_, err := instance.sessionManager.Authenticate(ctx, session.SessionID)
			require.Error(t, err)
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr)
			require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
		})
	}
}

func TestAuthenticate_RevokedAdminSupportSessionFailsImmediately(t *testing.T) {
	t.Parallel()
	userInfo := adminMockUserInfo()
	userInfo.UserID = "revoked-support-admin"
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	org := MockOrganizationEntry{ID: "revoked-support-org", Name: "Target", Slug: "revoked-target"}
	require.NoError(t, instance.createTestOrganization(ctx, org, ""))
	session := sessions.Session{SessionID: "revoked-support-session", UserID: userInfo.UserID, ActiveOrganizationID: org.ID, SupportOrganizationID: org.ID, SupportExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))
	_, err := instance.sessionManager.Authenticate(ctx, session.SessionID)
	require.NoError(t, err)

	require.NoError(t, authRepo.New(instance.conn).SetUserAdminFixture(ctx, authRepo.SetUserAdminFixtureParams{
		Admin:  false,
		UserID: userInfo.UserID,
	}))
	_, err = instance.sessionManager.Authenticate(ctx, session.SessionID)
	require.Error(t, err, "DB admin revocation must override stale identity cache")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
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
		ImpersonatorEmail:    "",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	_, err := instance.sessionManager.Authenticate(ctx, session.SessionID)
	require.Error(t, err, "non-admin must not authenticate into a non-member org")

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

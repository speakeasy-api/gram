package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_EnterDemo(t *testing.T) {
	t.Parallel()

	t.Run("enters demo org without membership", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))
		// Demo org metadata exists but the user is deliberately NOT a member.
		require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
			ID:   constants.DemoOrganizationID,
			Name: "Acme Demo Org",
			Slug: "acme-demo",
		}, ""))

		session := sessions.Session{
			SessionID:            uuid.NewString(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
			WorkOSSessionID:      "",
			ImpersonatorEmail:    "",
		}
		require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			AccountType:          "test",
			Email:                &userInfo.Email,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		result, err := instance.service.EnterDemo(ctx, &gen.EnterDemoPayload{SessionToken: nil})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, session.SessionID, result.SessionToken)

		// Request auth must accept the demo-org session despite the missing
		// membership row (the demo carve-out in sessions.Authenticate).
		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "authenticate demo session")
		got, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.Equal(t, constants.DemoOrganizationID, got.ActiveOrganizationID)
	})

	t.Run("demo org not seeded", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))

		session := sessions.Session{
			SessionID:            uuid.NewString(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
			WorkOSSessionID:      "",
			ImpersonatorEmail:    "",
		}
		require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			AccountType:          "test",
			Email:                &userInfo.Email,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		result, err := instance.service.EnterDemo(ctx, &gen.EnterDemoPayload{SessionToken: nil})
		require.Error(t, err)
		require.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	})

	t.Run("unauthenticated request", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		result, err := instance.service.EnterDemo(ctx, &gen.EnterDemoPayload{SessionToken: nil})
		require.Error(t, err)
		require.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("support session cannot enter demo", func(t *testing.T) {
		t.Parallel()

		userInfo := adminMockUserInfo()
		userInfo.UserID = "support-demo-admin"
		ctx, instance := newTestAuthService(t, userInfo)
		require.NoError(t, instance.createTestUser(ctx, userInfo))
		require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))

		session := sessions.Session{
			SessionID:             uuid.NewString(),
			UserID:                userInfo.UserID,
			ActiveOrganizationID:  userInfo.Organizations[0].ID,
			SupportOrganizationID: userInfo.Organizations[0].ID,
			SupportExpiresAt:      time.Now().Add(time.Hour),
		}
		require.NoError(t, instance.sessionManager.StoreSession(ctx, session))
		ctx, err := instance.sessionManager.Authenticate(ctx, session.SessionID)
		require.NoError(t, err)

		result, err := instance.service.EnterDemo(ctx, &gen.EnterDemoPayload{})
		require.Error(t, err)
		require.Nil(t, result)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeForbidden, oopsErr.Code)

		stored, err := instance.sessionManager.GetSession(ctx, session.SessionID)
		require.NoError(t, err)
		require.Equal(t, session.SessionID, stored.SessionID)
		require.Equal(t, session.ActiveOrganizationID, stored.ActiveOrganizationID)
		require.Equal(t, session.SupportOrganizationID, stored.SupportOrganizationID)
		require.True(t, session.SupportExpiresAt.Equal(stored.SupportExpiresAt))
	})
}

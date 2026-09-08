package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_SwitchScopes(t *testing.T) {
	t.Parallel()

	t.Run("successful organization switch", func(t *testing.T) {
		t.Parallel()

		userInfo := speakeasyMockUserInfo() // Has multiple organizations
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))

		// Seed org metadata so Authenticate can look it up after the switch
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		// Create and store a session first
		session := sessions.Session{
			SessionID:            uuid.NewString(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
			WorkOSSessionID:      "",
			ImpersonatorEmail:    "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			AccountType:          "test",
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		// Switch to the second organization
		newOrgID := userInfo.Organizations[1].ID
		payload := &gen.SwitchScopesPayload{
			OrganizationID: &newOrgID,
			ProjectID:      nil,
			SessionToken:   nil,
		}

		result, err := instance.service.SwitchScopes(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, session.SessionID, result.SessionToken)
		require.Equal(t, session.SessionID, result.SessionCookie)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, newOrgID, authCtx.ActiveOrganizationID, "incorrect active organization id after switch")
	})

	t.Run("switch to organization not in user's organizations", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))

		// Create and store a session first
		session := sessions.Session{
			SessionID:            uuid.NewString(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
			WorkOSSessionID:      "",
			ImpersonatorEmail:    "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			AccountType:          "test",
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		// Try to switch to an organization not in user's list
		invalidOrgID := "invalid-org-id"
		payload := &gen.SwitchScopesPayload{
			OrganizationID: &invalidOrgID,
			ProjectID:      nil,
			SessionToken:   nil,
		}

		result, err := instance.service.SwitchScopes(ctx, payload)
		require.Error(t, err)
		require.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	})

	t.Run("unauthenticated request", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// No auth context set
		payload := &gen.SwitchScopesPayload{
			OrganizationID: &userInfo.Organizations[0].ID,
			ProjectID:      nil,
			SessionToken:   nil,
		}

		result, err := instance.service.SwitchScopes(ctx, payload)
		require.Error(t, err)
		require.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("switch scopes without organization ID does nothing", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))

		// Seed org metadata so Authenticate can look it up
		err := instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID)
		require.NoError(t, err)

		// Create and store a session first
		session := sessions.Session{
			SessionID:            uuid.NewString(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
			WorkOSSessionID:      "",
			ImpersonatorEmail:    "",
		}
		err = instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			AccountType:          "test",
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		// Call with no organization ID
		payload := &gen.SwitchScopesPayload{
			OrganizationID: nil,
			ProjectID:      nil,
			SessionToken:   nil,
		}

		result, err := instance.service.SwitchScopes(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, session.SessionID, result.SessionToken)
		require.Equal(t, session.SessionID, result.SessionCookie)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, userInfo.Organizations[0].ID, authCtx.ActiveOrganizationID, "incorrect active organization id after switch")
	})

	t.Run("switch preserves WorkOS session metadata", func(t *testing.T) {
		t.Parallel()

		userInfo := speakeasyMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		session := sessions.Session{
			SessionID:            uuid.NewString(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
			WorkOSSessionID:      "workos-sid-abc123",
			ImpersonatorEmail:    "support@example.com",
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

		newOrgID := userInfo.Organizations[1].ID
		result, err := instance.service.SwitchScopes(ctx, &gen.SwitchScopesPayload{
			OrganizationID: &newOrgID,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		stored, err := instance.sessionManager.GetSession(ctx, session.SessionID)
		require.NoError(t, err)
		require.Equal(t, "workos-sid-abc123", stored.WorkOSSessionID, "WorkOSSessionID must survive SwitchScopes")
		require.Equal(t, "support@example.com", stored.ImpersonatorEmail, "ImpersonatorEmail must survive SwitchScopes")
		require.Equal(t, newOrgID, stored.ActiveOrganizationID)
	})

	t.Run("support session cannot switch organizations", func(t *testing.T) {
		t.Parallel()

		userInfo := adminMockUserInfo()
		userInfo.UserID = "support-switch-admin"
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

		newOrgID := "support-switch-target"
		result, err := instance.service.SwitchScopes(ctx, &gen.SwitchScopesPayload{OrganizationID: &newOrgID})
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

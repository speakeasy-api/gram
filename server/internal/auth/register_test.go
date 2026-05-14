package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_Register(t *testing.T) {
	t.Parallel()

	t.Run("successful register creates organization", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // User has no organizations
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))

		// Create and store a session with no active organization
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "", // No active organization
			WorkOSSessionID:      "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.RegisterPayload{
			OrgName:      "Test Organization",
			SessionToken: nil,
		}

		err = instance.service.Register(ctx, payload)
		require.NoError(t, err)
	})

	t.Run("register fails when user already has active organization", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session with active organization
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID, // Has active organization
			WorkOSSessionID:      "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.RegisterPayload{
			OrgName:      "Test Organization",
			SessionToken: nil,
		}

		err = instance.service.Register(ctx, payload)
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeInvalid, oopsErr.Code)
		require.Contains(t, err.Error(), "user already has an active organization")
	})

	t.Run("register fails when org name is empty", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // User has no organizations
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session with no active organization
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "", // No active organization
			WorkOSSessionID:      "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.RegisterPayload{
			OrgName:      "",
			SessionToken: nil,
		}

		err = instance.service.Register(ctx, payload)
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeInvalid, oopsErr.Code)
		require.Contains(t, err.Error(), "org name is required")
	})

	t.Run("register fails with invalid characters in org name", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // User has no organizations
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session with no active organization
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "", // No active organization
			WorkOSSessionID:      "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		testCases := []struct {
			name    string
			orgName string
		}{
			{"with special characters", "Test@Org!"},
			{"with brackets", "Test[Org]"},
			{"with slashes", "Test/Org\\"},
			{"with quotes", "Test\"Org'"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				payload := &gen.RegisterPayload{
					OrgName:      tc.orgName,
					SessionToken: nil,
				}

				err := instance.service.Register(ctx, payload)
				require.Error(t, err)

				var oopsErr *oops.ShareableError
				require.ErrorAs(t, err, &oopsErr)
				require.Equal(t, oops.CodeInvalid, oopsErr.Code)
				require.Contains(t, err.Error(), "organization name contains invalid characters")
			})
		}
	})

	t.Run("register allows valid characters in org name", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name    string
			orgName string
		}{
			{"alphanumeric", "TestOrg123"},
			{"with spaces", "Test Organization"},
			{"with hyphens", "Test-Organization"},
			{"with underscores", "Test_Organization"},
			{"mixed valid characters", "Test-Org_123 Demo"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				// Each subtest gets its own service, session and context so they
				// can run in parallel without racing on shared Redis state.
				userInfo := defaultMockUserInfo()
				userInfo.Organizations = []MockOrganizationEntry{}
				ctx, instance := newTestAuthService(t, userInfo)

				require.NoError(t, instance.createTestUser(ctx, userInfo))

				sessionID := "session-" + tc.name
				session := sessions.Session{
					SessionID:            sessionID,
					UserID:               userInfo.UserID,
					ActiveOrganizationID: "",
					WorkOSSessionID:      "",
				}
				err := instance.sessionManager.StoreSession(ctx, session)
				require.NoError(t, err)

				ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
					SessionID:            &sessionID,
					UserID:               session.UserID,
					ActiveOrganizationID: "",
					AccountType:          "test",
					Email:                &userInfo.Email,
				})

				err = instance.service.Register(ctx, &gen.RegisterPayload{
					OrgName:      tc.orgName,
					SessionToken: nil,
				})
				require.NoError(t, err)
			})
		}
	})

	t.Run("register fails when no session context", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Don't authenticate, so no session context
		payload := &gen.RegisterPayload{
			OrgName:      "Test Organization",
			SessionToken: nil,
		}

		err := instance.service.Register(ctx, payload)
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("register fails when session ID is nil", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Create auth context but with nil session ID
		authCtx := &contextvalues.AuthContext{
			UserID:               "test-user-123",
			ActiveOrganizationID: "",
			SessionID:            nil,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.RegisterPayload{
			OrgName:      "Test Organization",
			SessionToken: nil,
		}

		err := instance.service.Register(ctx, payload)
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("register preserves WorkOSSessionID", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // no orgs yet
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))

		session := sessions.Session{
			SessionID:            "workos-register-test",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "",
			WorkOSSessionID:      "workos-sid-register-456",
		}
		require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: "",
			AccountType:          "test",
			Email:                &userInfo.Email,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		err := instance.service.Register(ctx, &gen.RegisterPayload{
			OrgName: "Preserve Session Org",
		})
		require.NoError(t, err)

		stored, err := instance.sessionManager.GetSession(ctx, session.SessionID)
		require.NoError(t, err)
		require.Equal(t, "workos-sid-register-456", stored.WorkOSSessionID, "WorkOSSessionID must survive Register")
		require.NotEmpty(t, stored.ActiveOrganizationID, "should have an active org after Register")
	})
}

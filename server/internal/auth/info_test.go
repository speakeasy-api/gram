package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_Info(t *testing.T) {
	t.Parallel()

	t.Run("successful info request for regular user", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session first
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
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

		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, session.SessionID, result.SessionToken)
		require.Equal(t, session.SessionID, result.SessionCookie)
		require.Equal(t, session.ActiveOrganizationID, result.ActiveOrganizationID)
		require.Equal(t, userInfo.UserID, result.UserID)
		require.Equal(t, userInfo.Email, result.UserEmail)
		require.Equal(t, userInfo.Admin, result.IsAdmin)
		require.Equal(t, "test", result.GramAccountType)
		require.Len(t, result.Organizations, 1)

		org := result.Organizations[0]
		require.Equal(t, userInfo.Organizations[0].ID, org.ID)
		require.Equal(t, userInfo.Organizations[0].Name, org.Name)
		require.Equal(t, userInfo.Organizations[0].Slug, org.Slug)
		require.Len(t, org.Projects, 1) // Default project should be created
		require.Equal(t, "Default", org.Projects[0].Name)
	})

	t.Run("info request for admin user filters organizations", func(t *testing.T) {
		t.Parallel()

		userInfo := adminMockUserInfo()
		// Add additional organizations to test filtering
		userInfo.Organizations = append(userInfo.Organizations, MockOrganizationEntry{
			ID:                 "other-org-456",
			Name:               "Other Organization",
			Slug:               "other-org",
			SsoConnectionID:    nil,
			UserWorkspaceSlugs: []string{"other-workspace"},
		})
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session first
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID, // First org is active
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

		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.True(t, result.IsAdmin)
		// Admin should only see the active organization
		require.Len(t, result.Organizations, 1)
		require.Equal(t, userInfo.Organizations[0].ID, result.Organizations[0].ID)
	})

	t.Run("unauthenticated info request", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// No auth context set
		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.Error(t, err)
		require.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("info request without session ID", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Set up auth context without session ID
		authCtx := &contextvalues.AuthContext{
			SessionID:            nil, // No session ID
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
			AccountType:          "test",
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.Error(t, err)
		require.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("info request creates default project and environment", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session first
		session := sessions.Session{
			SessionID:            "test-session-id",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID,
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

		payload := &gen.InfoPayload{
			SessionToken: nil,
		}

		result, err := instance.service.Info(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Len(t, result.Organizations, 1)
		org := result.Organizations[0]
		require.Len(t, org.Projects, 1)

		project := org.Projects[0]
		require.Equal(t, "Default", project.Name)
		require.Equal(t, "default", string(project.Slug))
	})
}

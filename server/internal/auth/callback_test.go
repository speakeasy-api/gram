package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/gen/auth"
	"github.com/speakeasy-api/gram/internal/contextvalues"
)

func TestService_Callback(t *testing.T) {
	t.Parallel()

	t.Run("successful callback for regular user", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		payload := &gen.CallbackPayload{
			IDToken: "mock_token",
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)
		require.NotEmpty(t, result.SessionCookie)
		require.Equal(t, result.SessionToken, result.SessionCookie)
	})

	t.Run("successful callback for speakeasy user defaults to speakeasy-team", func(t *testing.T) {
		t.Parallel()

		userInfo := speakeasyMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		payload := &gen.CallbackPayload{
			IDToken: "mock_token",
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken, false)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, "speakeasy-team-123", authCtx.ActiveOrganizationID, "incorrect active organization id for speakeasy user")
	})

	t.Run("successful callback for admin with override", func(t *testing.T) {
		t.Parallel()

		userInfo := adminMockUserInfo()
		// Add multiple orgs including speakeasy-team
		userInfo.Organizations = append(userInfo.Organizations, MockOrganizationEntry{
			ID:                 "speakeasy-team-456",
			Name:               "Speakeasy Team",
			Slug:               "speakeasy-team",
			SsoConnectionID:    nil,
			UserWorkspaceSlugs: []string{"speakeasy-workspace"},
		})

		ctx, instance := newTestAuthService(t, userInfo)

		// Set admin override in context
		ctx = contextvalues.SetAdminOverrideInContext(ctx, "admin-org")

		payload := &gen.CallbackPayload{
			IDToken: "mock_token",
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken, false)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, "admin-org-123", authCtx.ActiveOrganizationID, "incorrect active organization id for admin override")
	})

	t.Run("user with no organizations returns successful redirect", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // No organizations
		ctx, instance := newTestAuthService(t, userInfo)

		payload := &gen.CallbackPayload{
			IDToken: "mock_token",
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)
		require.NotEmpty(t, result.SessionCookie)
		require.Equal(t, result.SessionToken, result.SessionCookie)
	})

	t.Run("invalid token returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Override the mock server to return an error for this test
		instance.mockAuthServer.Config.Handler = nil

		payload := &gen.CallbackPayload{
			IDToken: "invalid_token",
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Contains(t, result.Location, "signin_error=unexpected")
		require.Empty(t, result.SessionToken)
	})
}

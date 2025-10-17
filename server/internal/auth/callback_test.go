package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestService_Callback(t *testing.T) {
	t.Parallel()

	t.Run("successful callback for regular user", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		code := "mock_code"
		payload := &gen.CallbackPayload{
			Code: code,
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
		code := "mock_code"
		payload := &gen.CallbackPayload{
			Code: code,
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
		code := "mock_code"
		payload := &gen.CallbackPayload{
			Code: code,
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
		code := "mock_code"
		payload := &gen.CallbackPayload{
			Code: code,
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)
		require.NotEmpty(t, result.SessionCookie)
		require.Equal(t, result.SessionToken, result.SessionCookie)
	})

	t.Run("empty code returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		payload := &gen.CallbackPayload{
			Code: "",
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Contains(t, result.Location, "signin_error=")
		require.Empty(t, result.SessionToken)
	})

	t.Run("invalid code returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Override the mock server to return an error for this test
		instance.mockAuthServer.Config.Handler = nil
		code := "invalid_code"
		payload := &gen.CallbackPayload{
			Code: code,
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Contains(t, result.Location, "signin_error=")
		require.Empty(t, result.SessionToken)
	})

	t.Run("callback without state redirects to default URL", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		code := "mock_code"
		payload := &gen.CallbackPayload{
			Code: code,
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)
	})

	t.Run("callback with empty state redirects to default URL", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		code := "mock_code"

		// Create state with empty final_destination_url
		stateData := map[string]string{
			"final_destination_url": "",
		}
		stateJSON, err := json.Marshal(stateData)
		require.NoError(t, err)
		stateParam := base64.RawURLEncoding.EncodeToString(stateJSON)

		payload := &gen.CallbackPayload{
			Code:  code,
			State: &stateParam,
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)
	})

	t.Run("callback with state redirects to specified URL", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		code := "mock_code"
		redirectURL := "http://localhost:3000/dashboard/projects/my-project"

		// Create state with redirect URL
		stateData := map[string]string{
			"final_destination_url": redirectURL,
		}
		stateJSON, err := json.Marshal(stateData)
		require.NoError(t, err)
		stateParam := base64.RawURLEncoding.EncodeToString(stateJSON)

		payload := &gen.CallbackPayload{
			Code:  code,
			State: &stateParam,
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, "/dashboard/projects/my-project", result.Location)
		require.NotEmpty(t, result.SessionToken)
	})

	t.Run("callback with complex state URL redirects correctly", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		code := "mock_code"
		redirectURL := "http://localhost:3000/dashboard/projects/my-project?tab=settings&view=details"

		// Create state with complex redirect URL
		stateData := map[string]string{
			"final_destination_url": redirectURL,
		}
		stateJSON, err := json.Marshal(stateData)
		require.NoError(t, err)
		stateParam := base64.RawURLEncoding.EncodeToString(stateJSON)

		payload := &gen.CallbackPayload{
			Code:  code,
			State: &stateParam,
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, "/dashboard/projects/my-project?tab=settings&view=details", result.Location)
		require.NotEmpty(t, result.SessionToken)
	})

	t.Run("callback with invalid state redirects to default URL", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		code := "mock_code"

		// Create invalid state (not valid base64 JSON)
		invalidState := "not-valid-base64-json!!!"

		payload := &gen.CallbackPayload{
			Code:  code,
			State: &invalidState,
		}

		result, err := instance.service.Callback(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)
	})

	t.Run("callback preserves state through full flow", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		redirectURL := "http://localhost:3000/dashboard/environments/prod"

		// Simulate the full flow: Login -> Callback
		// Step 1: Call Login with redirect
		loginPayload := &gen.LoginPayload{
			Redirect: &redirectURL,
		}
		loginResult, err := instance.service.Login(ctx, loginPayload)
		require.NoError(t, err)
		require.NotNil(t, loginResult)

		// Step 2: Extract state parameter from login redirect URL
		stateFromLogin := extractStateFromURL(t, loginResult.Location)
		require.NotEmpty(t, stateFromLogin)

		// Step 3: Call Callback with the state
		callbackPayload := &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateFromLogin,
		}
		callbackResult, err := instance.service.Callback(ctx, callbackPayload)
		require.NoError(t, err)
		require.NotNil(t, callbackResult)

		// Step 4: Verify the callback redirects to the original redirect URL
		require.Equal(t, "/dashboard/environments/prod", callbackResult.Location)
		require.NotEmpty(t, callbackResult.SessionToken)
	})
}

// Helper function to extract state parameter from a URL string
func extractStateFromURL(t *testing.T, urlStr string) string {
	t.Helper()

	// Find the position of "state=" in the URL
	stateStart := 0
	for i := 0; i < len(urlStr); i++ {
		if i+6 <= len(urlStr) && urlStr[i:i+6] == "state=" {
			stateStart = i + 6
			break
		}
	}

	if stateStart == 0 {
		return ""
	}

	// Find the end of the state parameter (next & or end of string)
	stateEnd := len(urlStr)
	for i := stateStart; i < len(urlStr); i++ {
		if urlStr[i] == '&' {
			stateEnd = i
			break
		}
	}

	return urlStr[stateStart:stateEnd]
}

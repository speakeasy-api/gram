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

		// Seed user + orgs in DB so BuildUserInfoFromDB can find them
		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		stateParam := instance.stateWithNonce(ctx, t, "")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)
		require.NotEmpty(t, result.SessionCookie)
		require.Equal(t, result.SessionToken, result.SessionCookie)
	})

	t.Run("speakeasy user without explicit org uses first returned organization", func(t *testing.T) {
		t.Parallel()

		userInfo := speakeasyMockUserInfo()
		userInfo.Organizations[0], userInfo.Organizations[1] = userInfo.Organizations[1], userInfo.Organizations[0]
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		stateParam := instance.stateWithNonce(ctx, t, "")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, "other-org-123", authCtx.ActiveOrganizationID, "speakeasy user without state should use first returned org")
	})

	t.Run("callback final destination selects active organization", func(t *testing.T) {
		t.Parallel()

		userInfo := speakeasyMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		redirectURL := "https://dev.getgram.ai/other-org/projects/default"
		stateParam := instance.stateWithNonce(ctx, t, redirectURL)

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, "/other-org/projects/default", result.Location)
		require.NotEmpty(t, result.SessionToken)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, "other-org-123", authCtx.ActiveOrganizationID, "final destination org should select active org")
	})

	t.Run("non-admin admin override is ignored", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.UserID = "nonadmin-override-user"
		userInfo.Email = "nonadmin-override@example.com"
		userInfo.Organizations[0].ID = "nonadmin-primary-org"
		userInfo.Organizations = append(userInfo.Organizations, MockOrganizationEntry{
			ID:                 "override-org-123",
			Name:               "Override Organization",
			Slug:               "override-org",
			UserWorkspaceSlugs: []string{"override-workspace"},
		})

		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		ctx = contextvalues.SetAdminOverrideInContext(ctx, "override-org")

		stateParam := instance.stateWithNonce(ctx, t, "")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.SessionToken)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, "nonadmin-primary-org", authCtx.ActiveOrganizationID, "non-admin users should ignore admin override")
	})

	t.Run("successful callback for admin with override", func(t *testing.T) {
		t.Parallel()

		userInfo := adminMockUserInfo()
		userInfo.UserID = "admin-override-user-123"
		userInfo.Email = "admin-override@speakeasyapi.dev"
		userInfo.Organizations = append(userInfo.Organizations, MockOrganizationEntry{
			ID:                 "customer-org-123",
			Name:               "Customer Organization",
			Slug:               "customer-org",
			UserWorkspaceSlugs: []string{"customer-workspace"},
		})
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		ctx = contextvalues.SetAdminOverrideInContext(ctx, "customer-org")

		stateParam := instance.stateWithNonce(ctx, t, "")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
		require.NotEmpty(t, result.SessionToken)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, "customer-org-123", authCtx.ActiveOrganizationID, "incorrect active organization id for admin override")
	})

	t.Run("user with no organizations and assistants disposition auto-provisions org", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{}
		ctx, instance := newTestAuthService(t, userInfo)

		stateParam := instance.stateWithNonce(ctx, t, "/?disposition=assistants")

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.NotContains(t, result.Location, "signin_error=", "auto-provision should not surface a signin error")
		require.Contains(t, result.Location, "/projects/default/assistants/new?disposition=assistants", "auto-provisioned redirect should target the assistants/new page on the new org with the disposition marker")
		require.NotEmpty(t, result.SessionToken)
		require.Equal(t, result.SessionToken, result.SessionCookie)
	})

	t.Run("user with no organizations returns successful redirect", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{}
		ctx, instance := newTestAuthService(t, userInfo)

		stateParam := instance.stateWithNonce(ctx, t, "")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
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
		_ = ctx

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code: "",
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Contains(t, result.Location, "signin_error=")
		require.Empty(t, result.SessionToken)
	})

	t.Run("missing nonce returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		_ = instance

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code: "mock_code",
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Contains(t, result.Location, "signin_error=")
		require.Empty(t, result.SessionToken)
	})

	t.Run("forged nonce returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Craft state with a nonce that was never stored in Redis
		stateParam := instance.stateWithNonce(ctx, t, "")
		// Delete the nonce to simulate a forged/expired one
		require.NoError(t, instance.nonceStore.Delete(ctx, "auth:login_nonce:"+extractNonceFromState(t, stateParam)))

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Contains(t, result.Location, "signin_error=")
		require.Empty(t, result.SessionToken)
	})

	t.Run("nonce replay returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		stateParam := instance.stateWithNonce(ctx, t, "")

		// First callback succeeds
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotEmpty(t, result.SessionToken, "first callback should succeed")

		// Replaying the same state should fail — nonce was consumed
		result, err = instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.Contains(t, result.Location, "signin_error=", "replayed nonce should be rejected")
		require.Empty(t, result.SessionToken)
	})

	t.Run("invalid code returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Override the mock server to return an error for this test
		instance.mockAuthServer.Config.Handler = nil

		stateParam := instance.stateWithNonce(ctx, t, "")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "invalid_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Contains(t, result.Location, "signin_error=")
		require.Empty(t, result.SessionToken)
	})

	t.Run("callback with state redirects to specified URL", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		redirectURL := "http://localhost:3000/dashboard/projects/my-project"

		stateParam := instance.stateWithNonce(ctx, t, redirectURL)
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, "/dashboard/projects/my-project", result.Location)
		require.NotEmpty(t, result.SessionToken)
	})

	t.Run("callback with complex state URL redirects correctly", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		redirectURL := "http://localhost:3000/dashboard/projects/my-project?tab=settings&view=details"

		stateParam := instance.stateWithNonce(ctx, t, redirectURL)
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, "/dashboard/projects/my-project?tab=settings&view=details", result.Location)
		require.NotEmpty(t, result.SessionToken)
	})

	t.Run("callback preserves state through full flow", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		redirectURL := "http://localhost:3000/dashboard/environments/prod"

		// Simulate the full flow: Login -> Callback
		// Login generates and stores the nonce automatically.
		loginPayload := &gen.LoginPayload{
			Redirect: &redirectURL,
		}
		loginResult, err := instance.service.Login(ctx, loginPayload)
		require.NoError(t, err)
		require.NotNil(t, loginResult)

		stateFromLogin := extractStateFromURL(t, loginResult.Location)
		require.NotEmpty(t, stateFromLogin)

		callbackResult, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateFromLogin,
		})
		require.NoError(t, err)
		require.NotNil(t, callbackResult)

		require.Equal(t, "/dashboard/environments/prod", callbackResult.Location)
		require.NotEmpty(t, callbackResult.SessionToken)
	})
}

// extractStateFromURL extracts the state query parameter from a URL string.
func extractStateFromURL(t *testing.T, urlStr string) string {
	t.Helper()

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

	stateEnd := len(urlStr)
	for i := stateStart; i < len(urlStr); i++ {
		if urlStr[i] == '&' {
			stateEnd = i
			break
		}
	}

	return urlStr[stateStart:stateEnd]
}

// extractNonceFromState decodes a base64 state param and returns the nonce field.
func extractNonceFromState(t *testing.T, stateParam string) string {
	t.Helper()

	raw, err := base64.RawURLEncoding.DecodeString(stateParam)
	require.NoError(t, err)

	var state struct {
		Nonce string `json:"nonce"`
	}
	require.NoError(t, json.Unmarshal(raw, &state))
	return state.Nonce
}

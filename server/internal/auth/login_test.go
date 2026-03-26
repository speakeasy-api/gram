package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
)

func TestService_Login(t *testing.T) {
	t.Parallel()

	t.Run("successful login redirect", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		payload := &gen.LoginPayload{}
		result, err := instance.service.Login(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		require.True(t, strings.HasPrefix(result.Location, instance.authConfigs.SpeakeasyServerAddress))
		require.Contains(t, result.Location, "/v1/speakeasy_provider/login")
		require.Contains(t, result.Location, "return_url=")
		// The return URL is URL encoded, so decode to check
		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		returnURL := parsedURL.Query().Get("return_url")
		require.Contains(t, returnURL, "/rpc/auth.callback")
	})

	t.Run("login constructs correct return URL", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		payload := &gen.LoginPayload{}
		result, err := instance.service.Login(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		expectedReturnURL, err := url.JoinPath(instance.authConfigs.GramServerURL, "/rpc/auth.callback")
		require.NoError(t, err, "should construct expected return URL")

		// The return URL is URL encoded, so decode to check
		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		returnURL := parsedURL.Query().Get("return_url")
		require.Equal(t, expectedReturnURL, returnURL)
	})

	t.Run("login without redirect creates empty state", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		payload := &gen.LoginPayload{}
		result, err := instance.service.Login(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		stateParam := parsedURL.Query().Get("state")
		require.NotEmpty(t, stateParam, "state parameter should be present")

		// Decode and verify state structure
		stateBytes, err := base64.RawURLEncoding.DecodeString(stateParam)
		require.NoError(t, err)

		var state map[string]any
		err = json.Unmarshal(stateBytes, &state)
		require.NoError(t, err)
		require.Empty(t, state["final_destination_url"])
	})

	t.Run("login with redirect encodes state parameter", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		redirectURL := "http://localhost:3000/dashboard/projects/my-project"
		payload := &gen.LoginPayload{
			Redirect: &redirectURL,
		}
		result, err := instance.service.Login(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		stateParam := parsedURL.Query().Get("state")
		require.NotEmpty(t, stateParam, "state parameter should be present")

		// Decode and verify state contains redirect URL
		stateBytes, err := base64.RawURLEncoding.DecodeString(stateParam)
		require.NoError(t, err)

		var state map[string]any
		err = json.Unmarshal(stateBytes, &state)
		require.NoError(t, err)
		require.Equal(t, redirectURL, state["final_destination_url"])
	})

	t.Run("login with complex redirect URL encodes state correctly", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		redirectURL := "http://localhost:3000/dashboard/projects/my-project?tab=settings&view=details"
		payload := &gen.LoginPayload{
			Redirect: &redirectURL,
		}
		result, err := instance.service.Login(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		stateParam := parsedURL.Query().Get("state")
		require.NotEmpty(t, stateParam, "state parameter should be present")

		// Decode and verify state contains full redirect URL with query parameters
		stateBytes, err := base64.RawURLEncoding.DecodeString(stateParam)
		require.NoError(t, err)

		var state map[string]any
		err = json.Unmarshal(stateBytes, &state)
		require.NoError(t, err)
		require.Equal(t, redirectURL, state["final_destination_url"])
	})
}

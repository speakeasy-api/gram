package auth_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestService_Login(t *testing.T) {
	t.Parallel()

	t.Run("successful login redirect", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		result, err := instance.service.Login(ctx)
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

		result, err := instance.service.Login(ctx)
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
}

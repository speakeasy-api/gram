package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/supporthandoff"
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

		require.True(t, strings.HasPrefix(result.Location, instance.authConfigs.IDPBaseURL))
		require.Contains(t, result.Location, "/authorize")
		require.Contains(t, result.Location, "redirect_uri=")
		// The redirect_uri is URL encoded, so decode to check
		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		redirectURI := parsedURL.Query().Get("redirect_uri")
		require.Contains(t, redirectURI, "/rpc/auth.callback")
	})

	t.Run("login constructs correct return URL", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		payload := &gen.LoginPayload{}
		result, err := instance.service.Login(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, result)

		expectedRedirectURI, err := url.JoinPath(instance.authConfigs.GramServerURL, "/rpc/auth.callback")
		require.NoError(t, err, "should construct expected redirect URI")

		// The redirect_uri is URL encoded, so decode to check
		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		redirectURI := parsedURL.Query().Get("redirect_uri")
		require.Equal(t, expectedRedirectURI, redirectURI)
	})

	t.Run("login without redirect creates state with nonce", func(t *testing.T) {
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

		stateBytes, err := base64.RawURLEncoding.DecodeString(stateParam)
		require.NoError(t, err)

		var state map[string]any
		err = json.Unmarshal(stateBytes, &state)
		require.NoError(t, err)
		require.Empty(t, state["final_destination_url"])
		require.NotEmpty(t, state["nonce"], "state should contain a nonce")
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

		stateBytes, err := base64.RawURLEncoding.DecodeString(stateParam)
		require.NoError(t, err)

		var state map[string]any
		err = json.Unmarshal(stateBytes, &state)
		require.NoError(t, err)
		require.Equal(t, "/dashboard/projects/my-project", state["final_destination_url"])
		require.NotEmpty(t, state["nonce"], "state should contain a nonce")
	})

	t.Run("login drops a redirect that leaves the dashboard origin", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// AIS-428: browsers normalize the leading "/\" to "//", so storing this
		// verbatim would hand the callback a protocol-relative destination.
		for _, redirectURL := range []string{
			`/\attacker.example.net`,
			"//attacker.example.net/phish",
			"https://attacker.example.net/phish",
		} {
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

			stateBytes, err := base64.RawURLEncoding.DecodeString(stateParam)
			require.NoError(t, err)

			var state map[string]any
			err = json.Unmarshal(stateBytes, &state)
			require.NoError(t, err)
			require.Empty(t, state["final_destination_url"], "redirect %q must not reach the state param", redirectURL)
			require.NotEmpty(t, state["nonce"], "state should contain a nonce")
		}
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

		stateBytes, err := base64.RawURLEncoding.DecodeString(stateParam)
		require.NoError(t, err)

		var state map[string]any
		err = json.Unmarshal(stateBytes, &state)
		require.NoError(t, err)
		require.Equal(t, "/dashboard/projects/my-project?tab=settings&view=details", state["final_destination_url"])
		require.NotEmpty(t, state["nonce"], "state should contain a nonce")
	})

	t.Run("login without org name writes no signup intent", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		result, err := instance.service.Login(ctx, &gen.LoginPayload{})
		require.NoError(t, err)

		nonce := nonceFromLocation(t, result.Location)
		var intent map[string]any
		err = instance.nonceStore.Get(ctx, "auth:signup_intent:"+nonce, &intent)
		// ErrorIs, not Error: a bare error assertion would also pass if Redis
		// were unreachable, turning "nothing was written" into a claim the test
		// never actually checked.
		require.ErrorIs(t, err, redisCache.ErrCacheMiss, "ordinary login must not write a signup intent")
	})

	t.Run("login with org name stores the intent under the nonce", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		orgName := "Acme Inc"
		result, err := instance.service.Login(ctx, &gen.LoginPayload{OrgName: &orgName})
		require.NoError(t, err)

		nonce := nonceFromLocation(t, result.Location)
		// No tag: the nonce store marshals with msgpack, which keys by the Go
		// field name and ignores `json` tags, so the stored key is "OrgName".
		var intent struct {
			OrgName string
		}
		require.NoError(t, instance.nonceStore.Get(ctx, "auth:signup_intent:"+nonce, &intent))
		require.Equal(t, "Acme Inc", intent.OrgName)
	})

	t.Run("login with an invalid org name errors and stores nothing", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		orgName := "Acme Inc\t"
		result, err := instance.service.Login(ctx, &gen.LoginPayload{OrgName: &orgName})
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "organization name contains invalid characters")
	})

	t.Run("login with an over-long org name errors", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		orgName := strings.Repeat("a", 101)
		result, err := instance.service.Login(ctx, &gen.LoginPayload{OrgName: &orgName})
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "organization name is too long")
	})

	t.Run("login with a blank org name behaves like no org name", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		orgName := "   "
		result, err := instance.service.Login(ctx, &gen.LoginPayload{OrgName: &orgName})
		require.NoError(t, err, "a blank org name must not break an ordinary login")
		require.NotNil(t, result)

		nonce := nonceFromLocation(t, result.Location)
		var intent map[string]any
		err = instance.nonceStore.Get(ctx, "auth:signup_intent:"+nonce, &intent)
		require.ErrorIs(t, err, redisCache.ErrCacheMiss, "a blank org name must not write an intent")
	})

	t.Run("login from the sign-up page sets the AuthKit hints", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		orgName := "Acme Inc"
		email := "someone@example.com"
		result, err := instance.service.Login(ctx, &gen.LoginPayload{
			Redirect: nil,
			OrgName:  &orgName,
			Email:    &email,
		})
		require.NoError(t, err)

		parsed, err := url.Parse(result.Location)
		require.NoError(t, err)

		q := parsed.Query()
		require.Equal(t, "someone@example.com", q.Get("login_hint"))
		require.Equal(t, "sign-up", q.Get("screen_hint"))
	})

	t.Run("an ordinary login sets neither hint", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		result, err := instance.service.Login(ctx, &gen.LoginPayload{
			Redirect: nil,
			OrgName:  nil,
			Email:    nil,
		})
		require.NoError(t, err)

		parsed, err := url.Parse(result.Location)
		require.NoError(t, err)

		q := parsed.Query()
		require.False(t, q.Has("login_hint"))
		require.False(t, q.Has("screen_hint"))
	})

	// The org name is the marker that a login began on /sign-up, so it alone
	// selects the sign-up screen. An email only fills the field in.
	t.Run("an org name without an email still lands on the sign-up screen", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		orgName := "Acme Inc"
		result, err := instance.service.Login(ctx, &gen.LoginPayload{
			Redirect: nil,
			OrgName:  &orgName,
			Email:    nil,
		})
		require.NoError(t, err)

		parsed, err := url.Parse(result.Location)
		require.NoError(t, err)

		q := parsed.Query()
		require.False(t, q.Has("login_hint"))
		require.Equal(t, "sign-up", q.Get("screen_hint"))
	})

	t.Run("login with an invalid email errors and stores nothing", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		orgName := "Acme Inc"
		email := "not-an-address"
		result, err := instance.service.Login(ctx, &gen.LoginPayload{
			Redirect: nil,
			OrgName:  &orgName,
			Email:    &email,
		})
		require.Error(t, err)
		require.Nil(t, result)
	})
}

func TestService_LoginSupportHandoff(t *testing.T) {
	t.Parallel()

	ctx, instance := newTestAuthService(t, adminMockUserInfo())
	token, err := supporthandoff.NewIssuer(supporthandoff.NewStore(instance.nonceStore)).Issue(ctx, "support-target-org")
	require.NoError(t, err)

	result, err := instance.service.Login(ctx, &gen.LoginPayload{SupportHandoff: &token})
	require.NoError(t, err)
	nonce := nonceFromLocation(t, result.Location)

	parsed, err := url.Parse(result.Location)
	require.NoError(t, err)
	stateBytes, err := base64.RawURLEncoding.DecodeString(parsed.Query().Get("state"))
	require.NoError(t, err)
	require.NotContains(t, string(stateBytes), token)
	require.NotContains(t, string(stateBytes), "support-target-org")

	var intent struct{ OrganizationID string }
	require.NoError(t, instance.nonceStore.Get(ctx, "auth:support_intent:"+nonce, &intent))
	require.Equal(t, "support-target-org", intent.OrganizationID)

	_, err = instance.service.Login(ctx, &gen.LoginPayload{SupportHandoff: &token})
	require.Error(t, err, "handoff must be one-time")
}

func TestService_LoginStoresNormalizedUnicodeOrganizationName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "normalizes spaces", input: " Acme\u00a0 Inc ", want: "Acme Inc"},
		{name: "preserves non-Latin script", input: "アクメ株式会社", want: "アクメ株式会社"},
	}

	for _, tt := range tests {
		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		result, err := instance.service.Login(ctx, &gen.LoginPayload{OrgName: &tt.input})
		require.NoError(t, err, tt.name)

		nonce := nonceFromLocation(t, result.Location)
		var intent struct {
			OrgName string
		}
		require.NoError(t, instance.nonceStore.Get(ctx, "auth:signup_intent:"+nonce, &intent), tt.name)
		require.Equal(t, tt.want, intent.OrgName, tt.name)
	}
}

func TestService_LoginRejectsOrgNameWithTooFewLetters(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)

	orgName := "-----"
	result, err := instance.service.Login(ctx, &gen.LoginPayload{OrgName: &orgName})
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "organization name must contain at least 2 letters or numbers")
}

// nonceFromLocation pulls the nonce out of the state param on an authorization
// URL returned by Login.
func nonceFromLocation(t *testing.T, location string) string {
	t.Helper()

	parsed, err := url.Parse(location)
	require.NoError(t, err)

	raw, err := base64.RawURLEncoding.DecodeString(parsed.Query().Get("state"))
	require.NoError(t, err)

	var state struct {
		Nonce string `json:"nonce"`
	}
	require.NoError(t, json.Unmarshal(raw, &state))
	require.NotEmpty(t, state.Nonce)
	return state.Nonce
}

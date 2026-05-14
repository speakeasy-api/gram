package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
		require.Equal(t, redirectURL, state["final_destination_url"])
		require.NotEmpty(t, state["nonce"], "state should contain a nonce")
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
		require.Equal(t, redirectURL, state["final_destination_url"])
		require.NotEmpty(t, state["nonce"], "state should contain a nonce")
	})

	t.Run("login with redirect containing org slug sets organization_id", func(t *testing.T) {
		t.Parallel()

		workosOrgID := "org_workos_skip_selector"
		userInfo := defaultMockUserInfo()
		userInfo.Organizations[0].WorkosID = &workosOrgID
		ctx, instance := newTestAuthService(t, userInfo)

		// Seed org in DB so resolveWorkOSOrgIDForLogin can find it.
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		redirectURL := "/test-org/projects/default"
		ctx = auth.TestNonceBindingContext(ctx, testNonceBinding)
		result, err := instance.service.Login(ctx, &gen.LoginPayload{Redirect: &redirectURL})
		require.NoError(t, err)

		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		require.Equal(t, workosOrgID, parsedURL.Query().Get("organization_id"),
			"authorize URL should include organization_id when redirect contains a known org slug")
	})

	t.Run("login with admin override sets organization_id", func(t *testing.T) {
		t.Parallel()

		workosOrgID := "org_workos_admin_override"
		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Create the target org in DB (admin may not be a member).
		require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
			ID:       "admin-target-org",
			Name:     "Admin Target Org",
			Slug:     "admin-target",
			WorkosID: &workosOrgID,
		}, ""))

		ctx = contextvalues.SetAdminOverrideInContext(ctx, "admin-target")
		ctx = auth.TestNonceBindingContext(ctx, testNonceBinding)
		result, err := instance.service.Login(ctx, &gen.LoginPayload{})
		require.NoError(t, err)

		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		require.Equal(t, workosOrgID, parsedURL.Query().Get("organization_id"),
			"authorize URL should include organization_id from admin override")
	})

	t.Run("login without redirect or admin override omits organization_id", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		_ = instance

		ctx = auth.TestNonceBindingContext(ctx, testNonceBinding)
		result, err := instance.service.Login(ctx, &gen.LoginPayload{})
		require.NoError(t, err)

		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		require.Empty(t, parsedURL.Query().Get("organization_id"),
			"authorize URL should not include organization_id when no org context is available")
	})

	t.Run("login admin override takes priority over redirect URL", func(t *testing.T) {
		t.Parallel()

		workosAdminOrg := "org_workos_admin_prio"
		workosRedirectOrg := "org_workos_redirect_prio"
		userInfo := defaultMockUserInfo()
		userInfo.Organizations[0].WorkosID = &workosRedirectOrg
		ctx, instance := newTestAuthService(t, userInfo)

		// Seed both orgs in DB.
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}
		require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
			ID:       "admin-prio-org",
			Name:     "Admin Priority Org",
			Slug:     "admin-prio",
			WorkosID: &workosAdminOrg,
		}, ""))

		redirectURL := "/test-org/projects/default"
		ctx = contextvalues.SetAdminOverrideInContext(ctx, "admin-prio")
		ctx = auth.TestNonceBindingContext(ctx, testNonceBinding)
		result, err := instance.service.Login(ctx, &gen.LoginPayload{Redirect: &redirectURL})
		require.NoError(t, err)

		parsedURL, err := url.Parse(result.Location)
		require.NoError(t, err)
		require.Equal(t, workosAdminOrg, parsedURL.Query().Get("organization_id"),
			"admin override should take priority over redirect URL org slug")
	})
}

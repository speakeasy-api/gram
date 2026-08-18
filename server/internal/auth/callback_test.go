package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
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

		ctx, stateParam := instance.stateWithNonce(ctx, t, "")
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

	// Default org selection (fallback to first org) is covered by
	// TestE2E_Callback_MultipleOrgs and TestE2E_Callback_IDPOrgSelection.

	t.Run("callback final destination selects active organization", func(t *testing.T) {
		t.Parallel()

		userInfo := speakeasyMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		redirectURL := "http://localhost:3000/other-org/projects/default"
		ctx, stateParam := instance.stateWithNonce(ctx, t, redirectURL)

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

		ctx, stateParam := instance.stateWithNonce(ctx, t, "")
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

	t.Run("successful callback for admin with override to non-member org", func(t *testing.T) {
		t.Parallel()

		userInfo := adminMockUserInfo()
		userInfo.UserID = "admin-override-user-123"
		userInfo.Email = "admin-override@speakeasyapi.dev"
		// Admin is NOT a member of customer-org — it only exists in DB.
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}
		// Create customer org in DB without membership.
		require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
			ID:   "customer-org-123",
			Name: "Customer Organization",
			Slug: "customer-org",
		}, ""))

		ctx = contextvalues.SetAdminOverrideInContext(ctx, "customer-org")

		ctx, stateParam := instance.stateWithNonce(ctx, t, "")
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

	t.Run("admin override takes priority over state param org", func(t *testing.T) {
		t.Parallel()

		userInfo := adminMockUserInfo()
		userInfo.UserID = "admin-priority-user"
		userInfo.Email = "admin-priority@speakeasyapi.dev"
		// Admin is a member of state-org (via state param), but NOT override-org.
		userInfo.Organizations = append(userInfo.Organizations,
			MockOrganizationEntry{
				ID:                 "state-org-123",
				Name:               "State Organization",
				Slug:               "state-org",
				UserWorkspaceSlugs: []string{"state-workspace"},
			},
		)

		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}
		// Create override org in DB without membership.
		require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
			ID:   "override-org-456",
			Name: "Override Organization",
			Slug: "override-org",
		}, ""))

		// Set admin override to one org, but state param points to a different org.
		ctx = contextvalues.SetAdminOverrideInContext(ctx, "override-org")
		redirectURL := "http://localhost:3000/state-org/projects/default"
		ctx, stateParam := instance.stateWithNonce(ctx, t, redirectURL)

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, "override-org-456", authCtx.ActiveOrganizationID, "admin override should take priority over state param")
	})

	t.Run("state param takes priority over IDP org selection", func(t *testing.T) {
		t.Parallel()

		workosOrgID := "workos_org_idp_123"
		userInfo := defaultMockUserInfo()
		userInfo.UserID = "state-vs-idp-user"
		userInfo.Email = "state-vs-idp@example.com"
		userInfo.OrganizationID = workosOrgID // IDP selected this org
		userInfo.Organizations = []MockOrganizationEntry{
			{
				ID:                 "primary-org-000",
				Name:               "Primary Org",
				Slug:               "primary-org",
				UserWorkspaceSlugs: []string{"primary-workspace"},
			},
			{
				ID:                 "state-target-org",
				Name:               "State Target Org",
				Slug:               "state-target",
				UserWorkspaceSlugs: []string{"state-workspace"},
			},
			{
				ID:                 "idp-selected-org",
				Name:               "IDP Selected Org",
				Slug:               "idp-selected",
				WorkosID:           &workosOrgID,
				UserWorkspaceSlugs: []string{"idp-workspace"},
			},
		}

		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		// State param points to state-target org, IDP selected idp-selected org.
		redirectURL := "http://localhost:3000/state-target/projects/default"
		ctx, stateParam := instance.stateWithNonce(ctx, t, redirectURL)

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, "state-target-org", authCtx.ActiveOrganizationID, "state param should take priority over IDP org selection")
	})

	t.Run("admin state param resolves non-member org from DB", func(t *testing.T) {
		t.Parallel()

		userInfo := adminMockUserInfo()
		userInfo.UserID = "admin-state-nonmember"
		userInfo.Email = "admin-state-nonmember@speakeasyapi.dev"

		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}
		// Customer org exists in DB but admin is NOT a member.
		require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
			ID:   "customer-from-registry",
			Name: "Customer From Registry",
			Slug: "customer-registry",
		}, ""))

		// State param points to the non-member customer org (e.g. link from registry).
		redirectURL := "http://localhost:3000/customer-registry/projects/default"
		ctx, stateParam := instance.stateWithNonce(ctx, t, redirectURL)

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
		require.NoError(t, err, "load session after callback")
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok, "auth context should be set after callback")
		require.Equal(t, "customer-from-registry", authCtx.ActiveOrganizationID, "admin state param should resolve non-member org from DB")
	})

	t.Run("user with no organizations and assistants disposition auto-provisions org", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{}
		ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

		ctx, stateParam := instance.stateWithNonce(ctx, t, "/?disposition=assistants")

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

		ctx, stateParam := instance.stateWithNonce(ctx, t, "")
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
		ctx, stateParam := instance.stateWithNonce(ctx, t, "")
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

		ctx, stateParam := instance.stateWithNonce(ctx, t, "")

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

	t.Run("mismatched nonce binding cookie returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		// Seed nonce with one binding but inject a different binding into
		// the context (simulating a cookie from a different browser).
		ctx, stateParam := instance.stateWithNonce(ctx, t, "")
		ctx = auth.TestNonceBindingContext(ctx, "wrong-binding-value")

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Contains(t, result.Location, "signin_error=", "mismatched cookie should be rejected")
		require.Empty(t, result.SessionToken)
	})

	t.Run("missing nonce binding cookie returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		// Seed nonce normally but strip the binding from context
		// (simulating no cookie sent by the browser).
		_, stateParam := instance.stateWithNonce(ctx, t, "")
		// ctx has no nonce binding — simulates missing cookie

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Contains(t, result.Location, "signin_error=", "missing cookie should be rejected")
		require.Empty(t, result.SessionToken)
	})

	t.Run("invalid code returns error", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Override the mock server to return an error for this test
		instance.mockAuthServer.Config.Handler = nil

		ctx, stateParam := instance.stateWithNonce(ctx, t, "")
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

		ctx, stateParam := instance.stateWithNonce(ctx, t, redirectURL)
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Equal(t, "/dashboard/projects/my-project", result.Location)
		require.NotEmpty(t, result.SessionToken)
	})

	t.Run("callback refuses a state destination that leaves the dashboard origin", func(t *testing.T) {
		t.Parallel()

		// The state param is not signed, so an attacker can hand the callback any
		// destination directly — this is the check the browser ultimately depends
		// on. AIS-428 covered the first case: browsers read the leading "/\" as
		// "//" and treat the rest as a host.
		for _, redirectURL := range []string{
			`/\attacker.example.net`,
			`/\/attacker.example.net`,
			"//attacker.example.net/phish",
			"///attacker.example.net",
			"https://attacker.example.net/phish",
			"http://localhost:3000@attacker.example.net/",
			"attacker.example.net",
		} {
			userInfo := defaultMockUserInfo()
			ctx, instance := newTestAuthService(t, userInfo)

			ctx, stateParam := instance.stateWithNonce(ctx, t, redirectURL)
			result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
				Code:  "mock_code",
				State: &stateParam,
			})
			require.NoError(t, err)
			require.NotNil(t, result)

			require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location, "redirect %q must fall back to the sign-in URL", redirectURL)
			require.NotEmpty(t, result.SessionToken)
		}
	})

	t.Run("callback with complex state URL redirects correctly", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)
		redirectURL := "http://localhost:3000/dashboard/projects/my-project?tab=settings&view=details"

		ctx, stateParam := instance.stateWithNonce(ctx, t, redirectURL)
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
		// Inject a nonce binding into context (normally set by middleware cookie).
		ctx = auth.TestNonceBindingContext(ctx, testNonceBinding)

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

func TestService_CallbackAllowsWorkOSImpersonationWithoutState(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)

	require.NoError(t, instance.createTestUser(ctx, userInfo))
	for _, org := range userInfo.Organizations {
		require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
	}

	result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
		Code: "impersonation_code",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
	require.NotEmpty(t, result.SessionToken)
	require.Equal(t, result.SessionToken, result.SessionCookie)

	ctx, err = instance.sessionManager.Authenticate(ctx, result.SessionToken)
	require.NoError(t, err, "load impersonation session after callback")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context should be set after callback")
	require.Equal(t, userInfo.Organizations[0].ID, authCtx.ActiveOrganizationID)
}

func TestIdentityResolverCapturesWorkOSImpersonatorEmail(t *testing.T) {
	t.Parallel()

	ctx, instance := newTestAuthService(t, defaultMockUserInfo())

	idpUser, err := instance.identityResolver.ExchangeCodeForTokens(ctx, "impersonation_code")
	require.NoError(t, err)
	require.Equal(t, "support@example.com", idpUser.ImpersonatorEmail())
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

func TestService_Callback_SignupIntent(t *testing.T) {
	t.Parallel()

	t.Run("zero-org user with an intent gets the org created", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = nil
		// This path actually provisions an org, so it needs the harness that
		// wires a WorkOS client and backfills the user's workos_id — the admin
		// grant inside persistProvisionedOrganization requires one.
		ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

		ctx, stateParam := instance.stateWithSignupIntent(ctx, t, "", "Acme Inc")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotContains(t, result.Location, "signin_error=")
		require.NotEmpty(t, result.SessionToken)

		session, err := instance.sessionManager.GetSession(ctx, result.SessionToken)
		require.NoError(t, err)
		require.NotEmpty(t, session.ActiveOrganizationID, "the signup org must be active on the session")
		require.Equal(t, []string{session.ActiveOrganizationID}, instance.trialNotifier.trialStarted)

		org, err := orgRepo.New(instance.conn).GetOrganizationMetadata(ctx, session.ActiveOrganizationID)
		require.NoError(t, err)
		require.Equal(t, "Acme Inc", org.Name)
		require.True(t, org.Whitelisted, "signup orgs match register and clear the demo gate")
		require.Equal(t, "enterprise", org.GramAccountType)

		platformMCPEnabled, err := featurerepo.New(instance.conn).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
			OrganizationID: session.ActiveOrganizationID,
			FeatureName:    string(productfeatures.FeaturePlatformMCP),
		})
		require.NoError(t, err)
		require.True(t, platformMCPEnabled)

		trial, err := trialsRepo.New(instance.conn).GetTrial(ctx, session.ActiveOrganizationID)
		require.NoError(t, err)
		require.Equal(t, "enterprise", trial.Tier)

		// Callback is unauthenticated, so the audit actor cannot come from an
		// auth context. The email has to be threaded through provisioning.
		entry, err := audittest.LatestAuditLogByAction(ctx, instance.conn, audit.ActionOrganizationEnterpriseTrialArmed)
		require.NoError(t, err)
		require.NotEmpty(t, entry.ActorDisplay, "signup must attribute the trial to the user who signed up")
		require.Equal(t, userInfo.Email, entry.ActorDisplay)
	})

	t.Run("trial notifier failure does not fail signup", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = nil
		ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
		instance.trialNotifier.trialStartedErr = errors.New("notify failed")

		ctx, stateParam := instance.stateWithSignupIntent(ctx, t, "", "Acme Inc")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotEmpty(t, result.SessionToken)
		require.Len(t, instance.trialNotifier.trialStarted, 1)
	})

	t.Run("intent is consumed so it cannot be replayed", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = nil
		ctx, instance := newTestAuthService(t, userInfo)

		ctx, stateParam := instance.stateWithSignupIntent(ctx, t, "", "Acme Inc")
		nonce := extractNonceFromState(t, stateParam)

		_, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)

		var intent map[string]any
		err = instance.nonceStore.Get(ctx, "auth:signup_intent:"+nonce, &intent)
		// ErrorIs, not Error: a bare error assertion would also pass if Redis
		// were unreachable, turning "the key is gone" into a claim the test
		// never actually checked.
		require.ErrorIs(t, err, redisCache.ErrCacheMiss, "intent must be deleted after the callback")
	})

	t.Run("user who already has orgs ignores the intent and consumes it", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo() // has one org
		ctx, instance := newTestAuthService(t, userInfo)

		// Seed user + org in DB so BuildUserInfoFromDB finds a non-zero org
		// count — otherwise this is indistinguishable from the zero-org case
		// and would not actually exercise "already has orgs".
		require.NoError(t, instance.createTestUser(ctx, userInfo))
		for _, org := range userInfo.Organizations {
			require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
		}

		ctx, stateParam := instance.stateWithSignupIntent(ctx, t, "", "Acme Inc")
		nonce := extractNonceFromState(t, stateParam)

		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.NotContains(t, result.Location, "signin_error=")

		var intent map[string]any
		err = instance.nonceStore.Get(ctx, "auth:signup_intent:"+nonce, &intent)
		require.ErrorIs(t, err, redisCache.ErrCacheMiss, "intent must be consumed even when unused")

		orgs, err := orgRepo.New(instance.conn).ListOrganizationsForUser(ctx, conv.ToPGText(userInfo.UserID))
		require.NoError(t, err)
		require.Len(t, orgs, 1, "no second org may be created for a user who already has one")
	})

	t.Run("intent wins over the assistants disposition", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = nil
		ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

		ctx, stateParam := instance.stateWithSignupIntent(ctx, t, "/?disposition=assistants", "Acme Inc")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)

		session, err := instance.sessionManager.GetSession(ctx, result.SessionToken)
		require.NoError(t, err)

		org, err := orgRepo.New(instance.conn).GetOrganizationMetadata(ctx, session.ActiveOrganizationID)
		require.NoError(t, err)
		require.Equal(t, "Acme Inc", org.Name, "the typed name must beat a generated assistants name")
	})

	t.Run("zero-org user without an intent is unaffected", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = nil
		ctx, instance := newTestAuthService(t, userInfo)

		ctx, stateParam := instance.stateWithNonce(ctx, t, "")
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)

		session, err := instance.sessionManager.GetSession(ctx, result.SessionToken)
		require.NoError(t, err)
		require.Empty(t, session.ActiveOrganizationID, "no intent means no org, as today")
	})
}

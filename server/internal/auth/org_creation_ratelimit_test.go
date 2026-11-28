package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func disableUserOrganizations(t *testing.T, ctx context.Context, ti *testInstance, userID string) {
	t.Helper()
	queries := orgRepo.New(ti.conn)
	orgs, err := queries.ListOrganizationsForUser(ctx, conv.ToPGText(userID))
	require.NoError(t, err)
	for _, org := range orgs {
		require.NoError(t, queries.DeleteOrganizationUserRelationship(ctx, orgRepo.DeleteOrganizationUserRelationshipParams{
			OrganizationID: org.ID,
			UserID:         conv.ToPGText(userID),
		}))
	}
	require.NoError(t, ti.sessionManager.InvalidateUserInfoCache(ctx, userID))
}

func registerContextForUser(t *testing.T, ctx context.Context, ti *testInstance, userInfo *MockUserInfo, sessionID string) context.Context {
	t.Helper()
	session := sessions.Session{
		SessionID:            sessionID,
		UserID:               userInfo.UserID,
		ActiveOrganizationID: "",
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "",
	}
	require.NoError(t, ti.sessionManager.StoreSession(ctx, session))
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: "",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		AccountType:          "test",
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	})
}

func TestRegister_RateLimitedPerUser(t *testing.T) {
	t.Parallel()

	userA := &MockUserInfo{
		UserID:        "user-ratelimit-a-" + uuid.NewString(),
		Email:         "user-a@example.com",
		Organizations: []MockOrganizationEntry{},
	}
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userA)

	userB := &MockUserInfo{
		UserID:        "user-ratelimit-b-" + uuid.NewString(),
		Email:         "user-b@example.com",
		Organizations: []MockOrganizationEntry{},
	}
	require.NoError(t, instance.createTestUser(ctx, userB))
	require.NoError(t, userRepo.New(instance.conn).OverwriteUserWorkosID(ctx, userRepo.OverwriteUserWorkosIDParams{
		ID:       userB.UserID,
		WorkosID: conv.ToPGText(userB.UserID),
	}))

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	instance.service.WithOrgCreationLimiter(ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		"test-org-create-"+uuid.NewString(),
		auth.OrgCreationRate,
		ratelimit.WithMetrics(testenv.NewMeterProvider(t)),
	))

	// Requests up to the per-hour burst (3) succeed for User A.
	for i := range auth.OrgCreationRate.Burst {
		sessionID := fmt.Sprintf("%s-a-%d", t.Name(), i)
		regCtx := registerContextForUser(t, ctx, instance, userA, sessionID)
		err := instance.service.Register(regCtx, &gen.RegisterPayload{
			OrgName:      fmt.Sprintf("Org %d", i+1),
			SessionToken: nil,
		})
		require.NoError(t, err, "attempt %d within burst should succeed", i+1)
		disableUserOrganizations(t, ctx, instance, userA.UserID)
	}

	// The (N+1)th attempt for User A within the window is rejected with 429.
	blockedSessionID := t.Name() + "-a-blocked"
	blockedCtx := registerContextForUser(t, ctx, instance, userA, blockedSessionID)
	err = instance.service.Register(blockedCtx, &gen.RegisterPayload{
		OrgName:      "Blocked Org",
		SessionToken: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeRateLimitExceeded, oopsErr.Code)
	require.ErrorIs(t, err, auth.ErrOrgCreationRateLimited)
	require.Equal(t, "Too many organization creation attempts. Please try again later.", oopsErr.Error())

	stored, err := instance.sessionManager.GetSession(ctx, blockedSessionID)
	require.NoError(t, err)
	require.Empty(t, stored.ActiveOrganizationID)

	enabledOrgs, err := orgRepo.New(instance.conn).ListOrganizationsForUser(ctx, conv.ToPGText(userA.UserID))
	require.NoError(t, err)
	require.Empty(t, enabledOrgs, "a rate-limited registration must not create an organization")

	// Verify the HTTP surface returns 429 Too Many Requests JSON (not a 500).
	httpSessionID := t.Name() + "-a-http-blocked"
	require.NoError(t, instance.sessionManager.StoreSession(ctx, sessions.Session{
		SessionID:            httpSessionID,
		UserID:               userA.UserID,
		ActiveOrganizationID: "",
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "",
	}))
	mux := goahttp.NewMuxer()
	auth.Attach(mux, instance.service)

	body, err := json.Marshal(map[string]string{"org_name": "HTTP Blocked Org"})
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/auth.register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Session", httpSessionID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
	require.Equal(t, string(oops.CodeRateLimitExceeded), rec.Header().Get("goa-error"))
	var errResp struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, string(oops.CodeRateLimitExceeded), errResp.Name)
	require.Equal(t, "Too many organization creation attempts. Please try again later.", errResp.Message)

	// A different user has an independent bucket and is not affected.
	userBCtx := registerContextForUser(t, ctx, instance, userB, t.Name()+"-b-0")
	require.NoError(t, instance.service.Register(userBCtx, &gen.RegisterPayload{
		OrgName:      "User B Org",
		SessionToken: nil,
	}))
	userBOrgs, err := orgRepo.New(instance.conn).ListOrganizationsForUser(ctx, conv.ToPGText(userB.UserID))
	require.NoError(t, err)
	require.Len(t, userBOrgs, 1)
}

func TestCallback_SignupRateLimitedPerUser(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID:        "user-signup-rl-" + uuid.NewString(),
		Email:         "signup-rl@example.com",
		Organizations: []MockOrganizationEntry{},
	}
	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{},
		orgs:    map[string]*workos.Organization{},
	}
	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	inst.service.WithOrgCreationLimiter(ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		"test-signup-rl-"+uuid.NewString(),
		ratelimit.PerHour(1),
	))

	// First signup succeeds.
	firstOrg := "First Signup Org"
	ctx, login1 := inst.loginWithNonceBinding(ctx, t, &gen.LoginPayload{OrgName: &firstOrg})
	result1 := inst.callbackFromLogin(ctx, t, login1)
	require.NotEmpty(t, result1.SessionToken)

	disableUserOrganizations(t, ctx, &inst.testInstance, userInfo.UserID)

	// Second signup within the window redirects back with org_creation_rate_limited.
	secondOrg := "Second Signup Org"
	ctx, login2 := inst.loginWithNonceBinding(ctx, t, &gen.LoginPayload{OrgName: &secondOrg})
	state2 := nonceStateFromLocation(t, login2.Location)
	result2, err := inst.service.Callback(ctx, &gen.CallbackPayload{
		Code:  new("mock_code"),
		State: &state2,
	})
	require.NoError(t, err)
	require.NotNil(t, result2)
	require.Empty(t, result2.SessionToken)
	require.Empty(t, result2.SessionCookie)

	parsed, err := url.Parse(result2.Location)
	require.NoError(t, err)
	require.Equal(t, "org_creation_rate_limited", parsed.Query().Get("signin_error"))
	require.Len(t, fetcher.createdOrgs, 1, "rate-limited signup must not provision a WorkOS organization")
}

func TestCallback_AssistantsAutoProvisionRateLimitedPerUser(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID:        "user-assistants-rl-" + uuid.NewString(),
		Email:         "assistants-rl@example.com",
		Organizations: []MockOrganizationEntry{},
	}
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	instance.service.WithOrgCreationLimiter(ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		"test-assistants-rl-"+uuid.NewString(),
		ratelimit.PerHour(1),
	))

	// First assistants callback succeeds.
	ctx1, state1 := instance.stateWithNonce(ctx, t, "/?disposition=assistants")
	result1, err := instance.service.Callback(ctx1, &gen.CallbackPayload{
		Code:  new("mock_code"),
		State: &state1,
	})
	require.NoError(t, err)
	require.NotContains(t, result1.Location, "signin_error=")
	require.NotEmpty(t, result1.SessionToken)

	disableUserOrganizations(t, ctx, instance, userInfo.UserID)

	// Second assistants callback within the window redirects with org_creation_rate_limited.
	ctx2, state2 := instance.stateWithNonce(ctx, t, "/?disposition=assistants")
	result2, err := instance.service.Callback(ctx2, &gen.CallbackPayload{
		Code:  new("mock_code"),
		State: &state2,
	})
	require.NoError(t, err)
	require.NotNil(t, result2)
	require.Empty(t, result2.SessionToken)
	require.Empty(t, result2.SessionCookie)

	parsed, err := url.Parse(result2.Location)
	require.NoError(t, err)
	require.Equal(t, "org_creation_rate_limited", parsed.Query().Get("signin_error"))
}

func TestProvisionPlatformMCPWorkspace_RateLimitedPerUser(t *testing.T) {
	t.Parallel()

	userInfo := platformMCPWorkspaceUser()
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	instance.service.WithOrgCreationLimiter(ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		"test-platform-mcp-rl-"+uuid.NewString(),
		ratelimit.PerHour(1),
	))

	_, _, err = instance.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "First Workspace", "Claude Code")
	require.NoError(t, err)
	require.Len(t, instance.trialNotifier.trialStarted, 1)

	disableUserOrganizations(t, ctx, instance, userInfo.UserID)

	_, _, err = instance.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "Second Workspace", "Claude Code")
	require.ErrorIs(t, err, auth.ErrOrgCreationRateLimited)
	require.Len(t, instance.trialNotifier.trialStarted, 1)

	orgs, err := orgRepo.New(instance.conn).ListOrganizationsForUser(ctx, conv.ToPGText(userInfo.UserID))
	require.NoError(t, err)
	require.Empty(t, orgs)
}

func TestRegister_NilLimiterAllowsCreation(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID:        "user-nil-rl-" + uuid.NewString(),
		Email:         "nil-rl@example.com",
		Organizations: []MockOrganizationEntry{},
	}
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	instance.service.WithOrgCreationLimiter(nil)

	for i := range auth.OrgCreationRate.Burst + 1 {
		regCtx := registerContextForUser(t, ctx, instance, userInfo, fmt.Sprintf("%s-%d", t.Name(), i))
		require.NoError(t, instance.service.Register(regCtx, &gen.RegisterPayload{
			OrgName:      fmt.Sprintf("Unthrottled Org %d", i+1),
			SessionToken: nil,
		}))
		disableUserOrganizations(t, ctx, instance, userInfo.UserID)
	}
}

func TestRegister_UnavailableRedisLimiterFailsOpen(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID:        "user-down-rl-" + uuid.NewString(),
		Email:         "down-rl@example.com",
		Organizations: []MockOrganizationEntry{},
	}
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	instance.service.WithOrgCreationLimiter(ratelimit.New(
		ratelimit.NewRedisStore(brokenRedis(t)),
		"test-down-rl-"+uuid.NewString(),
		ratelimit.PerHour(1),
	))

	regCtx := registerContextForUser(t, ctx, instance, userInfo, t.Name())
	require.NoError(t, instance.service.Register(regCtx, &gen.RegisterPayload{
		OrgName:      "Fail Open Org",
		SessionToken: nil,
	}))
}

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	adminserver "github.com/speakeasy-api/gram/server/gen/http/admin/server"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type adminOrganizationFeaturesResponse = adminserver.GetOrganizationFeaturesResponseBody

var adminGenericWritableFeatures = []productfeatures.Feature{
	productfeatures.FeatureLogs,
	productfeatures.FeatureToolIOLogs,
	productfeatures.FeatureSessionCapture,
	productfeatures.FeatureAuthzChallengeLogging,
	productfeatures.FeatureSSO,
	productfeatures.FeatureSCIM,
	productfeatures.FeatureHooksBrowserLogin,
	productfeatures.FeatureHooksFailOpen,
	productfeatures.FeatureCustomModelKeys,
	productfeatures.FeatureSkillCaptureMetadataOnly,
	productfeatures.FeatureAIPlatformPushIntegrations,
	productfeatures.FeaturePlatformMCP,
	productfeatures.FeatureCustomerManagedEncryptionKeys,
	productfeatures.FeatureConsentToolFiltering,
	productfeatures.FeatureSessionPortability,
}

func TestAttach_MountsOrganizationFeaturesRoutes(t *testing.T) {
	t.Parallel()

	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-mounted-features", "operator@example.com")))
	svc.tracer = testenv.NewTracerProvider(t).Tracer("admin_test")
	mux := goahttp.NewMuxer()
	Attach(mux, svc)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, "/admin/organization.features", nil))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}

func TestGetOrganizationFeatures_ReturnsNineteenFields(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	orgID := "org_admin_features_contract"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Admin Features Contract Org", Slug: "admin-features-contract", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	req := httptest.NewRequest(http.MethodGet, "/admin/organization.features?organization_id="+orgID, nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var result map[string]bool
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	wantKeys := map[string]struct{}{
		"logs_enabled": {}, "tool_io_logs_enabled": {}, "session_capture_enabled": {},
		"authz_challenge_logging_enabled": {}, "sso_enabled": {}, "scim_enabled": {},
		"hooks_browser_login_enabled": {}, "hooks_fail_open_enabled": {}, "custom_model_keys_enabled": {},
		"skills_enabled": {}, "skill_capture_metadata_only": {}, "ai_platform_push_integrations_enabled": {},
		"platform_mcp_enabled": {}, "customer_managed_encryption_keys_enabled": {},
		"remote_session_auto_refresh_enabled": {}, "remote_session_auto_refresh_enforced_enabled": {},
		"consent_tool_filtering_enabled": {}, "session_portability_enabled": {}, "device_agent": {},
	}
	gotKeys := make(map[string]struct{}, len(result))
	for key := range result {
		gotKeys[key] = struct{}{}
	}
	require.Equal(t, wantKeys, gotKeys)
}

func TestSetOrganizationFeature_SkillsAndRefreshSemantics(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	orgID := "org_admin_features"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 orgID,
		Name:               "Admin Features Org",
		Slug:               "admin-features-org",
		GramAccountType:    "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, conn, orgID))

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")

	result := setAdminOrganizationFeature(t, handler, sessionID, orgID, productfeatures.FeatureSkills, false)
	require.True(t, result.SkillsEnabled)
	result = setAdminOrganizationFeature(t, handler, sessionID, orgID, productfeatures.FeatureSkills, true)
	require.True(t, result.SkillsEnabled)

	_, err := featurerepo.New(conn).EnableFeature(ctx, featurerepo.EnableFeatureParams{
		OrganizationID: orgID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced),
	})
	require.NoError(t, err)
	svc.productFeatures.UpdateFeatureCache(ctx, orgID, productfeatures.FeatureRemoteSessionAutoRefreshEnforced, true)

	result = setAdminOrganizationFeature(t, handler, sessionID, orgID, productfeatures.FeatureRemoteSessionAutoRefresh, true)
	require.True(t, result.RemoteSessionAutoRefreshEnabled)
	require.False(t, result.RemoteSessionAutoRefreshEnforcedEnabled)
	result = setAdminOrganizationFeature(t, handler, sessionID, orgID, productfeatures.FeatureRemoteSessionAutoRefresh, false)
	require.False(t, result.RemoteSessionAutoRefreshEnabled)
	require.False(t, result.RemoteSessionAutoRefreshEnforcedEnabled)
}

func TestSetOrganizationFeature_RejectsDirectEnforcementMutation(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	orgID := "org_admin_feature_enforcement"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Admin Feature Enforcement Org", Slug: "admin-feature-enforcement", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	enabled := true
	feature := string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
	body, err := json.Marshal(adminserver.SetOrganizationFeatureRequestBody{
		OrganizationID: &orgID, FeatureName: &feature, Enabled: &enabled,
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/admin/organization.features", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestSetOrganizationFeature_RecordsAdminActor(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_admin_feature_actor"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Admin Feature Actor Org", Slug: "admin-feature-actor", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	setAdminOrganizationFeature(t, handler, sessionID, orgID, productfeatures.FeatureLogs, true)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationProductFeatureEnabled)
	require.NoError(t, err)
	require.Equal(t, "sub-admin", entry.ActorID)
	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)
}

func TestSetOrganizationFeature_EnableThenDisableEveryGenericWritableFeature(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	orgID := "org_admin_feature_allowlist"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Admin Features Org", Slug: "admin-features-allowlist", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, conn, orgID))

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	require.Len(t, adminGenericWritableFeatures, 15)
	for _, feature := range adminGenericWritableFeatures {
		setAdminOrganizationFeature(t, handler, sessionID, orgID, feature, true)
	}
	var result adminOrganizationFeaturesResponse
	for _, feature := range adminGenericWritableFeatures {
		result = setAdminOrganizationFeature(t, handler, sessionID, orgID, feature, false)
	}
	response, err := json.Marshal(result)
	require.NoError(t, err)
	var states map[string]bool
	require.NoError(t, json.Unmarshal(response, &states))
	for _, feature := range adminGenericWritableFeatures {
		key := string(feature) + "_enabled"
		if feature == productfeatures.FeatureSkillCaptureMetadataOnly {
			key = string(feature)
		}
		require.False(t, states[key], "feature=%s", feature)
	}
}

func TestSetOrganizationFeature_RejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_admin_feature_strict_json"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Admin Feature Strict JSON Org", Slug: "admin-feature-strict-json", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")

	for name, body := range map[string]string{
		"invalid feature": `{"organization_id":"` + orgID + `","feature_name":"not_a_feature","enabled":true}`,
		"missing enabled": `{"organization_id":"` + orgID + `","feature_name":"logs"}`,
		"unknown field":   `{"organization_id":"` + orgID + `","feature_name":"logs","enabled":true,"extra":true}`,
		"trailing JSON":   `{"organization_id":"` + orgID + `","feature_name":"logs","enabled":true}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/admin/organization.features", bytes.NewBufferString(body))
			req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func setAdminOrganizationFeature(
	t *testing.T,
	handler http.Handler,
	sessionID string,
	orgID string,
	feature productfeatures.Feature,
	enabled bool,
) adminOrganizationFeaturesResponse {
	t.Helper()
	featureName := string(feature)
	body, err := json.Marshal(adminserver.SetOrganizationFeatureRequestBody{
		OrganizationID: &orgID, FeatureName: &featureName, Enabled: &enabled,
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/admin/organization.features", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "feature=%s body=%s", feature, rec.Body.String())

	var result adminOrganizationFeaturesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	return result
}

func makeAdminFeatureSession(t *testing.T, ctx context.Context, svc *Service, email string) string {
	t.Helper()

	sessionID, err := svc.sessions.Store(ctx, StoreParams{
		Email: email, Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD,
		AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	return sessionID
}

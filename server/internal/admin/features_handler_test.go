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

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

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

func TestOrganizationFeatures_MatchesPlatformAdminAndUpdatesCuratedFlags(t *testing.T) {
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

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")

	result := getAdminOrganizationFeatures(t, handler, sessionID, orgID)
	require.Equal(t, adminOrganizationFeaturesResponse{}, result)

	for feature := range adminOrganizationFeatures {
		body, err := json.Marshal(setAdminOrganizationFeatureRequest{
			OrganizationID: orgID,
			FeatureName:    string(feature),
			Enabled:        ptrTo(true),
		})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/admin/organization.features", bytes.NewReader(body))
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	result = getAdminOrganizationFeatures(t, handler, sessionID, orgID)
	require.True(t, result.AuthzChallengeLoggingEnabled)
	require.True(t, result.CustomerManagedEncryptionKeysEnabled)
	require.True(t, result.CustomModelKeysEnabled)
	require.True(t, result.PlatformMcpEnabled)
	require.True(t, result.RemoteSessionAutoRefreshEnabled)
	require.True(t, result.SsoEnabled)
	require.True(t, result.ScimEnabled)
}

func TestSetOrganizationFeature_RejectsFeatureOutsidePlatformAdminList(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	orgID := "org_admin_feature_allowlist"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Admin Features Org", Slug: "admin-features-allowlist", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	body := []byte(`{"organization_id":"` + orgID + `","feature_name":"hooks_fail_open","enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/organization.features", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	enabled, err := svc.productFeatures.IsFeatureEnabled(ctx, orgID, productfeatures.FeatureHooksFailOpen)
	require.NoError(t, err)
	require.False(t, enabled)
}

func getAdminOrganizationFeatures(t *testing.T, handler http.Handler, sessionID, orgID string) adminOrganizationFeaturesResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/organization.features?organization_id="+orgID, nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

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

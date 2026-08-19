package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAttach_MountsOrganizationFeaturesRoute(t *testing.T) {
	t.Parallel()

	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-mounted-features", "operator@example.com")))
	svc.tracer = testenv.NewTracerProvider(t).Tracer("admin_test")

	mux := goahttp.NewMuxer()
	Attach(mux, svc)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/organization.features", nil))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleGetOrganizationFeatures_ReturnsCuratedFlags(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	orgID := "org_admin_features"
	_, err := conn.Exec(ctx, `
    insert into organization_metadata (id, name, slug, gram_account_type, whitelisted)
    values ($1, 'Admin Features Org', 'admin-features-org', 'enterprise', false)
  `, orgID)
	require.NoError(t, err)

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	req := httptest.NewRequest(http.MethodGet, "/admin/organization.features?organization_id="+orgID, nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var result adminOrganizationFeatures
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.False(t, result.ConsentToolFilteringEnabled)
	require.False(t, result.HooksBrowserLoginEnabled)
	require.False(t, result.HooksFailOpenEnabled)
	require.False(t, result.PlatformMcpEnabled)
	require.Equal(t, "disabled", result.RemoteSessionAutoRefreshPolicy)
	require.False(t, result.SessionCaptureEnabled)
	require.False(t, result.SkillCaptureMetadataOnly)
	require.True(t, result.SkillsEnabled)

	queries := productfeaturesrepo.New(conn)
	for _, feature := range []productfeatures.Feature{
		productfeatures.FeatureConsentToolFiltering,
		productfeatures.FeatureHooksBrowserLogin,
		productfeatures.FeatureHooksFailOpen,
		productfeatures.FeaturePlatformMCP,
		productfeatures.FeatureRemoteSessionAutoRefresh,
		productfeatures.FeatureSessionCapture,
		productfeatures.FeatureSkillCaptureMetadataOnly,
	} {
		_, err = queries.EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
			OrganizationID: orgID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
		svc.productFeatures.UpdateFeatureCache(ctx, orgID, feature, true)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/organization.features?organization_id="+orgID, nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.True(t, result.ConsentToolFilteringEnabled)
	require.True(t, result.HooksBrowserLoginEnabled)
	require.True(t, result.HooksFailOpenEnabled)
	require.True(t, result.PlatformMcpEnabled)
	require.Equal(t, "user_controlled", result.RemoteSessionAutoRefreshPolicy)
	require.True(t, result.SessionCaptureEnabled)
	require.True(t, result.SkillCaptureMetadataOnly)
	require.True(t, result.SkillsEnabled)

	_, err = queries.EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
		OrganizationID: orgID,
		FeatureName:    string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced),
	})
	require.NoError(t, err)
	svc.productFeatures.UpdateFeatureCache(ctx, orgID, productfeatures.FeatureRemoteSessionAutoRefreshEnforced, true)

	req = httptest.NewRequest(http.MethodGet, "/admin/organization.features?organization_id="+orgID, nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, "enforced", result.RemoteSessionAutoRefreshPolicy)
}

func makeAdminFeatureSession(t *testing.T, ctx context.Context, svc *Service, email string) string {
	t.Helper()

	sessionID, err := svc.sessions.Store(ctx, StoreParams{
		Email:        email,
		Name:         "Test Operator",
		OIDCSubject:  "sub-admin",
		HD:           testAdminHD,
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	return sessionID
}

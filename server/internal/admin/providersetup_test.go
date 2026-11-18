package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminserver "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestProviderSetupRolloutStaffAPI(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	orgID := "org_provider_rollout"
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Provider Rollout Org", Slug: "provider-rollout", GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	for _, enabled := range []bool{true, false} {
		body, err := json.Marshal(adminserver.SetOrganizationProviderSetupRequestBody{OrganizationID: new(orgID), Enabled: new(enabled)})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/admin/organization.providerSetup", bytes.NewReader(body))
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var result adminOrganizationFeaturesResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
		require.NotNil(t, result.ProviderSetupEnabled)
		require.Equal(t, enabled, *result.ProviderSetupEnabled)
		stored, err := svc.productFeatures.IsFeatureEnabledUncached(ctx, orgID, productfeatures.FeatureProviderSetup)
		require.NoError(t, err)
		require.Equal(t, enabled, stored)
	}
	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationProductFeatureDisabled)
	require.NoError(t, err)
	require.Equal(t, "sub-admin", entry.ActorID)

	for _, body := range []string{`{"organization_id":"org_provider_rollout"}`, `{"organization_id":"org_provider_rollout","enabled":null}`, `{"organization_id":"org_provider_rollout","enabled":true,"unknown":true}`} {
		req := httptest.NewRequest(http.MethodPost, "/admin/organization.providerSetup", bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/organization.providerSetup", bytes.NewBufferString(`{"organization_id":"unknown-organization","enabled":true}`))
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

package productfeatures_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProductFeaturesService_RequestedOrganizationIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	activeOrganizationID := activeOrganizationID(t, ctx)
	targetOrganizationID := uuid.NewString()
	seedOrganization(t, ctx, ti.conn, targetOrganizationID)
	seedRequestedOrganizationRole(t, ctx, ti, targetOrganizationID, authz.SystemRoleAdmin)

	q := repo.New(ti.conn)
	_, err := q.EnableFeature(ctx, repo.EnableFeatureParams{OrganizationID: activeOrganizationID, FeatureName: string(productfeatures.FeatureLogs)})
	require.NoError(t, err)

	result, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{OrganizationID: targetOrganizationID})
	require.NoError(t, err)
	require.False(t, result.LogsEnabled)
	activeResult, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{OrganizationID: activeOrganizationID})
	require.NoError(t, err)
	require.True(t, activeResult.LogsEnabled)

	require.NoError(t, ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: targetOrganizationID,
		FeatureName:    string(productfeatures.FeatureLogs),
		Enabled:        true,
	}))
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	client := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), ti.conn, redisClient)
	activeEnabled, err := client.IsFeatureEnabled(ctx, activeOrganizationID, productfeatures.FeatureLogs)
	require.NoError(t, err)
	require.True(t, activeEnabled)
	targetEnabled, err := client.IsFeatureEnabled(ctx, targetOrganizationID, productfeatures.FeatureLogs)
	require.NoError(t, err)
	require.True(t, targetEnabled)

	require.NoError(t, ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: targetOrganizationID,
		FeatureName:    string(productfeatures.FeatureLogs),
		Enabled:        false,
	}))
	activeEnabled, err = client.IsFeatureEnabled(ctx, activeOrganizationID, productfeatures.FeatureLogs)
	require.NoError(t, err)
	require.True(t, activeEnabled, "target cache update must not affect the active organization")
	targetEnabled, err = client.IsFeatureEnabled(ctx, targetOrganizationID, productfeatures.FeatureLogs)
	require.NoError(t, err)
	require.False(t, targetEnabled)

	require.NoError(t, agentrepo.New(ti.conn).UpsertDeviceAgentSync(ctx, agentrepo.UpsertDeviceAgentSyncParams{
		OrganizationID: targetOrganizationID,
		Email:          "device@example.com",
	}))
	result, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{OrganizationID: targetOrganizationID})
	require.NoError(t, err)
	require.True(t, result.DeviceAgent)
	activeResult, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{OrganizationID: activeOrganizationID})
	require.NoError(t, err)
	require.False(t, activeResult.DeviceAgent)
}

func TestProductFeaturesService_RemoteSessionPolicyTargetsRequestedOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	activeOrganizationID := activeOrganizationID(t, ctx)
	targetOrganizationID := uuid.NewString()
	seedOrganization(t, ctx, ti.conn, targetOrganizationID)
	seedRequestedOrganizationRole(t, ctx, ti, targetOrganizationID, authz.SystemRoleAdmin)
	q := repo.New(ti.conn)
	_, err := q.EnableFeature(ctx, repo.EnableFeatureParams{
		OrganizationID: activeOrganizationID,
		FeatureName:    string(productfeatures.FeatureRemoteSessionAutoRefresh),
	})
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	featureClient := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), ti.conn, redisClient)

	for _, tc := range []struct {
		policy            string
		visible, enforced bool
	}{
		{policy: "user_controlled", visible: true},
		{policy: "enforced", enforced: true},
		{policy: "disabled"},
	} {
		require.NoError(t, ti.service.SetRemoteSessionAutoRefreshPolicy(ctx, &gen.SetRemoteSessionAutoRefreshPolicyPayload{
			OrganizationID: targetOrganizationID,
			Policy:         tc.policy,
		}))
		visible, err := q.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{OrganizationID: targetOrganizationID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefresh)})
		require.NoError(t, err)
		require.Equal(t, tc.visible, visible, tc.policy)
		enforced, err := q.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{OrganizationID: targetOrganizationID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)})
		require.NoError(t, err)
		require.Equal(t, tc.enforced, enforced, tc.policy)

		cachedVisible, err := featureClient.IsFeatureEnabled(ctx, targetOrganizationID, productfeatures.FeatureRemoteSessionAutoRefresh)
		require.NoError(t, err)
		require.Equal(t, tc.visible, cachedVisible, tc.policy)
		cachedEnforced, err := featureClient.IsFeatureEnabled(ctx, targetOrganizationID, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
		require.NoError(t, err)
		require.Equal(t, tc.enforced, cachedEnforced, tc.policy)

		activeVisible, err := q.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{OrganizationID: activeOrganizationID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefresh)})
		require.NoError(t, err)
		require.True(t, activeVisible, "target policy must not alter the active organization")
		activeEnforced, err := q.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{OrganizationID: activeOrganizationID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)})
		require.NoError(t, err)
		require.False(t, activeEnforced, "target policy must not alter the active organization enforced state")
	}
}

func TestProductFeaturesService_AuthorizesBeforeOrganizationLookup(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	activeOrganizationID := activeOrganizationID(t, ctx)
	targetOrganizationID := uuid.NewString()
	seedOrganization(t, ctx, ti.conn, targetOrganizationID)

	readActiveOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, activeOrganizationID))
	_, err := ti.service.GetProductFeatures(readActiveOnly, &gen.GetProductFeaturesPayload{OrganizationID: targetOrganizationID})
	requireOopsCode(t, err, oops.CodeForbidden)
	adminActiveOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, activeOrganizationID))
	err = ti.service.SetProductFeature(adminActiveOnly, &gen.SetProductFeaturePayload{OrganizationID: targetOrganizationID, FeatureName: string(productfeatures.FeatureLogs), Enabled: true})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.SetRemoteSessionAutoRefreshPolicy(adminActiveOnly, &gen.SetRemoteSessionAutoRefreshPolicyPayload{OrganizationID: targetOrganizationID, Policy: "enforced"})
	requireOopsCode(t, err, oops.CodeForbidden)

	unknownOrganizationID := uuid.NewString()
	_, err = ti.service.GetProductFeatures(readActiveOnly, &gen.GetProductFeaturesPayload{OrganizationID: unknownOrganizationID})
	requireOopsCode(t, err, oops.CodeForbidden, "unauthorized unknown target must not be enumerable")
	err = ti.service.SetProductFeature(adminActiveOnly, &gen.SetProductFeaturePayload{OrganizationID: unknownOrganizationID, FeatureName: string(productfeatures.FeatureLogs), Enabled: true})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.SetRemoteSessionAutoRefreshPolicy(adminActiveOnly, &gen.SetRemoteSessionAutoRefreshPolicyPayload{OrganizationID: unknownOrganizationID, Policy: "enforced"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestProductFeaturesService_HooksFailOpenAuditTargetsRequestedOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	activeOrganizationID := activeOrganizationID(t, ctx)
	targetOrganizationID := uuid.NewString()
	seedOrganization(t, ctx, ti.conn, targetOrganizationID)
	seedRequestedOrganizationRole(t, ctx, ti, targetOrganizationID, authz.SystemRoleAdmin)
	require.NoError(t, ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		OrganizationID: targetOrganizationID,
		FeatureName:    string(productfeatures.FeatureHooksFailOpen),
		Enabled:        true,
	}))
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenEnabled)
	require.NoError(t, err)
	require.Equal(t, targetOrganizationID, record.OrganizationID)
	require.Equal(t, targetOrganizationID, record.SubjectID)
	require.Equal(t, targetOrganizationID, record.SubjectSlug)
	require.NotEqual(t, activeOrganizationID, record.OrganizationID)
}

func TestProductFeaturesHTTP_UsesRequestedOrganizationPersistedGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.SessionID)
	session, err := ti.sessionManager.GetSession(ctx, *authCtx.SessionID)
	require.NoError(t, err)
	activeOrganizationID := session.ActiveOrganizationID
	targetOrganizationID := uuid.NewString()
	seedOrganization(t, ctx, ti.conn, targetOrganizationID)
	seedRequestedOrganizationRoleForUser(t, ctx, ti, activeOrganizationID, session.UserID, authz.SystemRoleAdmin)

	mux := goahttp.NewMuxer()
	productfeatures.Attach(mux, ti.service)
	do := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, target, bytes.NewBufferString(body))
		_, preloaded := contextvalues.GetAuthContext(req.Context())
		require.False(t, preloaded, "request must traverse session authentication and grant preparation")
		req.Header.Set("Gram-Session", session.SessionID)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)
		return recorder
	}

	for name, response := range map[string]*httptest.ResponseRecorder{
		"get":           do(http.MethodGet, "/rpc/productFeatures.get?organization_id="+targetOrganizationID, ""),
		"set":           do(http.MethodPost, "/rpc/productFeatures.set", `{"organization_id":"`+targetOrganizationID+`","feature_name":"hooks_fail_open","enabled":true}`),
		"remote policy": do(http.MethodPost, "/rpc/productFeatures.setRemoteSessionAutoRefreshPolicy", `{"organization_id":"`+targetOrganizationID+`","policy":"enforced"}`),
	} {
		require.Equal(t, http.StatusForbidden, response.Code, "%s: %s", name, response.Body.String())
	}

	seedRequestedOrganizationRoleForUser(t, ctx, ti, targetOrganizationID, session.UserID, authz.SystemRoleAdmin)
	for name, response := range map[string]*httptest.ResponseRecorder{
		"get":           do(http.MethodGet, "/rpc/productFeatures.get?organization_id="+targetOrganizationID, ""),
		"set":           do(http.MethodPost, "/rpc/productFeatures.set", `{"organization_id":"`+targetOrganizationID+`","feature_name":"hooks_fail_open","enabled":true}`),
		"remote policy": do(http.MethodPost, "/rpc/productFeatures.setRemoteSessionAutoRefreshPolicy", `{"organization_id":"`+targetOrganizationID+`","policy":"enforced"}`),
	} {
		require.Equal(t, http.StatusOK, response.Code, "%s: %s", name, response.Body.String())
	}

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationHooksFailOpenEnabled)
	require.NoError(t, err)
	require.Equal(t, session.UserID, record.ActorID)
	require.Equal(t, targetOrganizationID, record.OrganizationID)
}

func seedRequestedOrganizationRole(t *testing.T, ctx context.Context, ti *testInstance, organizationID, role string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedRequestedOrganizationRoleForUser(t, ctx, ti, organizationID, authCtx.UserID, role)
}

func seedRequestedOrganizationRoleForUser(t *testing.T, ctx context.Context, ti *testInstance, organizationID, userID, role string) {
	t.Helper()
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))
	_, err := orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     organizationID,
		WorkosUserID:       userID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGText("membership-" + userID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     role,
	})
	require.NoError(t, err)
}

func requireOopsCode(t *testing.T, err error, code oops.Code, msgAndArgs ...any) {
	t.Helper()
	require.Error(t, err, msgAndArgs...)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code, msgAndArgs...)
}

func TestProductFeaturesHTTP_RequiresOrganizationID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.SessionID)
	session, err := ti.sessionManager.GetSession(ctx, *authCtx.SessionID)
	require.NoError(t, err)

	mux := goahttp.NewMuxer()
	productfeatures.Attach(mux, ti.service)
	setBody, err := json.Marshal(map[string]any{"feature_name": "logs", "enabled": true})
	require.NoError(t, err)
	policyBody, err := json.Marshal(map[string]any{"policy": "enforced"})
	require.NoError(t, err)

	do := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, target, bytes.NewBufferString(body))
		req.Header.Set("Gram-Session", session.SessionID)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)
		return recorder
	}

	for name, response := range map[string]*httptest.ResponseRecorder{
		"get":           do(http.MethodGet, "/rpc/productFeatures.get", ""),
		"set":           do(http.MethodPost, "/rpc/productFeatures.set", string(setBody)),
		"remote policy": do(http.MethodPost, "/rpc/productFeatures.setRemoteSessionAutoRefreshPolicy", string(policyBody)),
	} {
		require.Equal(t, http.StatusBadRequest, response.Code, "%s: %s", name, response.Body.String())
	}
}

package remotesessions_test

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

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	authrepo "github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestProxyRegisterMountedHandlerResolvesProjectForTunneledRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithConfig(t, testServiceConfig{tunnelRouting: true})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.SessionID)
	require.NotNil(t, authCtx.ProjectSlug)

	require.NoError(t, authrepo.New(ti.conn).SetUserAdminFixture(ctx, authrepo.SetUserAdminFixtureParams{
		Admin:  true,
		UserID: authCtx.UserID,
	}))
	session, err := ti.sessionManager.GetSession(ctx, *authCtx.SessionID)
	require.NoError(t, err)
	session.SupportOrganizationID = authCtx.ActiveOrganizationID
	session.SupportExpiresAt = time.Now().Add(time.Hour)
	require.NoError(t, ti.sessionManager.UpdateSession(ctx, session))

	tunnelID := seedTunneledMcpServer(t, ctx, ti)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/register" {
			http.Error(w, "unexpected registration path", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"registered-through-tunnel"}`))
	}))
	t.Cleanup(gateway.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), gateway.URL, time.Minute))

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTunneledMcpServerDynamicClientRegistration)
	require.NoError(t, err)

	tunnelIDString := tunnelID.String()
	recorder := serveProxyRegister(t, ctx, ti, remotesessions.ProxyRegisterRequest{
		RegistrationEndpoint:    "https://idp.internal/oauth/register",
		Scope:                   nil,
		TokenEndpointAuthMethod: nil,
		TunneledMcpServerID:     &tunnelIDString,
	})

	require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())
	var response remotesessions.ProxyRegisterResponse
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	require.Equal(t, "registered-through-tunnel", response.ClientID)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTunneledMcpServerDynamicClientRegistration)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestProxyRegisterMountedHandlerRequiresPlatformAdminForTunnel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	tunnelID := seedTunneledMcpServer(t, ctx, ti)
	tunnelIDString := tunnelID.String()

	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalled = true
	}))
	t.Cleanup(upstream.Close)

	recorder := serveProxyRegister(t, ctx, ti, remotesessions.ProxyRegisterRequest{
		RegistrationEndpoint:    upstream.URL,
		Scope:                   nil,
		TokenEndpointAuthMethod: nil,
		TunneledMcpServerID:     &tunnelIDString,
	})
	require.Equal(t, http.StatusForbidden, recorder.Code, "body=%s", recorder.Body.String())
	require.False(t, upstreamCalled)
}

func serveProxyRegister(t *testing.T, ctx context.Context, ti *testInstance, payload remotesessions.ProxyRegisterRequest) *httptest.ResponseRecorder {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.SessionID)
	require.NotNil(t, authCtx.ProjectSlug)
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/oauth/proxy-register", bytes.NewReader(body))
	req.Header.Set(constants.SessionHeader, *authCtx.SessionID)
	req.Header.Set(constants.ProjectHeader, *authCtx.ProjectSlug)

	mux := goahttp.NewMuxer()
	remotesessions.Attach(mux, ti.service)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	return recorder
}

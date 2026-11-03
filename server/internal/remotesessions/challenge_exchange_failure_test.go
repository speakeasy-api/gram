package remotesessions_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/route"
)

func TestRemoteLoginCallback_ExchangeFailureNamesProviderReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        string
		wantCode    oops.Code
	}{
		{
			name:        "registered code",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"error":"invalid_grant","error_description":"Grant not found or authorization code expired"}`,
			want:        "token exchange with the identity provider failed: invalid_grant",
			wantCode:    oops.CodeUnauthorized,
		},
		{
			name:        "registered code wins over status",
			status:      http.StatusUnauthorized,
			contentType: "application/json",
			body:        `{"error":"invalid_client"}`,
			want:        "token exchange with the identity provider failed: invalid_client",
			wantCode:    oops.CodeUnauthorized,
		},
		{
			name:        "unregistered code",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"error":"app_not_enabled","error_description":"Click here to fix: https://evil.example"}`,
			want:        "token exchange with the identity provider failed: HTTP 400",
			wantCode:    oops.CodeUnauthorized,
		},
		{
			name:        "non-oauth body",
			status:      http.StatusBadRequest,
			contentType: "text/html",
			body:        `<html>Bad Request</html>`,
			want:        "token exchange with the identity provider failed: HTTP 400",
			wantCode:    oops.CodeUnauthorized,
		},
		{
			name:        "server error without body",
			status:      http.StatusServiceUnavailable,
			contentType: "text/plain",
			body:        ``,
			want:        "token exchange with the identity provider failed: HTTP 503",
			wantCode:    oops.CodeUnavailable,
		},
		{
			name:        "error body under 200",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired."}`,
			want:        "token exchange with the identity provider failed: invalid_grant",
			wantCode:    oops.CodeUnauthorized,
		},
		{
			name:        "undecodable error body under 200",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"error":"invalid_grant","expires_in":""}`,
			want:        "token exchange with the identity provider failed: invalid_grant",
			wantCode:    oops.CodeUnauthorized,
		},
		{
			name:        "server_error under 200",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"error":"server_error"}`,
			want:        "token exchange with the identity provider failed: server_error",
			wantCode:    oops.CodeGatewayError,
		},
		{
			name:        "unregistered code under 200",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"error":"app_not_enabled"}`,
			want:        "token exchange with the identity provider failed",
			wantCode:    oops.CodeUnauthorized,
		},
		{
			name:        "malformed body under 200",
			status:      http.StatusOK,
			contentType: "text/html",
			body:        `<html>OK</html>`,
			want:        "token exchange with the identity provider failed",
			wantCode:    oops.CodeUnauthorized,
		},
		{
			name:        "no access token under 200",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"token_type":"Bearer"}`,
			want:        "token exchange with the identity provider failed",
			wantCode:    oops.CodeUnauthorized,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, callback, err := driveSyntheticLogin(t, "exchange-failure-"+strings.ReplaceAll(tc.name, " ", "-"), func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			require.Error(t, err)
			require.NotEqual(t, http.StatusSeeOther, callback.Code)

			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable)
			require.Equal(t, tc.wantCode, shareable.Code)
			require.Equal(t, tc.want, shareable.Error(), "the provider's free-text description is never echoed")
		})
	}
}

// What the person reads when the sign-in provider fails during sign-in.
func TestRemoteLoginCallback_ProviderOutagePage(t *testing.T) {
	t.Parallel()

	failing := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	unused := failing(http.StatusOK, `{"access_token":"unused"}`)
	authorizeError := func(code string) syntheticLoginOption {
		return withCallbackQuery(func(q url.Values) {
			q.Del("code")
			q.Set("error", code)
		})
	}

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		opts    []syntheticLoginOption
		outage  bool

		// external: the provider's own failure, so it's tagged not to alert.
		external bool
	}{
		{name: "token endpoint 503", handler: failing(http.StatusServiceUnavailable, ""), outage: true, external: true},
		{name: "token endpoint 500", handler: failing(http.StatusInternalServerError, ""), outage: true, external: true},
		{name: "token endpoint 429", handler: failing(http.StatusTooManyRequests, ""), outage: true, external: true},
		{name: "token endpoint unreachable", handler: func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }, outage: true, external: true},
		{name: "authorize temporarily_unavailable", handler: unused, opts: []syntheticLoginOption{authorizeError(oautherr.CodeTemporarilyUnavailable)}, outage: true, external: true},
		{name: "authorize server_error", handler: unused, opts: []syntheticLoginOption{authorizeError(oautherr.CodeServerError)}, outage: true, external: true},
		{name: "invalid_client is not an outage", handler: failing(http.StatusUnauthorized, `{"error":"invalid_client"}`), outage: false},
		{name: "invalid_scope is not an outage", handler: unused, opts: []syntheticLoginOption{authorizeError(oautherr.CodeInvalidScope)}, outage: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, err := driveSyntheticLogin(t, "provider-outage-"+uuid.NewString()[:8], tc.handler, tc.opts...)
			require.Error(t, err)
			tagged := httptest.NewRecorder()
			remotesessions.TagExternalOutage(tagged, err)
			if tc.external {
				require.Equal(t, "1", tagged.Header().Get(constants.HeaderProxiedResponse))
			} else {
				require.Empty(t, tagged.Header().Get(constants.HeaderProxiedResponse))
			}
			page := httptest.NewRecorder()
			oops.BrowserErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error { return err }).ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/callback", nil))
			if !tc.outage {
				require.NotContains(t, page.Body.String(), "responding right now")
				return
			}
			require.GreaterOrEqual(t, page.Code, http.StatusInternalServerError)
			require.Contains(t, page.Body.String(), "responding right now")
			require.Contains(t, page.Body.String(), "127.0.0.1", "names the provider")
			require.Contains(t, page.Body.String(), "Try connecting again in a few minutes")
			require.NotContains(t, page.Body.String(), "problem on our side")
			require.NotContains(t, page.Body.String(), "allowed to connect")
		})
	}
}

// A tunneled provider's host is internal, so its outage page names no host.
func TestRemoteLoginCallback_TunneledProviderHostStaysOffThePage(t *testing.T) {
	t.Parallel()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	tunnels := tunnelrouting.NewHTTPClient(route.NewRouteTable(), "forward-token", policy, nil)
	_, env := newSyntheticExpiryEnv(t, "provider-outage-tunneled", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh"}`))
	}, withTunnels(tunnels))
	bindIssuerToTunnel(t, env)

	authURL, err := env.mgr.BuildAuthorizationUrl(t.Context(), remotesessions.ParentChallenge{
		ID:                  uuid.NewString(),
		ProjectID:           env.projectID,
		OrganizationID:      env.organizationID,
		UserSessionIssuerID: env.session.UserSessionIssuerID,
		Subject:             &env.subject,
		McpSlug:             "provider-outage-tunneled",
		FinalRedirectURI:    "",
		Resource:            "",
	}, listClient(t, env))
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	query := url.Values{"state": {parsed.Query().Get("state")}, "error": {oautherr.CodeServerError}}
	_, err = env.mgr.CompleteRemoteLogin(httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?"+query.Encode(), nil))
	require.Error(t, err)
	page := httptest.NewRecorder()
	oops.BrowserErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error {
		return fmt.Errorf("callback: %w", err)
	}).ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/callback", nil))
	require.Contains(t, page.Body.String(), "The service this connection uses")
	endpoint, err := url.Parse(issuerTokenEndpoint(t, env))
	require.NoError(t, err)
	require.NotContains(t, page.Body.String(), endpoint.Hostname())
}

package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestOAuthBrowserRoutePolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		method, suffix, body string
		html                 bool
	}{
		{http.MethodGet, "/authorize", "", true},
		{http.MethodGet, "/connect", "", true},
		{http.MethodPost, "/connect", "", true},
		{http.MethodGet, "/connect/first-party", "", true},
		{http.MethodPost, "/connect/remote-session", "action=connect", true},
		{http.MethodPost, "/connect/remote-session", "action=identity_chaining_check", false},
		{http.MethodPost, "/connect/remote-session", "action=agent_connections", false},
		{http.MethodPost, "/connect/remote-session", "action=agent_attach", false},
		{http.MethodPost, "/connect/remote-session", "action=agent_detach", false},
		{http.MethodPost, "/connect/remote-session", "", true},
		{http.MethodPost, "/connect/remote-session", "action=unknown", true},
		{http.MethodPost, "/connect/remote-session", "action=agent_attach&action=connect", false},
		{http.MethodPost, "/connect/remote-session", "action=agent_connections&unused=%zz", false},
		{http.MethodPost, "/connect/remote-session", "action=%zz", true},
		{http.MethodPost, "/connect/mcp", "", false},
		{http.MethodDelete, "/connect/mcp", "", false},
		{http.MethodPost, "/register", "", false},
	} {
		t.Run(tc.method+tc.suffix+tc.body, func(t *testing.T) {
			t.Parallel()
			handler := oauthRouteHandler(testenv.NewLogger(t), tc.method, "/mcp/{mcpSlug}"+tc.suffix, func(http.ResponseWriter, *http.Request) error {
				return oops.E(oops.CodeBadRequest, nil, "public refusal")
			})
			request := httptest.NewRequest(tc.method, "/mcp/example"+tc.suffix, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "text/html")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if tc.html {
				require.NotEmpty(t, oauthtest.RequireAuthorizationErrorPage(t, response, http.StatusBadRequest).Code)
			} else {
				require.Equal(t, http.StatusBadRequest, response.Code)
				require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			}
		})
	}
}

func TestConsentActionMalformedFormPreservesResolutionError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		code   oops.Code
		status int
	}{
		{"missing endpoint", oops.CodeNotFound, http.StatusNotFound},
		{"resolution outage", oops.CodeUnavailable, http.StatusServiceUnavailable},
	} {
		for _, action := range []string{"connect", "agent_connections"} {
			t.Run(tc.name+"/"+action, func(t *testing.T) {
				t.Parallel()
				called := false
				handler := consentActionRouteHandler(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error {
					called = true
					return oops.E(tc.code, nil, "endpoint resolution failed")
				})
				request := httptest.NewRequest(http.MethodPost, "/connect/remote-session", strings.NewReader("action="+action+"&unused=%zz"))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				require.True(t, called, "endpoint resolution must run before rejecting the form")
				require.Equal(t, tc.status, response.Code)
				if action == "connect" {
					body := oauthtest.RequireAuthorizationErrorPage(t, response, tc.status)
					require.Equal(t, "endpoint resolution failed", body.Description)
				} else {
					require.Equal(t, "application/json", response.Header().Get("Content-Type"))
					require.Contains(t, response.Body.String(), "endpoint resolution failed")
				}
			})
		}
	}
}

func TestConsentActionMalformedFormRetainsParseFailure(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"connect", "agent_connections", "agent_attach", "agent_detach", "identity_chaining_check"} {
		for _, source := range []string{"body", "query"} {
			t.Run(action+"/"+source, func(t *testing.T) {
				t.Parallel()
				service := browserTestService(t)
				handler := consentActionRouteHandler(service.logger, func(w http.ResponseWriter, r *http.Request) error {
					return service.ServeConsentAction(w, r, &ResolvedMcpEndpoint{})
				})
				target := "/connect/remote-session"
				body := "action=" + action
				if source == "body" {
					body += "&unused=%zz"
				} else {
					target += "?unused=%zz"
				}
				request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				require.Equal(t, http.StatusBadRequest, response.Code)
				if action == "connect" {
					failure := oauthtest.RequireAuthorizationErrorPage(t, response, http.StatusBadRequest)
					require.Equal(t, "failed to parse form", failure.Description)
				} else {
					require.Equal(t, "application/json", response.Header().Get("Content-Type"))
					require.Contains(t, response.Body.String(), "failed to parse form")
				}
			})
		}
	}
}

func TestCallbackRoutesRenderExpiredPages(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/mcp/idp_callback", "/mcp/idp_callback/test-client", "/mcp/example/idp_callback",
		"/mcp/remote_login_callback", "/mcp/example/remote_login_callback", "/mcp/remote_login_bind",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			service := browserTestService(t)
			mux := goahttp.NewMuxer()
			Attach(mux, service, nil)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path+"?state=expired&code=unused", nil)
			request.Header.Set("Accept", "application/json")
			mux.ServeHTTP(response, request)
			status := http.StatusBadRequest
			if strings.Contains(path, "remote_login") {
				status = http.StatusUnauthorized
			}
			require.NotEmpty(t, oauthtest.RequireAuthorizationErrorPage(t, response, status).Code)
			require.Contains(t, response.Body.String(), "expired or was already used")
			require.Empty(t, response.Header().Get("Location"))
			require.NotContains(t, response.Body.String(), "unused")
		})
	}
}

func TestRemoteCallbackBrowserMismatchPagePreservesState(t *testing.T) {
	t.Parallel()
	service := browserTestService(t)
	parent := browserTestState()
	require.NoError(t, service.authnChallengeCache.Store(t.Context(), parent))
	remote := remotesessions.RemoteLoginState{ID: uuid.NewString(), ParentChallengeID: parent.ID, Subject: parent.Subject, UserSessionIssuerID: parent.UserSessionIssuerID}
	require.NoError(t, service.remoteLoginCache.Store(t.Context(), remote))
	mux := goahttp.NewMuxer()
	Attach(mux, service, nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?state="+remote.ID+"&code=unused", nil))
	require.NotEmpty(t, oauthtest.RequireAuthorizationErrorPage(t, response, http.StatusUnauthorized).Code)
	require.Contains(t, response.Body.String(), "doesn't match your current browser")
	_, err := service.remoteLoginCache.Get(t.Context(), "remoteLogin:"+remote.ID)
	require.NoError(t, err)
}

func TestFirstPartyBrowserRecovery(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"start", "idp", "consent", "action"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			service := browserTestService(t)
			var err error
			service.siteURL, err = url.Parse("https://app.example.test")
			require.NoError(t, err)
			state := browserTestState()
			state.FirstParty = true
			require.NoError(t, service.authnChallengeCache.Store(t.Context(), state))
			request := httptest.NewRequest(http.MethodGet, "/connect?state="+state.ID+"&redirect_uri=https://untrusted.example/", nil)
			handler := service.HandleFirstPartyConnect
			switch route {
			case "idp":
				handler = service.HandleIDPCallback
			case "consent":
				handler = func(w http.ResponseWriter, r *http.Request) error {
					return service.ServeConsent(w, r, &ResolvedMcpEndpoint{})
				}
			case "action":
				handler = func(w http.ResponseWriter, r *http.Request) error {
					return service.ServeConsentAction(w, r, &ResolvedMcpEndpoint{})
				}
				request = httptest.NewRequest(http.MethodPost, "/connect", strings.NewReader("state="+state.ID+"&action=connect"))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			response := httptest.NewRecorder()
			oops.BrowserErrHandle(service.logger, handler).ServeHTTP(response, request)
			require.NotEmpty(t, oauthtest.RequireAuthorizationErrorRecovery(t, response, http.StatusBadRequest, "https://app.example.test/@self").Code)
			require.Contains(t, response.Body.String(), "Return to dashboard")
		})
	}
}

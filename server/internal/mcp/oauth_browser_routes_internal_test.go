package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
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

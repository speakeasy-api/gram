package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type testAuthenticator struct {
	principal Principal
	err       error
	calls     int
}

func (a *testAuthenticator) Authenticate(_ context.Context, _ string) (Principal, error) {
	a.calls++
	return a.principal, a.err
}

func staffPrincipal() Principal {
	return Principal{Subject: "staff-subject", Email: "staff@example.test", ClientID: "test-client", ConnectionID: "test-connection", Scopes: []string{"admin:read"}}
}

func TestRuntimeFailsClosedWithoutAuthenticator(t *testing.T) {
	t.Parallel()

	handler := NewRuntime(nil, "").Handler()
	request := httptest.NewRequest(http.MethodPost, Path, nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}

func TestRuntimeRequiresBearerAndIgnoresBrowserCookie(t *testing.T) {
	t.Parallel()

	auth := &testAuthenticator{principal: staffPrincipal()}
	handler := NewRuntime(auth, "https://staff.example.test/.well-known/oauth-protected-resource/admin-mcp").Handler()
	request := httptest.NewRequest(http.MethodPost, Path, nil)
	request.AddCookie(&http.Cookie{Name: "gram_admin", Value: "browser-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, `Bearer resource_metadata="https://staff.example.test/.well-known/oauth-protected-resource/admin-mcp"`, response.Header().Get("WWW-Authenticate"))
	require.Zero(t, auth.calls)
}

func TestRuntimeRejectsInvalidIdentityAndReadScope(t *testing.T) {
	t.Parallel()

	for _, principal := range []Principal{
		{Subject: "", Email: "staff@example.test", ClientID: "test-client", ConnectionID: "test-connection", Scopes: []string{"admin:read"}},
		{Subject: "staff-subject", Email: "staff@example.test", ClientID: "", ConnectionID: "test-connection", Scopes: []string{"admin:read"}},
		{Subject: "staff-subject", Email: "staff@example.test", ClientID: "test-client", ConnectionID: "", Scopes: []string{"admin:read"}},
		{Subject: "staff-subject", Email: "staff@example.test", ClientID: "test-client", ConnectionID: "test-connection", Scopes: []string{"admin:write"}},
	} {
		auth := &testAuthenticator{principal: principal}
		request := httptest.NewRequest(http.MethodPost, Path, nil)
		request.Header.Set("Authorization", "Bearer test-token")
		response := httptest.NewRecorder()
		NewRuntime(auth, "").Handler().ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code)
		require.Equal(t, 1, auth.calls)
	}
}

func TestRuntimeRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	auth := &testAuthenticator{principal: staffPrincipal(), err: errors.New("invalid token")}
	request := httptest.NewRequest(http.MethodPost, Path, nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	NewRuntime(auth, "").Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.NotContains(t, response.Body.String(), "invalid token")
}

func TestRuntimeReportsIdentityOutage(t *testing.T) {
	t.Parallel()

	auth := &testAuthenticator{principal: staffPrincipal(), err: ErrAuthUnavailable}
	request := httptest.NewRequest(http.MethodPost, Path, nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	NewRuntime(auth, "").Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Empty(t, response.Header().Get("WWW-Authenticate"))
}

func TestRuntimeRejectsUnauthenticatedDiscovery(t *testing.T) {
	t.Parallel()

	auth := &testAuthenticator{principal: staffPrincipal()}
	for _, method := range []string{"initialize", "tools/list"} {
		request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{}}`))
		request.AddCookie(&http.Cookie{Name: "gram_admin", Value: "browser-session"})
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		NewRuntime(auth, "").Handler().ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code)
		require.Zero(t, auth.calls)
	}
}

func TestRuntimeRejectsUnsupportedMethodWithoutCaching(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, Path, nil)
	response := httptest.NewRecorder()
	NewRuntime(nil, "").Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
}

func TestContextToolReturnsOnlyAuthenticatedContext(t *testing.T) {
	t.Parallel()

	auth := &testAuthenticator{principal: staffPrincipal()}
	handler := NewRuntime(auth, "").Handler()
	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_admin_context","arguments":{}}}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, 1, auth.calls)

	var message struct {
		Result struct {
			StructuredContent AdminContext `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	require.Equal(t, "staff@example.test", message.Result.StructuredContent.Email)
	require.True(t, message.Result.StructuredContent.ReadOnly)
	require.Equal(t, []string{"admin:read"}, message.Result.StructuredContent.Scopes)
	require.NotContains(t, response.Body.String(), "test-token")
	require.NotContains(t, response.Body.String(), "test-connection")
}

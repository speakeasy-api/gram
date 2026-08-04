package adminmcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeHandlerRejectsMissingBearerToken(t *testing.T) {
	t.Parallel()

	authenticator := &testAuthenticator{principal: testPrincipal()}
	handler := NewRuntime(authenticator, testGate{enabled: true}, &testAuthorizer{}, "https://gram.example/.well-known/oauth-protected-resource/admin-mcp", nil).Handler()
	req := httptest.NewRequest(http.MethodPost, Path, nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	require.Equal(t, http.StatusUnauthorized, res.Code)
	require.Equal(t, `Bearer resource_metadata="https://gram.example/.well-known/oauth-protected-resource/admin-mcp"`, res.Header().Get("WWW-Authenticate"))
	require.Zero(t, authenticator.calls)
}

func TestRuntimeHandlerFailsClosedWhenGateIsDisabledOrErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		gate testGate
	}{
		{name: "disabled", gate: testGate{enabled: false}},
		{name: "error", gate: testGate{err: errors.New("feature provider unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			authorizer := &testAuthorizer{}
			handler := NewRuntime(&testAuthenticator{principal: testPrincipal()}, tc.gate, authorizer, "", nil).Handler()
			req := httptest.NewRequest(http.MethodPost, Path, nil)
			req.Header.Set("Authorization", "Bearer access-token")
			res := httptest.NewRecorder()

			handler.ServeHTTP(res, req)

			require.Equal(t, http.StatusForbidden, res.Code)
			require.Zero(t, authorizer.calls)
		})
	}
}

func TestRuntimeHandlerRequiresLiveOrganizationAdmin(t *testing.T) {
	t.Parallel()

	authorizer := &testAuthorizer{err: errors.New("admin grant removed")}
	handler := NewRuntime(&testAuthenticator{principal: testPrincipal()}, testGate{enabled: true}, authorizer, "", nil).Handler()
	req := httptest.NewRequest(http.MethodPost, Path, nil)
	req.Header.Set("Authorization", "Bearer access-token")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	require.Equal(t, http.StatusForbidden, res.Code)
	require.Equal(t, 1, authorizer.calls)
}

func TestRuntimeAuthenticateRejectsIncompletePrincipal(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime(&testAuthenticator{principal: Principal{UserID: "user"}}, testGate{enabled: true}, &testAuthorizer{}, "", nil)
	req := httptest.NewRequest(http.MethodPost, Path, nil)
	req.Header.Set("Authorization", "Bearer access-token")

	_, err := runtime.authenticate(req)

	require.ErrorIs(t, err, ErrUnauthorized)
}

type testAuthenticator struct {
	principal Principal
	err       error
	calls     int
}

func (a *testAuthenticator) Authenticate(_ context.Context, _ string) (Principal, error) {
	a.calls++
	return a.principal, a.err
}

type testGate struct {
	enabled bool
	err     error
}

func (g testGate) Enabled(_ context.Context, _ string) (bool, error) {
	return g.enabled, g.err
}

type testAuthorizer struct {
	err   error
	calls int
}

func (a *testAuthorizer) RequireLiveOrgAdmin(_ context.Context, _ Principal) error {
	a.calls++
	return a.err
}

func TestPrincipalContext(t *testing.T) {
	t.Parallel()

	principal := testPrincipal()
	got, ok := PrincipalFromContext(contextWithPrincipal(t.Context(), principal))

	require.True(t, ok)
	require.Equal(t, principal, got)
}

func testPrincipal() Principal {
	return Principal{
		UserID:         "user-1",
		OrganizationID: "organization-1",
		ConnectionID:   "connection-1",
		Generation:     "generation-1",
		ClientID:       "client-1",
	}
}

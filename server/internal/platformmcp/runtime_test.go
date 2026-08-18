package platformmcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRuntimeHandlerRejectsMissingBearerToken(t *testing.T) {
	t.Parallel()

	authenticator := &testAuthenticator{principal: testPrincipal()}
	handler := NewRuntime(testenv.NewLogger(t), authenticator, testGate{enabled: true}, &testAuthorizer{}, "https://gram.example/.well-known/oauth-protected-resource/platform-mcp", "test-cursor-key", nil, nil, nil, nil, nil).Handler()
	req := httptest.NewRequest(http.MethodPost, Path, nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	require.Equal(t, http.StatusUnauthorized, res.Code)
	require.Equal(t, `Bearer resource_metadata="https://gram.example/.well-known/oauth-protected-resource/platform-mcp"`, res.Header().Get("WWW-Authenticate"))
	require.Zero(t, authenticator.calls)
}

func TestRuntimeHandlerFailsClosedWhenGateIsDisabledOrErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		gate   testGate
		status int
	}{
		{name: "disabled", gate: testGate{enabled: false}, status: http.StatusForbidden},
		{name: "error", gate: testGate{err: errors.New("feature provider unavailable")}, status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			authorizer := &testAuthorizer{}
			handler := NewRuntime(testenv.NewLogger(t), &testAuthenticator{principal: testPrincipal()}, tc.gate, authorizer, "", "test-cursor-key", nil, nil, nil, nil, nil).Handler()
			req := httptest.NewRequest(http.MethodPost, Path, nil)
			req.Header.Set("Authorization", "Bearer access-token")
			res := httptest.NewRecorder()

			handler.ServeHTTP(res, req)

			require.Equal(t, tc.status, res.Code)
			require.Zero(t, authorizer.calls)
		})
	}
}

func TestRuntimeHandlerClassifiesAuthenticationFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		authErr   error
		status    int
		telemetry OAuthEvent
	}{
		{name: "unavailable", authErr: ErrUnavailable, status: http.StatusServiceUnavailable, telemetry: OAuthEvent{Operation: "runtime_auth", Outcome: "temporarily_unavailable"}},
		{name: "unauthorized", authErr: ErrUnauthorized, status: http.StatusUnauthorized, telemetry: OAuthEvent{Operation: "runtime_auth", Outcome: "unauthorized"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			telemetry := &testOAuthTelemetry{}
			handler := NewRuntime(testenv.NewLogger(t), &testAuthenticator{err: tc.authErr}, testGate{enabled: true}, &testAuthorizer{}, "", "test-cursor-key", nil, nil, nil, nil, nil).WithOAuthTelemetry(telemetry).Handler()
			req := httptest.NewRequest(http.MethodPost, Path, nil)
			req.Header.Set("Authorization", "Bearer access-token")
			res := httptest.NewRecorder()

			handler.ServeHTTP(res, req)

			require.Equal(t, tc.status, res.Code)
			require.Equal(t, []OAuthEvent{tc.telemetry}, telemetry.events)
		})
	}
}

func TestRuntimeHandlerRequiresLiveOrganizationAdmin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		err       error
		status    int
		telemetry OAuthEvent
	}{
		{name: "denied", err: ErrForbidden, status: http.StatusForbidden, telemetry: OAuthEvent{Operation: "runtime_auth", Outcome: "access_denied", Reason: "authorization_denied"}},
		{name: "unavailable", err: ErrUnavailable, status: http.StatusServiceUnavailable, telemetry: OAuthEvent{Operation: "runtime_auth", Outcome: "temporarily_unavailable"}},
		{name: "unexpected error", err: errors.New("authorization store unavailable"), status: http.StatusServiceUnavailable, telemetry: OAuthEvent{Operation: "runtime_auth", Outcome: "temporarily_unavailable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			authorizer := &testAuthorizer{err: tc.err}
			telemetry := &testOAuthTelemetry{}
			handler := NewRuntime(testenv.NewLogger(t), &testAuthenticator{principal: testPrincipal()}, testGate{enabled: true}, authorizer, "", "test-cursor-key", nil, nil, nil, nil, nil).WithOAuthTelemetry(telemetry).Handler()
			req := httptest.NewRequest(http.MethodPost, Path, nil)
			req.Header.Set("Authorization", "Bearer access-token")
			res := httptest.NewRecorder()

			handler.ServeHTTP(res, req)

			require.Equal(t, tc.status, res.Code)
			require.Equal(t, 1, authorizer.calls)
			require.Equal(t, []OAuthEvent{tc.telemetry}, telemetry.events)
		})
	}
}

func TestRuntimeHandlerRecordsReadyAfterSuccessfulToolsList(t *testing.T) {
	t.Parallel()

	recorder := &testReadinessRecorder{}
	handler := NewRuntime(
		testenv.NewLogger(t),
		&testAuthenticator{principal: testPrincipal()},
		testGate{enabled: true},
		&testAuthorizer{},
		"",
		"test-cursor-key",
		nil,
		nil,
		nil,
		recorder,
		nil,
	).Handler()
	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer access-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, 1, recorder.calls)
	require.Equal(t, testPrincipal(), recorder.principal)
}

func TestRuntimeAuthenticateAcceptsCaseInsensitiveBearer(t *testing.T) {
	t.Parallel()

	authenticator := &testAuthenticator{principal: testPrincipal()}
	runtime := NewRuntime(testenv.NewLogger(t), authenticator, testGate{enabled: true}, &testAuthorizer{}, "", "test-cursor-key", nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, Path, nil)
	req.Header.Set("Authorization", "bearer  access-token  ")

	principal, err := runtime.authenticate(req)

	require.NoError(t, err)
	require.Equal(t, testPrincipal(), principal)
	require.Equal(t, "access-token", authenticator.token)
}

func TestRuntimeAuthenticateRejectsIncompletePrincipal(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime(testenv.NewLogger(t), &testAuthenticator{principal: Principal{UserID: "user"}}, testGate{enabled: true}, &testAuthorizer{}, "", "test-cursor-key", nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, Path, nil)
	req.Header.Set("Authorization", "Bearer access-token")

	_, err := runtime.authenticate(req)

	require.ErrorIs(t, err, ErrUnauthorized)
}

type testReadinessRecorder struct {
	principal Principal
	calls     int
}

func (r *testReadinessRecorder) RecordReady(_ context.Context, principal Principal, _ time.Time) error {
	r.calls++
	r.principal = principal
	return nil
}

type testOAuthTelemetry struct {
	events []OAuthEvent
}

func (t *testOAuthTelemetry) Record(_ context.Context, event OAuthEvent) {
	t.events = append(t.events, event)
}

func (*testOAuthTelemetry) RecordRefreshSuccess(context.Context, time.Duration, time.Duration) {}

func (*testOAuthTelemetry) RecordTerminalTransition(context.Context, platformoauth.ReauthorizationReason) {
}

type testAuthenticator struct {
	principal Principal
	err       error
	calls     int
	token     string
}

func (a *testAuthenticator) Authenticate(_ context.Context, token string) (Principal, error) {
	a.calls++
	a.token = token
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

func TestBoundedRows(t *testing.T) {
	t.Parallel()

	rows := []int{1, 2, 3}

	got, truncated := boundedRows(rows, 2)
	require.Equal(t, []int{1, 2}, got)
	require.True(t, truncated)

	got, truncated = boundedRows(rows, 3)
	require.Equal(t, rows, got)
	require.False(t, truncated)
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

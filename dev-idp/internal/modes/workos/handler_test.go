package workos

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/plog"
)

func newTestHandler(t *testing.T, cfg Config, emulator http.Handler) *Handler {
	t.Helper()
	h, err := NewHandler(cfg, emulator, nil, plog.NewLogger(io.Discard), tracenoop.NewTracerProvider(), nil)
	require.NoError(t, err)
	return h
}

// stubHandler records whether it was reached and echoes a marker.
func stubHandler(marker string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, marker)
	})
}

func TestParseBackend(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want Backend
	}{
		{in: "", want: BackendLocal},
		{in: "local", want: BackendLocal},
		{in: "workos", want: BackendWorkOS},
	} {
		got, err := ParseBackend(tc.in)
		require.NoError(t, err, "ParseBackend(%q)", tc.in)
		require.Equal(t, tc.want, got, "ParseBackend(%q)", tc.in)
	}

	_, err := ParseBackend("mock-workos")
	require.Error(t, err, "retired mode names must not silently resolve")
	require.Contains(t, err.Error(), "GRAM_DEVIDP_BACKEND")
}

// The emulator owns every REST path under the local backend, including
// authenticate — there is no proxy to fall through to.
func TestLocalBackendRoutesRestToEmulator(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, Config{Backend: BackendLocal, UpstreamURL: "", APIKey: ""}, stubHandler("emulator"))

	for _, path := range []string{
		"/user_management/authenticate",
		"/user_management/users/user_123",
		"/organizations/org_123/roles",
	} {
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, "emulator", rec.Body.String(), "path %s should reach the emulator", path)
	}
}

// Under the WorkOS backend the emulator is out of the picture, but
// authenticate must still be served locally rather than proxied upstream —
// real WorkOS would reject a code the dev-idp minted.
func TestWorkOSBackendKeepsAuthenticateLocal(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(stubHandler("upstream"))
	t.Cleanup(upstream.Close)

	h := newTestHandler(t, Config{
		Backend:     BackendWorkOS,
		UpstreamURL: upstream.URL,
		APIKey:      "sk_test_fake",
	}, stubHandler("emulator"))

	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/user_management/users/user_123", nil))
	require.Equal(t, "upstream", rec.Body.String(), "ordinary REST calls should proxy upstream")

	// authenticate is handled locally; with a nil db it cannot succeed, but
	// reaching the local handler at all proves it was not proxied.
	rec = httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/user_management/authenticate", http.NoBody))
	require.NotEqual(t, "upstream", rec.Body.String(), "authenticate must never be proxied to real WorkOS")
}

// The inspection routes read the live WorkOS API through a client that only
// exists under BackendWorkOS. Under BackendLocal they must say so, not
// dereference a nil client.
func TestLocalBackendInspectionRoutesDoNotPanic(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, Config{Backend: BackendLocal, UpstreamURL: "", APIKey: ""}, stubHandler("emulator"))

	for _, path := range []string{
		"/_inspect/currentUser",
		"/_inspect/users/someone@example.com",
		"/_inspect/organizations/org_123",
	} {
		rec := httptest.NewRecorder()
		require.NotPanics(t, func() {
			h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		}, "path %s", path)
		require.Equal(t, http.StatusConflict, rec.Code, "path %s", path)
		require.Contains(t, rec.Body.String(), "GRAM_DEVIDP_BACKEND=workos")
	}
}

// The proxy borrows the configured API key only when the caller did not
// bring one of their own.
func TestProxyInjectsAPIKeyOnlyWhenAbsent(t *testing.T) {
	t.Parallel()

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	h := newTestHandler(t, Config{
		Backend:     BackendWorkOS,
		UpstreamURL: upstream.URL,
		APIKey:      "sk_test_configured",
	}, stubHandler("emulator"))

	h.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/organizations/org_1", nil))
	require.Equal(t, "Bearer sk_test_configured", gotAuth, "a caller without credentials borrows the configured key")

	req := httptest.NewRequest(http.MethodGet, "/organizations/org_1", nil)
	req.Header.Set("Authorization", "Bearer sk_test_caller")
	h.Handler().ServeHTTP(httptest.NewRecorder(), req)
	require.Equal(t, "Bearer sk_test_caller", gotAuth, "a caller's own credentials pass through untouched")
}

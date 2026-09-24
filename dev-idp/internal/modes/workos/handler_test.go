package workos

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/plog"
)

const testClientValue = "devidp_test_client_value"

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

	h := newTestHandler(t, Config{Backend: BackendLocal, ClientSecret: testClientValue, UpstreamURL: "", APIKey: ""}, stubHandler("emulator"))

	for _, path := range []string{
		"/user_management/authenticate",
		"/user_management/users/user_123",
		"/organizations/org_123/roles",
	} {
		rec := httptest.NewRecorder()
		var body io.Reader = http.NoBody
		if path == "/user_management/authenticate" {
			body = strings.NewReader(`{"client_secret":"` + testClientValue + `"}`)
		}
		req := httptest.NewRequest(http.MethodPost, path, body)
		if path != "/user_management/authenticate" {
			req.Header.Set("Authorization", "Bearer "+testClientValue)
		}
		h.Handler().ServeHTTP(rec, req)
		require.Equal(t, "emulator", rec.Body.String(), "path %s should reach the emulator", path)
	}
}

// Under the WorkOS backend only dev-idp's authorization-code grant is local.
// WorkOS-native grants must retain their body and reach the upstream API.
func TestWorkOSBackendRoutesAuthenticateByGrant(t *testing.T) {
	t.Parallel()

	var upstreamBody string
	var upstreamAuthorization string
	var upstreamReadErr error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		upstreamReadErr = err
		if err != nil {
			http.Error(w, "read request body", http.StatusInternalServerError)
			return
		}
		upstreamBody = string(body)
		upstreamAuthorization = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "upstream")
	}))
	t.Cleanup(upstream.Close)

	h := newTestHandler(t, Config{
		Backend:      BackendWorkOS,
		ClientSecret: testClientValue,
		UpstreamURL:  upstream.URL,
		APIKey:       "sk_test_upstream",
	}, stubHandler("emulator"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/user_management/users/user_123", nil)
	req.Header.Set("Authorization", "Bearer "+testClientValue)
	h.Handler().ServeHTTP(rec, req)
	require.Equal(t, "upstream", rec.Body.String(), "ordinary REST calls should proxy upstream")
	require.Equal(t, "Bearer sk_test_upstream", upstreamAuthorization)

	// An authorization-code request missing its code fails in the local handler
	// rather than reaching WorkOS.
	rec = httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/user_management/authenticate",
		strings.NewReader(`{"grant_type":"authorization_code","client_secret":"`+testClientValue+`"}`)))
	require.NotEqual(t, "upstream", rec.Body.String(), "authorization_code must stay local")

	magicBody := `{"grant_type":"urn:workos:oauth:grant-type:magic-auth:code","email":"invitee@example.com","code":"123456","client_secret":"` + testClientValue + `"}`
	rec = httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/user_management/authenticate", strings.NewReader(magicBody)))
	require.NoError(t, upstreamReadErr)
	require.Equal(t, "upstream", rec.Body.String(), "Magic Auth must reach WorkOS")
	require.JSONEq(t, `{"grant_type":"urn:workos:oauth:grant-type:magic-auth:code","email":"invitee@example.com","code":"123456","client_secret":"sk_test_upstream"}`, upstreamBody)
	require.Equal(t, "Bearer sk_test_upstream", upstreamAuthorization)
}

func TestWorkOSBackendRejectsOversizedAuthenticateBody(t *testing.T) {
	t.Parallel()

	proxied := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	h := newTestHandler(t, Config{
		Backend:      BackendWorkOS,
		ClientSecret: testClientValue,
		UpstreamURL:  upstream.URL,
		APIKey:       "test-key",
	}, stubHandler("emulator"))
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/user_management/authenticate",
		strings.NewReader(strings.Repeat("x", maxAuthenticateBodyBytes+1))))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.False(t, proxied)
}

// The inspection routes read the live WorkOS API through a client that only
// exists under BackendWorkOS. Under BackendLocal they must say so, not
// dereference a nil client.
func TestLocalBackendInspectionRoutesDoNotPanic(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, Config{Backend: BackendLocal, ClientSecret: testClientValue, UpstreamURL: "", APIKey: ""}, stubHandler("emulator"))

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

// Downstream credentials terminate at dev-idp and are always replaced before
// the request crosses into WorkOS.
func TestProxyReplacesDownstreamCredential(t *testing.T) {
	t.Parallel()

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	h := newTestHandler(t, Config{
		Backend:      BackendWorkOS,
		ClientSecret: testClientValue,
		UpstreamURL:  upstream.URL,
		APIKey:       "sk_test_upstream",
	}, stubHandler("emulator"))

	req := httptest.NewRequest(http.MethodGet, "/organizations/org_1", nil)
	req.Header.Set("Authorization", "Bearer "+testClientValue)
	h.Handler().ServeHTTP(httptest.NewRecorder(), req)
	require.Equal(t, "Bearer sk_test_upstream", gotAuth)
}

func TestRejectsInvalidDownstreamCredential(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, Config{Backend: BackendLocal, ClientSecret: testClientValue, UpstreamURL: "", APIKey: ""}, stubHandler("emulator"))
	req := httptest.NewRequest(http.MethodGet, "/organizations/org_1", nil)
	req.Header.Set("Authorization", "Bearer wrong-secret")
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.NotEqual(t, "emulator", rec.Body.String())
}

func TestRejectsMisCasedAuthenticateCredential(t *testing.T) {
	t.Parallel()

	proxied := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	h := newTestHandler(t, Config{
		Backend:      BackendWorkOS,
		ClientSecret: testClientValue,
		UpstreamURL:  upstream.URL,
		APIKey:       "sk_test_upstream",
	}, stubHandler("emulator"))
	req := httptest.NewRequest(http.MethodPost, "/user_management/authenticate",
		strings.NewReader(`{"grant_type":"urn:workos:oauth:grant-type:magic-auth:code","CLIENT_SECRET":"`+testClientValue+`"}`))
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, proxied)
}

func TestEmulatorBrowserRoutesAreNotPublicUnderWorkOSBackend(t *testing.T) {
	t.Parallel()

	proxied := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	workosHandler := newTestHandler(t, Config{
		Backend:      BackendWorkOS,
		ClientSecret: testClientValue,
		UpstreamURL:  upstream.URL,
		APIKey:       "sk_test_upstream",
	}, stubHandler("emulator"))
	localHandler := newTestHandler(t, Config{
		Backend:      BackendLocal,
		ClientSecret: testClientValue,
		UpstreamURL:  "",
		APIKey:       "",
	}, stubHandler("emulator"))

	for _, path := range []string{"/portal", "/passwordless/sessions/session_1/authorize"} {
		rec := httptest.NewRecorder()
		workosHandler.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusUnauthorized, rec.Code)

		rec = httptest.NewRecorder()
		localHandler.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, "emulator", rec.Body.String())
	}
	require.False(t, proxied)
}

// Only the registered inspection GETs are public. Anything else under
// /_inspect/ falls through to the catch-all upstream proxy, so exempting it
// from client authentication would hand the real WorkOS API to an
// unauthenticated caller.
func TestUnregisteredInspectPathsAreNotPublicUnderWorkOSBackend(t *testing.T) {
	t.Parallel()

	proxied := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	h := newTestHandler(t, Config{
		Backend:      BackendWorkOS,
		ClientSecret: testClientValue,
		UpstreamURL:  upstream.URL,
		APIKey:       "sk_test_upstream",
	}, stubHandler("emulator"))

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/_inspect/currentUser"},
		{http.MethodGet, "/_inspect/not-a-route"},
		{http.MethodDelete, "/_inspect/organizations/org_1"},
	} {
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
	require.False(t, proxied, "an unauthenticated request must never reach upstream")
}

func TestWorkOSBackendRewritesSSOTokenCredential(t *testing.T) {
	t.Parallel()

	var upstreamForm url.Values
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request body", http.StatusInternalServerError)
			return
		}
		upstreamForm, err = url.ParseQuery(string(body))
		if err != nil {
			http.Error(w, "parse request body", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	h := newTestHandler(t, Config{
		Backend:      BackendWorkOS,
		ClientSecret: testClientValue,
		UpstreamURL:  upstream.URL,
		APIKey:       "sk_test_upstream",
	}, stubHandler("emulator"))
	form := url.Values{
		"client_secret": {testClientValue},
		"code":          {"auth-code"},
	}
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sso/token", strings.NewReader(form.Encode())))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "sk_test_upstream", upstreamForm.Get("client_secret"))
}

func TestWorkOSAuthorizationCodeClientMismatchDoesNotConsumeCode(t *testing.T) {
	t.Parallel()

	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	queries := repo.New(db)
	user, err := queries.CreateUser(t.Context(), repo.CreateUserParams{
		ID:           uuid.New(),
		Email:        "code-client@example.com",
		DisplayName:  "Code Client",
		PhotoUrl:     sql.NullString{String: "", Valid: false},
		GithubHandle: sql.NullString{String: "", Valid: false},
		Admin:        false,
		Whitelisted:  true,
	})
	require.NoError(t, err)
	_, err = queries.CreateAuthCode(t.Context(), repo.CreateAuthCodeParams{
		Code:                "client-bound-code",
		UserID:              user.ID,
		ClientID:            "expected-client",
		RedirectUri:         "http://localhost/callback",
		CodeChallenge:       sql.NullString{String: "", Valid: false},
		CodeChallengeMethod: sql.NullString{String: "", Valid: false},
		Scope:               sql.NullString{String: "", Valid: false},
		ExpiresAt:           time.Now().Add(time.Minute),
	})
	require.NoError(t, err)

	h, err := NewHandler(Config{
		Backend:      BackendWorkOS,
		ClientSecret: testClientValue,
		UpstreamURL:  "http://workos.example",
		APIKey:       "test-key",
	}, stubHandler("emulator"), nil, plog.NewLogger(io.Discard), tracenoop.NewTracerProvider(), db)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/user_management/authenticate", strings.NewReader(
		`{"grant_type":"authorization_code","code":"client-bound-code","client_id":"wrong-client","client_secret":"`+testClientValue+`"}`)))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	_, err = queries.ConsumeAuthCodeForClient(t.Context(), repo.ConsumeAuthCodeForClientParams{
		Code:     "client-bound-code",
		ClientID: "expected-client",
		Ts:       time.Now(),
	})
	require.NoError(t, err, "a mismatched client must not burn the authorization code")
}

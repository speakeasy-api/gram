package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
)

const testAdminHD = "example.com"

// newTestOIDCClient starts a stub OIDC provider that serves a discovery
// document and the supplied userinfo handler, then points a real OIDCClient at
// it. The verifier calls userinfo on every request, so a handler that returns
// 401 simulates a token the provider has revoked.
func newTestOIDCClient(t *testing.T, userinfo http.HandlerFunc) *OIDCClient {
	t.Helper()
	return newTestOIDCClientWithToken(t, userinfo, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
}

func newTestOIDCClientWithToken(t *testing.T, userinfo, token http.HandlerFunc) *OIDCClient {
	t.Helper()

	var issuer string

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/auth",
			"token_endpoint":         issuer + "/token",
			"userinfo_endpoint":      issuer + "/userinfo",
			"jwks_uri":               issuer + "/keys",
		})
	})
	mux.HandleFunc("/userinfo", userinfo)
	mux.HandleFunc("/token", token)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer = srv.URL

	provider, err := oidc.NewProvider(t.Context(), srv.URL)
	require.NoError(t, err)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	return NewOIDCClient(OIDCClientOptions{
		HTTPClient:   policy.Client(),
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/admin/auth.callback",
		AllowedHDs:   []string{testAdminHD},
		Provider:     provider,
	})
}

// userinfoOK serves a verified admin identity for the given subject.
func userinfoOK(sub, email string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":            sub,
			"email":          email,
			"email_verified": true,
			"hd":             testAdminHD,
		})
	}
}

func newTestSessionService(t *testing.T, oidcClient *OIDCClient) *Service {
	t.Helper()

	logger := testenv.NewLogger(t)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)

	sessions := NewSessionStore(
		cache.NewTypedObjectCache[Session](
			logger,
			cache.NewRedisCacheAdapter(redisClient),
			cache.SuffixNone,
		),
		enc,
	)

	return &Service{
		logger:   logger,
		oidc:     oidcClient,
		sessions: sessions,
		verifier: NewVerifier(logger, sessions, oidcClient),
		trial:    trialemails.NoopNotifier{},
	}
}

// callSessionGet drives the handler through the same middleware and error
// wrapper that Attach mounts it behind.
func callSessionGet(t *testing.T, svc *Service, sessionID string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/admin/session.get", nil)
	if sessionID != "" {
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	}

	rec := httptest.NewRecorder()
	SessionMiddleware(oops.ErrHandle(svc.logger, svc.handleGetSession)).ServeHTTP(rec, req)

	return rec
}

// TestAttach_MountsSessionRoute proves the hand-written route reaches the
// handler on the same muxer that carries the generated admin routes. A missing
// or conflicting pattern would give 404 instead of 401.
func TestAttach_MountsSessionRoute(t *testing.T) {
	t.Parallel()

	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-mounted", "operator@example.com")))
	svc.tracer = testenv.NewTracerProvider(t).Tracer("admin_test")

	mux := goahttp.NewMuxer()
	Attach(mux, svc)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/session.get", nil))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleGetSession_ReturnsIdentity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-identity", "operator@example.com")))

	sessionID, err := svc.sessions.Store(ctx, StoreParams{
		Email:        "operator@example.com",
		Name:         "Test Operator",
		OIDCSubject:  "sub-identity",
		HD:           testAdminHD,
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	rec := callSessionGet(t, svc, sessionID)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got sessionInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, sessionInfo{Email: "operator@example.com", Name: "Test Operator"}, got)
}

func TestHandleGetSession_RefreshesExpiredTokens(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	oidcClient := newTestOIDCClientWithToken(
		t,
		userinfoOK("sub-refresh", "operator@example.com"),
		func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			require.Equal(t, "refresh_token", r.PostForm.Get("grant_type"))
			require.Equal(t, "old-refresh-token", r.PostForm.Get("refresh_token"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "new-access-token",
				"refresh_token": "new-refresh-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		},
	)
	svc := newTestSessionService(t, oidcClient)

	sessionID, err := svc.sessions.Store(ctx, StoreParams{
		Email:        "operator@example.com",
		Name:         "Test Operator",
		OIDCSubject:  "sub-refresh",
		HD:           testAdminHD,
		AccessToken:  "old-access-token",
		RefreshToken: "old-refresh-token",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, callSessionGet(t, svc, sessionID).Code)

	session, err := svc.sessions.Get(ctx, sessionID)
	require.NoError(t, err)
	accessToken, err := svc.sessions.DecryptAccessToken(session)
	require.NoError(t, err)
	require.Equal(t, "new-access-token", accessToken)
	refreshToken, err := svc.sessions.DecryptRefreshToken(session)
	require.NoError(t, err)
	require.Equal(t, "new-refresh-token", refreshToken)
	require.True(t, session.AccessTokenExpiresAt.After(time.Now()))
}

func TestAuthCodeURLRequestsConsentForInteractiveLogin(t *testing.T) {
	t.Parallel()

	client := newTestOIDCClient(t, userinfoOK("sub-consent", "operator@example.com"))
	interactive, err := url.Parse(client.AuthCodeURL("state", "challenge", ""))
	require.NoError(t, err)
	require.Equal(t, "consent", interactive.Query().Get("prompt"))

	silent, err := url.Parse(client.AuthCodeURL("state", "challenge", "none"))
	require.NoError(t, err)
	require.Equal(t, "none", silent.Query().Get("prompt"))
}

func TestHandleGetSession_NoCookie(t *testing.T) {
	t.Parallel()

	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-no-cookie", "operator@example.com")))

	require.Equal(t, http.StatusUnauthorized, callSessionGet(t, svc, "").Code)
}

func TestHandleGetSession_UnknownSession(t *testing.T) {
	t.Parallel()

	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-unknown", "operator@example.com")))

	require.Equal(t, http.StatusUnauthorized, callSessionGet(t, svc, "not-a-session").Code)
}

func TestHandleGetSession_ProviderRejectsToken(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc := newTestSessionService(t, newTestOIDCClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	sessionID, err := svc.sessions.Store(ctx, StoreParams{
		Email:        "operator@example.com",
		Name:         "Test Operator",
		OIDCSubject:  "sub-revoked",
		HD:           testAdminHD,
		AccessToken:  "revoked-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	require.Equal(t, http.StatusUnauthorized, callSessionGet(t, svc, sessionID).Code)

	_, err = svc.sessions.Get(ctx, sessionID)
	require.Error(t, err, "a session the provider rejects must be deleted")
}

func TestHandleGetSession_SubjectMismatch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-attacker", "operator@example.com")))

	sessionID, err := svc.sessions.Store(ctx, StoreParams{
		Email:        "operator@example.com",
		Name:         "Test Operator",
		OIDCSubject:  "sub-owner",
		HD:           testAdminHD,
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	require.Equal(t, http.StatusUnauthorized, callSessionGet(t, svc, sessionID).Code)
}

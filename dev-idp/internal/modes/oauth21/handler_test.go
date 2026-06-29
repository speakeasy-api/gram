package oauth21

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
	"github.com/speakeasy-api/gram/plog"
)

func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return plog.NewLogger(io.Discard)
}

func newTestTracer(t *testing.T) trace.TracerProvider {
	t.Helper()
	return tracenoop.NewTracerProvider()
}

func TestValidatePKCES256_AcceptsCorrectVerifier(t *testing.T) {
	t.Parallel()

	verifier := "the-quick-brown-fox-jumps-over-the-lazy-dog"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])

	require.True(t, validatePKCES256(verifier, challenge))
}

func TestValidatePKCES256_RejectsWrongVerifier(t *testing.T) {
	t.Parallel()

	verifier := "correct-verifier"
	digest := sha256.Sum256([]byte("different-verifier"))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])

	require.False(t, validatePKCES256(verifier, challenge))
}

func TestScopeContains(t *testing.T) {
	t.Parallel()

	require.True(t, scopeContains("openid email profile", "openid"))
	require.True(t, scopeContains("openid", "openid"))
	require.False(t, scopeContains("email profile", "openid"))
	require.False(t, scopeContains("", "openid"))
	require.False(t, scopeContains("openidx", "openid"))
}

func TestOIDCDiscoveryDocumentShape(t *testing.T) {
	t.Parallel()

	ks, err := keystore.New(nil, newTestLogger(t))
	require.NoError(t, err)
	h := NewHandler(
		Config{ExternalURL: "https://idp.example.com"},
		ks,
		newTestLogger(t),
		newTestTracer(t),
		nil,
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	h.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))

	require.Equal(t, "https://idp.example.com/oauth2-1", doc["issuer"])
	require.Equal(t, "https://idp.example.com/oauth2-1/authorize", doc["authorization_endpoint"])
	require.Equal(t, "https://idp.example.com/oauth2-1/token", doc["token_endpoint"])
	require.Equal(t, "https://idp.example.com/oauth2-1/userinfo", doc["userinfo_endpoint"])
	require.Equal(t, "https://idp.example.com/oauth2-1/.well-known/jwks.json", doc["jwks_uri"])

	// OIDC-required fields beyond the AS metadata
	require.NotEmpty(t, doc["subject_types_supported"])
	require.NotEmpty(t, doc["id_token_signing_alg_values_supported"])
	require.NotEmpty(t, doc["claims_supported"])

	// PKCE S256 must be advertised
	methods, ok := doc["code_challenge_methods_supported"].([]any)
	require.True(t, ok)
	require.Contains(t, methods, "S256")
}

func TestAuthorizeRejectsRequestsWithoutPKCE(t *testing.T) {
	t.Parallel()

	ks, err := keystore.New(nil, newTestLogger(t))
	require.NoError(t, err)
	h := NewHandler(
		Config{ExternalURL: "https://idp.example.com"},
		ks,
		newTestLogger(t),
		newTestTracer(t),
		nil, // db unused: PKCE check fires before any DB call
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/authorize?response_type=code&client_id=c&redirect_uri=https%3A%2F%2Fapp.example%2Fcb&state=s",
		nil)
	h.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_request", body["error"])
	require.Contains(t, body["error_description"], "code_challenge")
}

func TestAuthorizeRejectsNonS256Challenge(t *testing.T) {
	t.Parallel()

	ks, err := keystore.New(nil, newTestLogger(t))
	require.NoError(t, err)
	h := NewHandler(
		Config{ExternalURL: "https://idp.example.com"},
		ks,
		newTestLogger(t),
		newTestTracer(t),
		nil,
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/authorize?response_type=code&client_id=c&redirect_uri=https%3A%2F%2Fapp.example%2Fcb&code_challenge=abc&code_challenge_method=plain",
		nil)
	h.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_request", body["error"])
	require.Contains(t, body["error_description"], "S256")
}

// =============================================================================
// CIMD (Client ID Metadata Document) — client_id is a hosted-document URL
// =============================================================================

func TestASMetadataAdvertisesCIMDSupport(t *testing.T) {
	t.Parallel()

	ks, err := keystore.New(nil, newTestLogger(t))
	require.NoError(t, err)
	h := NewHandler(Config{ExternalURL: "https://idp.example.com"}, ks, newTestLogger(t), newTestTracer(t), nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	h.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Equal(t, true, doc["client_id_metadata_document_supported"])
}

// cimdDocServer serves a fixed JSON client metadata document at every path.
func cimdDocServer(t *testing.T, doc map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// cimdSelfDocServer serves a document whose client_id equals the server's own
// URL (the CIMD invariant), with the supplied redirect_uris.
func cimdSelfDocServer(t *testing.T, redirectURIs []string) *httptest.Server {
	t.Helper()
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id":     base,
			"redirect_uris": redirectURIs,
		})
	}))
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

func cimdAuthorizeRequest(clientID, redirectURI string) *http.Request {
	// PKCE is verified at the token leg, so any non-empty S256 challenge passes
	// the authorize-time check and lets the request reach CIMD validation.
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", "abc")
	q.Set("code_challenge_method", "S256")
	return httptest.NewRequest(http.MethodGet, "/authorize?"+q.Encode(), nil)
}

func newCIMDTestHandler(t *testing.T) *Handler {
	t.Helper()
	ks, err := keystore.New(nil, newTestLogger(t))
	require.NoError(t, err)
	// db is nil: every assertion below rejects before the auth-code DB write.
	return NewHandler(Config{ExternalURL: "https://idp.example.com"}, ks, newTestLogger(t), newTestTracer(t), nil)
}

func TestAuthorizeCIMDRejectsDocumentClientIDMismatch(t *testing.T) {
	t.Parallel()

	srv := cimdDocServer(t, map[string]any{
		"client_id":     "https://attacker.example/other",
		"redirect_uris": []string{"https://app.example/cb"},
	})

	rec := httptest.NewRecorder()
	newCIMDTestHandler(t).Handler().ServeHTTP(rec, cimdAuthorizeRequest(srv.URL, "https://app.example/cb"))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_client", body["error"])
	require.Contains(t, body["error_description"], "does not match")
}

func TestAuthorizeCIMDRejectsRedirectURINotInDocument(t *testing.T) {
	t.Parallel()

	srv := cimdSelfDocServer(t, []string{"https://other.example/cb"})

	rec := httptest.NewRecorder()
	newCIMDTestHandler(t).Handler().ServeHTTP(rec, cimdAuthorizeRequest(srv.URL, "https://app.example/cb"))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_request", body["error"])
	require.Contains(t, body["error_description"], "redirect_uri")
}

func TestAuthorizeCIMDRejectsUnfetchableDocument(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	rec := httptest.NewRecorder()
	newCIMDTestHandler(t).Handler().ServeHTTP(rec, cimdAuthorizeRequest(srv.URL, "https://app.example/cb"))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_client", body["error"])
	require.Contains(t, body["error_description"], "could not fetch")
}

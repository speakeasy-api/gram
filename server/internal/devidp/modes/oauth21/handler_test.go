package oauth21

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/devidp/keystore"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

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

	ks, err := keystore.New(nil, testenv.NewLogger(t))
	require.NoError(t, err)
	h := NewHandler(
		Config{ExternalURL: "https://idp.example.com"},
		ks,
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
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

	ks, err := keystore.New(nil, testenv.NewLogger(t))
	require.NoError(t, err)
	h := NewHandler(
		Config{ExternalURL: "https://idp.example.com"},
		ks,
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
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

	ks, err := keystore.New(nil, testenv.NewLogger(t))
	require.NoError(t, err)
	h := NewHandler(
		Config{ExternalURL: "https://idp.example.com"},
		ks,
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
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

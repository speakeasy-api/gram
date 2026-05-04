package oauth2

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	ks, err := keystore.New(nil, newTestLogger(t))
	require.NoError(t, err)
	return NewHandler(
		Config{ExternalURL: "https://idp.example.com"},
		ks,
		newTestLogger(t),
		newTestTracer(t),
		nil,
	)
}

func TestOIDCDiscoveryDocumentShape(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	h.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))

	require.Equal(t, "https://idp.example.com/oauth2", doc["issuer"])
	require.Equal(t, "https://idp.example.com/oauth2/authorize", doc["authorization_endpoint"])
	require.Equal(t, "https://idp.example.com/oauth2/token", doc["token_endpoint"])

	// OAuth 2.0 doesn't have DCR; the discovery doc should NOT advertise
	// a registration_endpoint.
	require.NotContains(t, doc, "registration_endpoint")

	// PKCE S256 is still advertised — it's optional but supported.
	methods, ok := doc["code_challenge_methods_supported"].([]any)
	require.True(t, ok)
	require.Contains(t, methods, "S256")
}

func TestNoRegisterEndpoint(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/register", nil)
	h.Handler().ServeHTTP(rec, req)

	// /register isn't routed; ServeMux returns 404.
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAuthorizeRejectsBadPKCEMethod(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)

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

// The oauth2-vs-oauth21 acceptance test for "no PKCE works" requires a
// live database (the handler reaches resolveCurrentUserID before PKCE
// would have been validated, and that path needs the current_users
// row). It belongs in the integration test ticket — covered there
// alongside the rest of the per-mode end-to-end coverage.


package resourceas

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/plog"
)

func newDiscoveryHandler(t *testing.T, tlsServer *httptest.Server) *Handler {
	t.Helper()
	h := NewHandler(Config{ExternalURL: "http://idp.local", AudienceAliases: nil}, nil, plog.NewLogger(io.Discard), tracenoop.NewTracerProvider(), nil)
	if tlsServer != nil {
		h.httpClient.Transport = tlsServer.Client().Transport
	}
	return h
}

func serveJSON(doc any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}
}

func TestDiscoverRejectsIssuerMismatch(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	doc := map[string]string{"issuer": "https://elsewhere.example", "jwks_uri": srv.URL + "/jwks"}
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/idp", serveJSON(doc))
	mux.HandleFunc("GET /idp/.well-known/openid-configuration", serveJSON(doc))

	_, err := newDiscoveryHandler(t, nil).discoverJWKSURI(t.Context(), srv.URL+"/idp")
	require.ErrorContains(t, err, "names issuer")
}

func TestDiscoverRejectsHTTPJWKSUnderHTTPSIssuer(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	issuer := srv.URL + "/idp"
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/idp", serveJSON(map[string]string{"issuer": issuer, "jwks_uri": "http://plain.example/jwks"}))

	_, err := newDiscoveryHandler(t, srv).discoverJWKSURI(t.Context(), issuer)
	require.ErrorContains(t, err, "unusable jwks_uri")
}

func TestDiscoverRejectsHostlessJWKS(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer := srv.URL + "/idp"
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/idp", serveJSON(map[string]string{"issuer": issuer, "jwks_uri": "/jwks"}))

	_, err := newDiscoveryHandler(t, nil).discoverJWKSURI(t.Context(), issuer)
	require.ErrorContains(t, err, "unusable jwks_uri")
}

func TestDiscoverFallsBackToOpenIDAfter404(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer := srv.URL + "/idp"
	mux.HandleFunc("GET /idp/.well-known/openid-configuration", serveJSON(map[string]string{"issuer": issuer, "jwks_uri": issuer + "/keys"}))

	got, err := newDiscoveryHandler(t, nil).discoverJWKSURI(t.Context(), issuer)
	require.NoError(t, err)
	require.Equal(t, issuer+"/keys", got)
}

func TestDiscoverJoinsErrorsFromBothDocuments(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	_, err := newDiscoveryHandler(t, nil).discoverJWKSURI(t.Context(), srv.URL+"/idp")
	require.ErrorContains(t, err, "/.well-known/oauth-authorization-server/idp")
	require.ErrorContains(t, err, "/idp/.well-known/openid-configuration")
}

func TestDiscoverTrimsTrailingSlashBeforePathInsertion(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer := srv.URL + "/idp/"
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/idp", serveJSON(map[string]string{"issuer": issuer, "jwks_uri": srv.URL + "/keys"}))

	got, err := newDiscoveryHandler(t, nil).discoverJWKSURI(t.Context(), issuer)
	require.NoError(t, err)
	require.Equal(t, srv.URL+"/keys", got)
}

func TestDiscoverRejectsIssuerWithQueryOrFragment(t *testing.T) {
	t.Parallel()
	h := newDiscoveryHandler(t, nil)
	for _, issuer := range []string{"https://idp.example/x?a=1", "https://idp.example/x?", "https://idp.example/x#f"} {
		_, err := h.discoverJWKSURI(t.Context(), issuer)
		require.ErrorContains(t, err, "query or fragment", issuer)
	}
}

func TestFetchRefusesHTTPSToHTTPRedirect(t *testing.T) {
	t.Parallel()
	plain := httptest.NewServer(serveJSON(map[string]string{"issuer": "x", "jwks_uri": "y"}))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.RedirectHandler(plain.URL+"/meta", http.StatusFound))
	t.Cleanup(secure.Close)

	var doc map[string]string
	err := newDiscoveryHandler(t, secure).getJSON(t.Context(), secure.URL+"/meta", &doc)
	require.ErrorContains(t, err, "refusing redirect")
}

func TestFetchJWKSSelectsOnlyRS256SigningKeys(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	jwk := func(kid, use, alg string) map[string]string {
		return map[string]string{
			"kty": "RSA", "kid": kid, "use": use, "alg": alg,
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}
	}
	srv := httptest.NewServer(serveJSON(map[string]any{"keys": []map[string]string{
		jwk("enc", "enc", "RS256"),
		jwk("ps", "sig", "PS256"),
		jwk("good", "sig", "RS256"),
	}}))
	t.Cleanup(srv.Close)
	h := newDiscoveryHandler(t, nil)

	for _, kid := range []string{"enc", "ps"} {
		_, err := h.fetchJWKSKey(t.Context(), srv.URL, kid)
		require.Error(t, err, kid)
	}
	got, err := h.fetchJWKSKey(t.Context(), srv.URL, "good")
	require.NoError(t, err)
	require.Equal(t, key.N, got.N)
	got, err = h.fetchJWKSKey(t.Context(), srv.URL, "")
	require.NoError(t, err)
	require.Equal(t, key.N, got.N)
}

func TestDiscoverRejectsIssuerWithUserinfo(t *testing.T) {
	t.Parallel()
	_, err := newDiscoveryHandler(t, nil).discoverJWKSURI(t.Context(), "https://user:secret@idp.example/x")
	require.ErrorContains(t, err, "userinfo")
}

func TestRefuseDowngradeChecksEachHop(t *testing.T) {
	t.Parallel()
	hop := func(raw string) *http.Request {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, raw, nil)
		require.NoError(t, err)
		return req
	}
	via := []*http.Request{hop("http://a.example/1"), hop("https://b.example/2")}
	require.Error(t, refuseDowngrade(hop("http://c.example/3"), via))
	require.NoError(t, refuseDowngrade(hop("https://c.example/3"), via))
	require.NoError(t, refuseDowngrade(hop("http://b.example/2"), via[:1]))
}

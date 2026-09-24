package gateway

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/speakeasy-api/gram/tunnel/jwks"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/stretchr/testify/require"
)

func publicPEMForTest(t *testing.T, key any) string {
	t.Helper()
	encoded, err := x509.MarshalPKIXPublicKey(key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
}

func TestPublicHandlerServesJWKSWithOnlyPublicKeys(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	gw := newForwardTestGateway(t, Config{ForwardToken: "test-forward", AuthzPublicKeys: publicPEMForTest(t, &key.PublicKey)})
	handler := gw.PublicHandler()
	req := httptest.NewRequest(http.MethodGet, jwks.Path, nil)
	req.Header.Set("Authorization", "invalid")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/jwk-set+json", w.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=300, must-revalidate", w.Header().Get("Cache-Control"))
	require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	etag := w.Header().Get("ETag")
	require.NotEmpty(t, etag)
	var keys jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &keys))
	require.Len(t, keys.Keys, 1)
	thumbprint, err := (&jose.JSONWebKey{Key: &key.PublicKey}).Thumbprint(crypto.SHA256)
	require.NoError(t, err)
	require.Equal(t, base64.RawURLEncoding.EncodeToString(thumbprint), keys.Keys[0].KeyID)
	require.Equal(t, "RS256", keys.Keys[0].Algorithm)
	require.Equal(t, "sig", keys.Keys[0].Use)
	var document map[string][]map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &document))
	for _, secret := range []string{"d", "p", "q", "dp", "dq", "qi", "oth"} {
		require.NotContains(t, document["keys"][0], secret)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(method, jwks.Path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, etag, w.Header().Get("ETag"))
		if method == http.MethodHead {
			require.Empty(t, w.Body.String())
		}
		req.Header.Set("If-None-Match", etag)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotModified, w.Code)
		require.Equal(t, etag, w.Header().Get("ETag"))
		require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
		require.Empty(t, w.Body.String())
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, jwks.Path, nil))
		require.Equal(t, http.StatusMethodNotAllowed, w.Code)
		require.Equal(t, "GET, HEAD", w.Header().Get("Allow"))
	}
}

func TestPublicHandlerEmptyJWKS(t *testing.T) {
	t.Parallel()
	gw := newForwardTestGateway(t, Config{ForwardToken: "test-forward", AuthzPublicKeys: ""})
	w := httptest.NewRecorder()
	gw.PublicHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, jwks.Path, nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"keys":[]}`, w.Body.String())
}

func TestPublicHandlerJWKSStableAcrossOrderingAndDuplicates(t *testing.T) {
	t.Parallel()
	a, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	b, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicA, publicB := publicPEMForTest(t, &a.PublicKey), publicPEMForTest(t, &b.PublicKey)
	var previous *httptest.ResponseRecorder
	for _, bundle := range []string{publicA + publicB, "\n" + publicB + publicA + publicA} {
		gw := newForwardTestGateway(t, Config{ForwardToken: "test-forward", AuthzPublicKeys: bundle})
		w := httptest.NewRecorder()
		gw.PublicHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, jwks.Path, nil))
		require.Equal(t, http.StatusOK, w.Code)
		var keys jose.JSONWebKeySet
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &keys))
		require.Len(t, keys.Keys, 2)
		require.Less(t, keys.Keys[0].KeyID, keys.Keys[1].KeyID)
		if previous != nil {
			require.Equal(t, previous.Body.String(), w.Body.String())
			require.Equal(t, previous.Header().Get("ETag"), w.Header().Get("ETag"))
		}
		previous = w
	}
}

func TestGatewayRejectsPrivateOrMalformedJWKSPublicConfiguration(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	small, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public := publicPEMForTest(t, &key.PublicKey)
	for _, bundle := range []string{
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})),
		string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})),
		publicPEMForTest(t, &small.PublicKey), publicPEMForTest(t, &ec.PublicKey),
		"not a key", public + "junk",
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("invalid DER")})),
	} {
		gw, err := New(Config{ForwardToken: "test-forward", AuthzPublicKeys: bundle},
			NewStaticKeyStore(map[string]string{}), route.NewRouteTable(), slog.Default())
		require.ErrorContains(t, err, "GRAM_AUTHZ_PUBLIC_KEYS")
		require.Nil(t, gw)
		require.NotContains(t, err.Error(), "-----BEGIN")
	}
}

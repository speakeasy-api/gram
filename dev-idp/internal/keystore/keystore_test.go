package keystore_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
	"github.com/speakeasy-api/gram/plog"
)

func newLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return plog.NewLogger(io.Discard)
}

func TestNewGeneratesEphemeralKey(t *testing.T) {
	t.Parallel()

	ks, err := keystore.New(nil, newLogger(t))
	require.NoError(t, err)
	require.NotNil(t, ks.PrivateKey())
	require.NotEmpty(t, ks.KID())
	require.Equal(t, "RS256", ks.SigningAlg())
}

func TestNewLoadsPKCS8PEM(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	ks, err := keystore.New(pemBytes, newLogger(t))
	require.NoError(t, err)
	require.True(t, ks.PrivateKey().Equal(priv))
}

func TestNewLoadsPKCS1PEM(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der := x509.MarshalPKCS1PrivateKey(priv)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})

	ks, err := keystore.New(pemBytes, newLogger(t))
	require.NoError(t, err)
	require.True(t, ks.PrivateKey().Equal(priv))
}

func TestKIDIsStableAcrossInstances(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	a, err := keystore.New(pemBytes, newLogger(t))
	require.NoError(t, err)
	b, err := keystore.New(pemBytes, newLogger(t))
	require.NoError(t, err)

	require.Equal(t, a.KID(), b.KID())
}

func TestJWKSHandlerServesValidDocument(t *testing.T) {
	t.Parallel()

	ks, err := keystore.New(nil, newLogger(t))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	ks.JWKSHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Len(t, doc.Keys, 1)
	jwk := doc.Keys[0]
	require.Equal(t, "RSA", jwk.Kty)
	require.Equal(t, "sig", jwk.Use)
	require.Equal(t, "RS256", jwk.Alg)
	require.Equal(t, ks.KID(), jwk.Kid)
	require.NotEmpty(t, jwk.N)
	require.NotEmpty(t, jwk.E)
}

// Take the handler before rotating, as modes mount it once at boot.
func TestRotateRepublishesOnlyTheNewKey(t *testing.T) {
	t.Parallel()

	ks, err := keystore.New(nil, newLogger(t))
	require.NoError(t, err)
	handler := ks.JWKSHandler()
	retired := ks.KID()

	next, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	require.NoError(t, ks.Rotate(next))

	require.True(t, ks.PrivateKey().Equal(next))
	require.NotEqual(t, retired, ks.KID())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))

	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
		} `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Len(t, doc.Keys, 1, "a rotation replaces the published key rather than adding to it")
	require.Equal(t, ks.KID(), doc.Keys[0].Kid)
}

func TestRotateRejectsNilKey(t *testing.T) {
	t.Parallel()

	ks, err := keystore.New(nil, newLogger(t))
	require.NoError(t, err)
	kid := ks.KID()

	require.Error(t, ks.Rotate(nil))
	require.Equal(t, kid, ks.KID(), "a refused rotation keeps the current key")
}

func TestNewRejectsInvalidPEM(t *testing.T) {
	t.Parallel()

	_, err := keystore.New([]byte("not a pem"), newLogger(t))
	require.Error(t, err)
}

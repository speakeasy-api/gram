package o11y

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
)

func selfSignedCert(t *testing.T, dnsNames []string, ips []net.IP) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "health-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func probeStatus(t *testing.T, serverCert tls.Certificate, trusted []byte) int {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL + "/healthz")
	require.NoError(t, err)
	handler := NewHealthCheckHandler(
		[]*NamedResource[*HTTPEndpoint]{{Name: "api", Resource: &HTTPEndpoint{URL: u, TLSCertificate: trusted}}},
		[]*NamedResource[*pgxpool.Pool]{},
		[]*NamedResource[*redis.Client]{},
		[]*NamedResource[client.Client]{},
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	return rec.Code
}

func TestHealthCheckProbesListenerByIPWithDNSOnlyCertificate(t *testing.T) {
	t.Parallel()
	cert, certPEM := selfSignedCert(t, []string{"dev-machine.example.ts.net"}, nil)
	require.Equal(t, http.StatusOK, probeStatus(t, cert, certPEM))
}

func TestHealthCheckProbesListenerByIPWithIPCertificate(t *testing.T) {
	t.Parallel()
	cert, certPEM := selfSignedCert(t, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	require.Equal(t, http.StatusOK, probeStatus(t, cert, certPEM))
}

func TestHealthCheckRejectsListenerServingAnotherCertificate(t *testing.T) {
	t.Parallel()
	served, _ := selfSignedCert(t, []string{"dev-machine.example.ts.net"}, nil)
	_, trusted := selfSignedCert(t, []string{"dev-machine.example.ts.net"}, nil)
	require.Equal(t, http.StatusServiceUnavailable, probeStatus(t, served, trusted))
}

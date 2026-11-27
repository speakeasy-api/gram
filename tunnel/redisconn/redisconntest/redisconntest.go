// Package redisconntest provides TLS fixtures for tests of Redis clients: a
// throwaway certificate authority and server certificates it signs, and an
// oauth2 token source whose tokens the test controls.
package redisconntest

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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// certLifetime is how long fixture certificates are valid. Tests finish in
// minutes; an hour either side tolerates a skewed clock.
const certLifetime = time.Hour

// CA is a certificate authority that signs server certificates for tests.
type CA struct {
	// PEM is the authority's certificate, for a client's trust store.
	PEM string

	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// NewCA creates a certificate authority.
func NewCA(t *testing.T) *CA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: "redis test CA"},
		NotBefore:             time.Now().Add(-certLifetime),
		NotAfter:              time.Now().Add(certLifetime),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return &CA{
		PEM:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		cert: cert,
		key:  key,
	}
}

// ServerConfig returns a server TLS configuration whose certificate the
// authority signed for the given IP addresses and DNS names.
func (ca *CA) ServerConfig(t *testing.T, ips []net.IP, names []string) *tls.Config {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: "redis test server"},
		NotBefore:    time.Now().Add(-certLifetime),
		NotAfter:     time.Now().Add(certLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
		DNSNames:     names,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{der},
			PrivateKey:  key,
		}},
	}
}

// LoopbackServerConfig returns a server TLS configuration valid for
// 127.0.0.1, where test servers listen.
func (ca *CA) LoopbackServerConfig(t *testing.T) *tls.Config {
	t.Helper()

	return ca.ServerConfig(t, []net.IP{net.IPv4(127, 0, 0, 1)}, nil)
}

func serial(t *testing.T) *big.Int {
	t.Helper()

	limit := new(big.Int).Lsh(big.NewInt(1), 62)
	n, err := rand.Int(rand.Reader, limit)
	require.NoError(t, err)
	return n
}

// TokenSource hands out the token a test sets and counts the requests.
type TokenSource struct {
	mu     sync.Mutex
	token  oauth2.Token
	err    error
	served int
}

// NewTokenSource returns a token source that serves accessToken, valid for
// lifetime from now.
func NewTokenSource(accessToken string, lifetime time.Duration) *TokenSource {
	ts := &TokenSource{mu: sync.Mutex{}, token: oauth2.Token{AccessToken: "", TokenType: "", RefreshToken: "", Expiry: time.Time{}, ExpiresIn: 0}, err: nil, served: 0}
	ts.Set(accessToken, lifetime)
	return ts
}

// Set replaces the token served from now on.
func (ts *TokenSource) Set(accessToken string, lifetime time.Duration) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.token = oauth2.Token{AccessToken: accessToken, TokenType: "Bearer", RefreshToken: "", Expiry: time.Now().Add(lifetime), ExpiresIn: 0}
	ts.err = nil
}

// Fail makes every later request fail with err.
func (ts *TokenSource) Fail(err error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.err = err
}

// Served returns how many tokens have been requested.
func (ts *TokenSource) Served() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	return ts.served
}

// Token implements oauth2.TokenSource.
func (ts *TokenSource) Token() (*oauth2.Token, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.served++
	if ts.err != nil {
		return nil, ts.err
	}
	token := ts.token
	return &token, nil
}

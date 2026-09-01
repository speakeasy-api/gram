package oauthtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockoidc "github.com/speakeasy-api/gram/mock-oidc"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// WorkloadIssuer is a real OIDC issuer standing in for the platform that
// vouches for a workload: it serves a discovery document and a key set over
// HTTP, and mints assertions signed with the key it publishes.
//
// The point of it being real is what an in-process key source cannot express.
// A synthetic source hands the verifier a key set directly, so "the issuer
// rotated its key" and "the issuer is unreachable" have no honest
// representation — there is nothing to rotate and nothing to take away. Here
// both are the server doing what a real one does, and the verifier reaches it
// the way it reaches any customer's issuer: discovery, a fetch, a cache.
type WorkloadIssuer struct {
	// URL is the issuer identifier, which is also where its documents live.
	URL string

	server  *httptest.Server
	logger  *slog.Logger
	config  *mockoidc.Config
	mu      sync.Mutex
	signer  jose.Signer
	keyID   string
	stopped bool
}

// LaunchWorkloadIssuer starts an issuer and stops it when the test ends.
func LaunchWorkloadIssuer(t *testing.T) *WorkloadIssuer {
	t.Helper()

	config := &mockoidc.Config{Provider: mockoidc.ProviderConfig{}}
	// Discarded: mock-oidc's request logging is noise for these tests, and
	// nothing here asserts on it.
	logger := slog.New(slog.DiscardHandler)

	// TLS because jwks.NewRemoteSource refuses a jwks_uri that is not https,
	// which is a rule worth exercising rather than working around: a key set
	// fetched in the clear is one an on-path attacker can replace.
	//
	// The issuer identifier has to be the URL the documents are served from,
	// and that URL is only known once the listener is bound — so the server
	// starts holding nothing and is given its handler afterwards.
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	server.StartTLS()

	issuer := &WorkloadIssuer{
		URL:     server.URL,
		server:  server,
		logger:  logger,
		config:  config,
		mu:      sync.Mutex{},
		signer:  nil,
		keyID:   "",
		stopped: false,
	}
	issuer.rotate(t)

	t.Cleanup(issuer.Stop)

	return issuer
}

// rotate generates a key, publishes it, and mints with it from here on.
func (w *WorkloadIssuer) rotate(t *testing.T) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "generate issuer key")

	provider, err := mockoidc.NewProvider(w.config, w.logger, w.URL, key)
	require.NoError(t, err, "build provider")

	// The provider derives its kid from the key it publishes, so the signer
	// has to take that value rather than invent one: an assertion naming a kid
	// absent from the served set is rejected as an unknown key, which is a
	// real rejection but not the one any of these tests mean.
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), provider.KeyID()),
	)
	require.NoError(t, err, "build signer")

	w.mu.Lock()
	defer w.mu.Unlock()
	w.signer = signer
	w.keyID = provider.KeyID()
	w.server.Config.Handler = mockoidc.NewServer(provider, w.logger).Handler()
}

// Rotate replaces the issuer's signing key and publishes the new one, exactly
// as an issuer rotating on its own schedule would. Assertions already minted
// name a kid the served key set no longer contains.
func (w *WorkloadIssuer) Rotate(t *testing.T) {
	t.Helper()
	w.rotate(t)
}

// Stop takes the issuer off the network. Its key set stops being reachable
// while assertions it minted stay perfectly valid, which is the shape of a
// real outage.
func (w *WorkloadIssuer) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	w.stopped = true
	w.server.Close()
}

// RootCAs is the pool that trusts this issuer's certificate. A caller builds
// its guardian policy with guardian.WithTLSRootCAs so the fetch reaches a
// server whose certificate nothing else has heard of.
func (w *WorkloadIssuer) RootCAs() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(w.server.Certificate())
	return pool
}

// KeySource resolves this issuer's published key set over the network, rather
// than handing the verifier a key set it never had to fetch.
//
// The jwks_uri is the one the discovery document advertises, so the path is
// the server's to choose and this cannot drift from what it serves.
func (w *WorkloadIssuer) KeySource(t *testing.T) jwks.Source {
	t.Helper()

	source, err := jwks.NewRemoteSource(w.URL + "/jwks.json")
	require.NoError(t, err, "build remote key source")
	return source
}

// Mint signs claims with the issuer's current key.
func (w *WorkloadIssuer) Mint(t *testing.T, claims jwt.Claims) string {
	t.Helper()

	w.mu.Lock()
	signer := w.signer
	w.mu.Unlock()

	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err, "mint assertion")
	return raw
}

// WorkloadClaims is an assertion this issuer vouching for externalSubject,
// addressed to audience. Every field satisfies the verifier, so a test can
// change exactly one and know what it is testing.
//
// iss is the issuer and sub is the workload — unlike a client assertion,
// where RFC 7523 §3 requires both to be the client_id.
func (w *WorkloadIssuer) WorkloadClaims(externalSubject, audience string) jwt.Claims {
	now := time.Now()
	return jwt.Claims{
		Issuer:    w.URL,
		Subject:   externalSubject,
		Audience:  jwt.Audience{audience},
		Expiry:    jwt.NewNumericDate(now.Add(2 * time.Minute)),
		NotBefore: jwt.NewNumericDate(now),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        "jti-" + uuid.NewString(),
	}
}

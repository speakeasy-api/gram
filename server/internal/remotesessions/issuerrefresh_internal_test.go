package remotesessions

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

func TestRefreshIssuerMetadataRequiresConfiguredTunnelTransport(t *testing.T) {
	t.Parallel()

	issuer := repo.RemoteSessionIssuer{
		Issuer:              "https://idp.example.com",
		TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))

	_, _, err := refreshIssuerMetadata(t.Context(), policy, nil, nil, issuer)
	require.ErrorContains(t, err, "select issuer discovery transport: tunnel transport is not configured")
}

// testJWKSet serves a one-key JWK Set over TLS and counts the fetches it saw.
func testJWKSet(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       key.Public(),
		KeyID:     "kid-1",
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}}})
	require.NoError(t, err)

	var fetches atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/jwk-set+json")
		_, _ = w.Write(document)
	}))
	t.Cleanup(server.Close)
	return server, &fetches
}

// countingDoer is a transport that reaches a key set the resolver's own client
// cannot, standing in for a tunnel into a customer network.
type countingDoer struct {
	inner *http.Client
	calls int
}

func (d *countingDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls++
	resp, err := d.inner.Do(req)
	if err != nil {
		return nil, fmt.Errorf("counting doer: %w", err)
	}
	return resp, nil
}

func TestRefreshIssuerKeySetUsesTheIssuerTunnel(t *testing.T) {
	t.Parallel()

	server, fetches := testJWKSet(t)
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	resolver := jwks.NewResolver(policy, testenv.NewMeterProvider(t), testenv.NewLogger(t))
	doer := &countingDoer{inner: server.Client(), calls: 0}

	keySet, err := refreshIssuerKeySet(t.Context(), resolver, doer, server.URL, repo.RemoteSessionIssuer{Issuer: server.URL})
	require.NoError(t, err)
	require.NotEmpty(t, keySet.document)
	require.Equal(t, 1, doer.calls, "a bound issuer's key set must be read over its tunnel")
	require.Equal(t, int64(1), fetches.Load())
}

// The counterpart: without a tunnel the fetch takes the resolver's own
// direct-egress client, which is what fails for a private issuer today.
func TestRefreshIssuerKeySetWithoutTunnelUsesDirectEgress(t *testing.T) {
	t.Parallel()

	server, fetches := testJWKSet(t)
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	resolver := jwks.NewResolver(policy, testenv.NewMeterProvider(t), testenv.NewLogger(t))

	_, err := refreshIssuerKeySet(t.Context(), resolver, nil, server.URL, repo.RemoteSessionIssuer{Issuer: server.URL})
	require.Error(t, err)
	require.Zero(t, fetches.Load())
}

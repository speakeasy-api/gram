package oauthtest

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
)

// A dev-idp stands in for the platform that vouches for a workload: its OAuth
// 2.1 issuer, OAuth21URL, is the issuer identifier, and it serves a real
// discovery document and key set over HTTP. Launch it with LaunchOpts.TLS,
// because jwks.NewRemoteSource refuses a jwks_uri that is not https, which is
// a rule worth exercising rather than working around.
//
// dev-idp mints id_tokens for its own users and clients, whose sub and aud do
// not describe a workload addressed to Gram. So assertions are signed here
// with the key the dev-idp publishes, and verification reaches the dev-idp the
// way it reaches a customer's issuer: discovery, a fetch, a cache. What an
// in-process key source cannot express becomes real as well: the dev-idp can
// rotate its key, go offline, and count what reached it.

// DiscoverWorkloadJWKSURI reads the dev-idp's OpenID discovery document and
// returns the jwks_uri it advertises, so a key source is built from what the
// issuer publishes rather than a path the test assumes.
//
// This is a request to the dev-idp. A test counting requests takes its
// baseline after calling it.
func DiscoverWorkloadJWKSURI(t *testing.T, issuer *devidptest.Instance) string {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, issuer.OAuth21URL+"/.well-known/openid-configuration", nil)
	require.NoError(t, err, "build discovery request")

	resp, err := issuer.Client().Do(req)
	require.NoError(t, err, "fetch discovery document")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "discovery status")

	var discovery struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&discovery), "decode discovery document")
	require.Equal(t, issuer.OAuth21URL, discovery.Issuer, "discovery must name the issuer it is served from")
	require.NotEmpty(t, discovery.JWKSURI, "discovery must advertise a jwks_uri")

	return discovery.JWKSURI
}

// MintWorkloadAssertion signs claims with the dev-idp's current key, naming
// its current kid. After RotateKey it signs with the new key, and anything
// minted before names a kid the published set no longer contains.
func MintWorkloadAssertion(t *testing.T, issuer *devidptest.Instance, claims jwt.Claims) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: issuer.SigningKey()},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), issuer.KeyID()),
	)
	require.NoError(t, err, "build signer")

	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err, "mint assertion")
	return raw
}

// WorkloadClaims is an assertion the dev-idp vouching for externalSubject,
// addressed to audience. Every field satisfies the verifier, so a test can
// change exactly one and know what it is testing.
//
// iss is the issuer and sub is the workload, unlike a client assertion, where
// RFC 7523 §3 requires both to be the client_id.
func WorkloadClaims(issuer *devidptest.Instance, externalSubject, audience string) jwt.Claims {
	now := time.Now()
	return jwt.Claims{
		Issuer:    issuer.OAuth21URL,
		Subject:   externalSubject,
		Audience:  jwt.Audience{audience},
		Expiry:    jwt.NewNumericDate(now.Add(2 * time.Minute)),
		NotBefore: jwt.NewNumericDate(now),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        "jti-" + uuid.NewString(),
	}
}

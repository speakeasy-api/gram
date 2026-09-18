package remotesessions

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

func TestFederatedSignatureVerification(t *testing.T) {
	t.Parallel()
	key, keys, _ := newRSAKeyPolicyFixture(t, 2048)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	p := federatedFixture(t)
	p.metadata.JwksURI = rsaKeyPolicyJWKSURI
	m := &ChallengeManager{idTokens: NewIDTokenVerifier(keys)}
	for _, test := range []struct {
		name       string
		signingKey any
		alg        jose.SignatureAlgorithm
		headers    map[jose.HeaderKey]any
		claim      string
		value      json.RawMessage
		valid      bool
	}{
		{name: "valid", signingKey: key, alg: jose.RS256, valid: true},
		{name: "bad signature", signingKey: other, alg: jose.RS256},
		{name: "symmetric signing", signingKey: []byte("01234567890123456789012345678901"), alg: jose.HS256},
		{name: "key URL injection", signingKey: key, alg: jose.RS256, headers: map[jose.HeaderKey]any{"jku": "https://attacker.example.test/keys"}},
		{name: "certificate URL injection", signingKey: key, alg: jose.RS256, headers: map[jose.HeaderKey]any{"x5u": "https://attacker.example.test/cert"}},
		{name: "embedded key injection", signingKey: key, alg: jose.RS256, headers: map[jose.HeaderKey]any{"jwk": jose.JSONWebKey{Key: &key.PublicKey}}},
		{name: "exact issuer", signingKey: key, alg: jose.RS256, claim: "iss", value: json.RawMessage(`"https://idp.example.test/tenant/"`)},
		{name: "wrong nonce", signingKey: key, alg: jose.RS256, claim: "nonce", value: json.RawMessage(`"other"`)},
		{name: "missing issued at", signingKey: key, alg: jose.RS256, claim: "iat"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := (&jose.SignerOptions{}).WithHeader("kid", "example-key")
			for name, value := range test.headers {
				options = options.WithHeader(name, value)
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: test.alg, Key: test.signingKey}, options)
			require.NoError(t, err)
			claims := federatedClaims(t, p, time.Now())
			if test.claim != "" {
				if test.value == nil {
					delete(claims, test.claim)
				} else {
					claims[test.claim] = test.value
				}
			}
			payload, err := json.Marshal(claims)
			require.NoError(t, err)
			signed, err := signer.Sign(payload)
			require.NoError(t, err)
			raw, err := signed.CompactSerialize()
			require.NoError(t, err)
			identity, err := m.verifyFederatedIdentity(t.Context(), p, tokenResponse{IDToken: raw}, "code", "nonce", nil)
			if test.valid {
				require.NoError(t, err)
				require.Equal(t, "foreign-subject", identity.Subject)
			} else {
				require.ErrorIs(t, err, ErrFederatedIdentity)
				require.Nil(t, identity)
			}
		})
	}
}

func TestFederatedCredentialsHandoff(t *testing.T) {
	t.Parallel()
	credentials := EphemeralFederatedCredentials{idToken: "secret-id-token", refreshToken: "secret-refresh-token", expiresIn: 3600}
	identity := &FederatedIdentity{Subject: "subject", credentials: &credentials}
	for _, value := range []any{credentials, &credentials, identity, *identity} {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(data), "secret-")
		require.NotContains(t, fmt.Sprintf("%v %+v %#v", value, value, value), "secret-")
	}
	consumerError := errors.New("consumer failure")
	called := false
	err := identity.WithCredentials(func(c EphemeralFederatedCredentials) error {
		called = true
		require.Equal(t, "secret-id-token", c.IDToken())
		require.Equal(t, "secret-refresh-token", c.RefreshToken())
		require.Equal(t, 3600, c.ExpiresIn())
		return consumerError
	})
	require.True(t, called)
	require.ErrorIs(t, err, consumerError)
	require.Empty(t, credentials.IDToken())
	require.Nil(t, identity.credentials)
	require.Error(t, identity.WithCredentials(func(EphemeralFederatedCredentials) error { t.Fatal("replayed credential handoff"); return nil }))
	identity.DiscardCredentials()
}

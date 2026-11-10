package remotesessions

import (
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSignedTokensRequireExactIssuer(t *testing.T) {
	t.Parallel()
	key, keys, policy := newRSAKeyPolicyFixture(t, 2048)
	verifier := NewIDTokenVerifier(keys)
	enricher := NewSessionEnricher(testenv.NewLogger(t), nil, policy, keys, nil, nil, nil)
	for _, tc := range []struct{ name, expected, advertised string }{
		{"exact slash", "/tenant/", "/tenant/"},
		{"exact no slash", "/tenant", "/tenant"},
		{"added slash", "/tenant", "/tenant/"},
		{"missing slash", "/tenant/", "/tenant"},
		{"whitespace", "/tenant/", "/tenant/ "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expected, advertised := rsaKeyPolicyIssuer+tc.expected, rsaKeyPolicyIssuer+tc.advertised
			now := time.Now()
			claims := map[string]any{
				"iss": advertised, "sub": "example-subject", "aud": rsaKeyPolicyClientID,
				"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(),
				"token_introspection": map[string]any{"active": true, "sub": "example-subject"},
			}
			for _, typ := range []jose.ContentType{"JWT", "token-introspection+jwt"} {
				signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType(typ).WithHeader("kid", "example-key"))
				require.NoError(t, err)
				raw, err := jwt.Signed(signer).Claims(claims).Serialize()
				require.NoError(t, err)
				if typ == "JWT" {
					identity, err := verifier.Verify(t.Context(), raw, IDTokenExpectation{
						issuer: expected, clientID: rsaKeyPolicyClientID, jwksURI: rsaKeyPolicyJWKSURI,
						signingAlgs: []string{"RS256"},
					})
					if expected == advertised {
						require.NoError(t, err)
						require.Equal(t, "example-subject", identity.Subject)
					} else {
						require.ErrorContains(t, err, "is not the grant's issuer")
						require.Empty(t, identity.Subject)
					}
				} else {
					members, err := enricher.decodeIntrospection(t.Context(), enrichmentTarget{
						issuerID: uuid.New(), issuerURL: expected, jwksURI: rsaKeyPolicyJWKSURI, externalClientID: rsaKeyPolicyClientID,
					}, interfaceAnswer{mediaType: introspectionJWTMediaType, body: []byte(raw)})
					if expected == advertised {
						require.NoError(t, err)
						require.JSONEq(t, `true`, string(members["active"]))
					} else {
						require.ErrorContains(t, err, "is not the grant's issuer")
						require.Nil(t, members)
					}
				}
			}
		})
	}
}

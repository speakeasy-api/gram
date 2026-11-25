package remotesessions_test

import (
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestJWTAccessTokenRejectsUnencodedPayload(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client"}
	for _, encoded := range []bool{true, false} {
		name := "encoded"
		if !encoded {
			name = "unencoded"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			opts := (&jose.SignerOptions{}).WithType("at+jwt").WithHeader("kid", "synthetic-kid").WithBase64(encoded)
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: issuer.key}, opts)
			require.NoError(t, err)
			claims := accessTokenClaims(issuer.issuerURL, "oauth-client")
			if !encoded {
				// Dot-free payload so the compact form passes the segment gate and reaches the b64 header check.
				claims = map[string]any{"iss": "https://issuer-local", "sub": "user-123", "aud": "oauth-client", "exp": time.Now().Add(5 * time.Minute).Unix(), "iat": time.Now().Unix(), "client_id": "oauth-client", "jti": "jti-1"}
			}
			raw, err := jwt.Signed(signer).Claims(claims).Serialize()
			require.Equal(t, 2, strings.Count(raw, "."))
			require.NoError(t, err)
			// RFC 7797 §7: a valid JWS using b64=false is not a valid JWT.
			result := enricher.JWTAccessToken(t.Context(), target, raw)
			if encoded {
				require.Equal(t, "ok", result.Status)
			} else {
				require.Equal(t, "failed", result.Status)
				require.Equal(t, "invalid token type", result.Reason)
				require.Empty(t, result.Subject)
			}
		})
	}
}

// Header hygiene the enricher depends on: an explicit b64=true is fine and
// go-jose refuses unknown critical extensions.
func TestJWTAccessTokenHeaderExtensions(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client"}
	mint := func(extra map[jose.HeaderKey]any) string {
		opts := (&jose.SignerOptions{}).WithType("at+jwt").WithHeader("kid", "synthetic-kid")
		for k, v := range extra {
			opts = opts.WithHeader(k, v)
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: issuer.key}, opts)
		require.NoError(t, err)
		raw, err := jwt.Signed(signer).Claims(accessTokenClaims(issuer.issuerURL, "oauth-client")).Serialize()
		require.NoError(t, err)
		return raw
	}

	result := enricher.JWTAccessToken(t.Context(), target, mint(map[jose.HeaderKey]any{"b64": true}))
	require.Equal(t, "ok", result.Status, "an explicit b64=true is the RFC 7797 default")

	result = enricher.JWTAccessToken(t.Context(), target, mint(map[jose.HeaderKey]any{"crit": []string{"gram-ext"}, "gram-ext": 1}))
	require.Equal(t, "failed", result.Status)
	require.Equal(t, "unverifiable token", result.Reason, "RFC 7515 §4.1.11: an unknown critical extension invalidates the JWS")
}

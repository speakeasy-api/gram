package remotesessions

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

const testKMSResourceName = "projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/1"

func TestSerializeClientAssertion_HeadersAndClaims(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client, err := gcpkms.NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)
	public, err := client.GetPublicKey(ctx, testKMSResourceName)
	require.NoError(t, err)
	publicJWK, err := json.Marshal(jose.JSONWebKey{
		Key:                         public.Key,
		KeyID:                       "published-kid",
		Algorithm:                   string(public.Algorithm),
		Use:                         "sig",
		Certificates:                nil,
		CertificatesURL:             nil,
		CertificateThumbprintSHA1:   nil,
		CertificateThumbprintSHA256: nil,
	})
	require.NoError(t, err)
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)

	raw, err := serializeClientAssertion(ctx, client, testKMSResourceName, "published-kid", publicJWK, "client-123", "https://issuer.example.com/", now)
	require.NoError(t, err)

	parsed, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	require.NoError(t, err)
	require.Equal(t, "published-kid", parsed.Headers[0].KeyID)
	require.Equal(t, "client-authentication+jwt", parsed.Headers[0].ExtraHeaders[jose.HeaderKey("typ")])

	var claims jwt.Claims
	require.NoError(t, parsed.Claims(public.Key, &claims))
	require.Equal(t, "client-123", claims.Issuer)
	require.Equal(t, "client-123", claims.Subject)
	require.Equal(t, jwt.Audience{"https://issuer.example.com/"}, claims.Audience)
	require.Equal(t, now.Unix(), claims.IssuedAt.Time().Unix())
	require.Equal(t, now.Add(clientAssertionLifetime).Unix(), claims.Expiry.Time().Unix())
	require.Nil(t, claims.NotBefore)
	require.NotEmpty(t, claims.ID)

	parts := strings.Split(raw, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var rawClaims map[string]any
	require.NoError(t, json.Unmarshal(payload, &rawClaims))
	require.Equal(t, "https://issuer.example.com/", rawClaims["aud"], "a single audience must serialize as a JSON string")
	require.NotContains(t, rawClaims, "nbf")

	second, err := serializeClientAssertion(ctx, client, testKMSResourceName, "published-kid", publicJWK, "client-123", "https://issuer.example.com/", now)
	require.NoError(t, err)
	secondParsed, err := jwt.ParseSigned(second, []jose.SignatureAlgorithm{jose.RS256})
	require.NoError(t, err)
	var secondClaims jwt.Claims
	require.NoError(t, secondParsed.Claims(public.Key, &secondClaims))
	require.NotEqual(t, claims.ID, secondClaims.ID, "each assertion must carry a fresh jti")
}

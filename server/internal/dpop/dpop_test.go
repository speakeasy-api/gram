package dpop

import (
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type proofClaims struct {
	JTI   string `json:"jti"`
	HTM   string `json:"htm"`
	HTU   string `json:"htu"`
	IAT   int64  `json:"iat"`
	ATH   string `json:"ath"`
	Nonce string `json:"nonce"`
}

func parseProof(t *testing.T, raw string) (jose.Header, proofClaims) {
	t.Helper()
	parsed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	header := parsed.Signatures[0].Header
	require.NotNil(t, header.JSONWebKey)
	payload, err := parsed.Verify(header.JSONWebKey)
	require.NoError(t, err)
	var claims proofClaims
	require.NoError(t, json.Unmarshal(payload, &claims))
	return header, claims
}

func TestHTU_StripsQueryAndFragment(t *testing.T) {
	t.Parallel()
	target, err := url.Parse("https://example.okta.com/api/v1/apps?limit=20&after=x#frag")
	require.NoError(t, err)
	require.Equal(t, "https://example.okta.com/api/v1/apps", HTU(target))
	require.Equal(t, "limit=20&after=x", target.RawQuery, "input must not be mutated")
}

func TestAccessTokenHash_IsBase64URLSHA256(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte("tok"))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), AccessTokenHash("tok"))
}

func TestKey_ProofCarriesRequiredClaimsAndEmbeddedJWK(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)
	target, err := url.Parse("https://example.okta.com/oauth2/v1/token?x=1")
	require.NoError(t, err)
	now := time.Unix(1_700_000_000, 0)

	raw, err := key.Proof(http.MethodPost, target, ProofOptions{AccessToken: "", Nonce: "", IssuedAt: now})
	require.NoError(t, err)

	header, claims := parseProof(t, raw)
	require.Equal(t, ProofType, header.ExtraHeaders[jose.HeaderType])
	require.Equal(t, string(jose.ES256), header.Algorithm)
	thumb, err := header.JSONWebKey.Thumbprint(crypto.SHA256)
	require.NoError(t, err)
	require.Equal(t, key.Thumbprint(), base64.RawURLEncoding.EncodeToString(thumb))

	require.Equal(t, http.MethodPost, claims.HTM)
	require.Equal(t, "https://example.okta.com/oauth2/v1/token", claims.HTU)
	require.Equal(t, now.Unix(), claims.IAT)
	_, err = uuid.Parse(claims.JTI)
	require.NoError(t, err)
	require.Empty(t, claims.ATH)
	require.Empty(t, claims.Nonce)
}

func TestKey_ProofBindsNonceAndAccessToken(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)
	target, err := url.Parse("https://example.okta.com/api/v1/apps")
	require.NoError(t, err)

	raw, err := key.Proof(http.MethodGet, target, ProofOptions{AccessToken: "tok", Nonce: "n1", IssuedAt: time.Unix(1, 0)})
	require.NoError(t, err)

	_, claims := parseProof(t, raw)
	require.Equal(t, "n1", claims.Nonce)
	require.Equal(t, AccessTokenHash("tok"), claims.ATH)
}

func TestKey_ProofsHaveUniqueJTI(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)
	target, err := url.Parse("https://example.okta.com/api/v1/apps")
	require.NoError(t, err)

	first, err := key.Proof(http.MethodGet, target, ProofOptions{AccessToken: "", Nonce: "", IssuedAt: time.Unix(1, 0)})
	require.NoError(t, err)
	second, err := key.Proof(http.MethodGet, target, ProofOptions{AccessToken: "", Nonce: "", IssuedAt: time.Unix(1, 0)})
	require.NoError(t, err)

	_, a := parseProof(t, first)
	_, b := parseProof(t, second)
	require.NotEqual(t, a.JTI, b.JTI)
}

func TestNewKey_ThumbprintDiffersPerInstance(t *testing.T) {
	t.Parallel()
	a, err := NewKey()
	require.NoError(t, err)
	b, err := NewKey()
	require.NoError(t, err)
	require.NotEqual(t, a.Thumbprint(), b.Thumbprint())
}

func nonceHeader(nonce string, extra map[string]string) http.Header {
	h := http.Header{}
	if nonce != "" {
		h.Set(NonceHeaderName, nonce)
	}
	for k, v := range extra {
		h.Set(k, v)
	}
	return h
}

func TestNonceCache_RemembersLatestNonEmpty(t *testing.T) {
	t.Parallel()
	var cache NonceCache
	require.Empty(t, cache.Current())

	cache.Remember(nonceHeader("n1", nil))
	require.Equal(t, "n1", cache.Current())

	cache.Remember(nonceHeader("", nil))
	require.Equal(t, "n1", cache.Current())

	cache.Remember(nonceHeader("n2", nil))
	require.Equal(t, "n2", cache.Current())
}

func TestIsUseNonceChallenge_RequiresNonceHeader(t *testing.T) {
	t.Parallel()
	require.False(t, IsUseNonceChallenge(nonceHeader("", map[string]string{"WWW-Authenticate": `DPoP error="use_dpop_nonce"`}), nil))
	require.False(t, IsUseNonceChallenge(nonceHeader("", nil), []byte(`{"error":"use_dpop_nonce"}`)))
}

func TestIsUseNonceChallenge_MatchesHeaderOrBody(t *testing.T) {
	t.Parallel()
	require.True(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `DPoP error="use_dpop_nonce"`}), nil))
	require.True(t, IsUseNonceChallenge(nonceHeader("n1", nil), []byte(`{"error":"use_dpop_nonce"}`)))
	require.False(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `DPoP error="invalid_token"`}), []byte(`{"error":"invalid_token"}`)))
	require.False(t, IsUseNonceChallenge(nonceHeader("n1", nil), []byte(`not json`)))
}

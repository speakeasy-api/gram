package dpop

import (
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
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

// RFC 9449 §4.2, §4.3: htu is emitted in RFC 3986 canonical form.
func TestHTU_EmitsCanonicalForm(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"HTTPS://Example.OKTA.com:443/api/v1/apps": "https://example.okta.com/api/v1/apps",
		"http://localhost:80/token":                "http://localhost/token",
		"http://127.0.0.1:8080/token":              "http://127.0.0.1:8080/token",
		"https://example.okta.com":                 "https://example.okta.com/",
		"https://example.okta.com?x=1#f":           "https://example.okta.com/",
		"https://user:pw@example.okta.com/token":   "https://example.okta.com/token",
		"https://example.okta.com/a%2Fb/c":         "https://example.okta.com/a%2Fb/c",
		"https://example.okta.com:0443/token":      "https://example.okta.com/token",
		"http://[::1]:080/token":                   "http://[::1]/token",
		"http://[::1]:08080/token":                 "http://[::1]:8080/token",
	}
	for raw, want := range cases {
		target, err := url.Parse(raw)
		require.NoError(t, err)
		require.Equal(t, want, HTU(target), raw)
	}
}

func TestProof_RejectsNilTarget(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)

	_, err = key.Proof(http.MethodGet, nil, ProofOptions{AccessToken: "", Nonce: "", IssuedAt: time.Time{}})
	require.EqualError(t, err, "dpop proof: target url is required")
	require.Empty(t, HTU(nil))
}

// RFC 9449 §7.1 Figure 14: ath for the RFC's example access token.
func TestAccessTokenHash_MatchesRFCExample(t *testing.T) {
	t.Parallel()
	require.Equal(t, "fUHyO2r2Z3DZ53EsNrWBb0xWXoaNy59IiKCAqksmQEo", AccessTokenHash("Kz~8mXK1EalYznwH-LC-1fBAo.4Ljp~zsPE_NeO.gxU"))
}

// RFC 9449 §4.2: the jwk header carries only the public key.
func TestKey_ProofJWKOmitsPrivateKey(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)
	target, err := url.Parse("https://example.okta.com/oauth2/v1/token")
	require.NoError(t, err)

	raw, err := key.Proof(http.MethodPost, target, ProofOptions{AccessToken: "", Nonce: "", IssuedAt: time.Unix(1, 0)})
	require.NoError(t, err)

	headerJSON, err := base64.RawURLEncoding.DecodeString(strings.Split(raw, ".")[0])
	require.NoError(t, err)
	var header struct {
		Typ string         `json:"typ"`
		Alg string         `json:"alg"`
		JWK map[string]any `json:"jwk"`
	}
	require.NoError(t, json.Unmarshal(headerJSON, &header))
	require.Equal(t, ProofType, header.Typ)
	require.Equal(t, "ES256", header.Alg)
	require.Equal(t, "EC", header.JWK["kty"])
	require.Equal(t, "P-256", header.JWK["crv"])
	require.NotContains(t, header.JWK, "d")
}

// RFC 9449 §4.2: iat is the creation time, so a zero IssuedAt means now.
func TestKey_ProofZeroIssuedAtUsesNow(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)
	target, err := url.Parse("https://example.okta.com/api/v1/apps")
	require.NoError(t, err)

	before := time.Now().Unix()
	raw, err := key.Proof(http.MethodGet, target, ProofOptions{AccessToken: "", Nonce: "", IssuedAt: time.Time{}})
	require.NoError(t, err)
	_, claims := parseProof(t, raw)
	require.GreaterOrEqual(t, claims.IAT, before)
	require.LessOrEqual(t, claims.IAT, time.Now().Unix())
}

// RFC 9449 §4.2, §4.3: htm is the request method verbatim.
func TestKey_ProofHTMIsMethodVerbatim(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)
	target, err := url.Parse("https://example.okta.com/api/v1/apps")
	require.NoError(t, err)

	raw, err := key.Proof(http.MethodDelete, target, ProofOptions{AccessToken: "", Nonce: "", IssuedAt: time.Unix(1, 0)})
	require.NoError(t, err)
	_, claims := parseProof(t, raw)
	require.Equal(t, "DELETE", claims.HTM)
}

// RFC 9449 §7.1, §9: the challenge is read from every WWW-Authenticate value
// and only from its error parameter.
func TestIsUseNonceChallenge_ParsesWWWAuthenticateParams(t *testing.T) {
	t.Parallel()
	multi := nonceHeader("n1", nil)
	multi.Add("WWW-Authenticate", `Bearer realm="okta"`)
	multi.Add("WWW-Authenticate", `DPoP algs="ES256 PS256", error="use_dpop_nonce", error_description="nonce required"`)
	require.True(t, IsUseNonceChallenge(multi, nil))

	require.True(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `DPoP error=use_dpop_nonce`}), nil))
	require.False(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `DPoP error="invalid_token", error_description="use_dpop_nonce"`}), nil))
	require.False(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `DPoP algs="ES256"`}), nil))
}

// RFC 9449 §7.1, §9: only the error parameter of a DPoP challenge counts.
func TestIsUseNonceChallenge_ReadsOnlyDPoPScheme(t *testing.T) {
	t.Parallel()
	require.False(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `Bearer error="use_dpop_nonce"`}), nil))
	require.True(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `Bearer error="invalid_token", DPoP error="use_dpop_nonce"`}), nil))
	require.True(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `DPoP error="use_dpop_nonce", Bearer error="invalid_token"`}), nil))
	require.False(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `DPoP realm="okta", Bearer error="use_dpop_nonce"`}), nil))
	require.True(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `dpop ERROR="use_dpop_nonce"`}), nil))
	require.True(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `Bearer, DPoP error="use_dpop_nonce"`}), nil))
}

// RFC 9110 §11.2: OWS is allowed around "=" and commas may sit inside quoted-strings.
func TestIsUseNonceChallenge_ToleratesOWSAndQuotedCommas(t *testing.T) {
	t.Parallel()
	require.True(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `DPoP error = "use_dpop_nonce"`}), nil))
	require.True(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": "DPoP\terror\t=\t\"use_dpop_nonce\""}), nil))
	require.True(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `Bearer error_description="a, b, DPoP error=\"x\"", DPoP error_description="nonce, please", error="use_dpop_nonce"`}), nil))
	require.False(t, IsUseNonceChallenge(nonceHeader("n1", map[string]string{"WWW-Authenticate": `Bearer realm="DPoP error=\"use_dpop_nonce\""`}), nil))
}

// RFC 9110 §5.3: multiple WWW-Authenticate fields are one list, so every field is scanned.
func TestIsUseNonceChallenge_ScansEveryHeaderField(t *testing.T) {
	t.Parallel()
	h := nonceHeader("n1", nil)
	h.Add("WWW-Authenticate", `Bearer error="invalid_token"`)
	h.Add("WWW-Authenticate", `Basic realm="x"`)
	h.Add("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
	require.True(t, IsUseNonceChallenge(h, nil))

	h = nonceHeader("n1", nil)
	h.Add("WWW-Authenticate", `Bearer error="use_dpop_nonce"`)
	h.Add("WWW-Authenticate", `DPoP error="invalid_dpop_proof"`)
	require.False(t, IsUseNonceChallenge(h, nil))
}

func TestChallengeError_UnquotesValue(t *testing.T) {
	t.Parallel()
	require.Equal(t, `say "hi", ok`, challengeError(`DPoP error="say \"hi\", ok"`))
	require.Equal(t, "use_dpop_nonce", challengeError(`DPoP algs="ES256", error=use_dpop_nonce`))
	require.Empty(t, challengeError(`DPoP`))
	require.Empty(t, challengeError(`Basic dXNlcjpwYXNz==`))
	require.Empty(t, challengeError(``))
}

// proofWire decodes the raw JWS segments of a proof without a JWT library so
// the test sees exactly what goes on the wire.
func proofWire(t *testing.T, raw string) (header, jwk, payload map[string]json.RawMessage) {
	t.Helper()
	// RFC 7515 §7.1: compact serialization is three unpadded base64url segments.
	parts := strings.Split(raw, ".")
	require.Len(t, parts, 3)
	require.NotContains(t, raw, "=")
	require.NotEmpty(t, parts[2], "signature segment must be present")
	decode := func(segment string) map[string]json.RawMessage {
		b, err := base64.RawURLEncoding.DecodeString(segment)
		require.NoError(t, err)
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &m))
		return m
	}
	header = decode(parts[0])
	require.NoError(t, json.Unmarshal(header["jwk"], &jwk))
	return header, jwk, decode(parts[1])
}

func wireKeys(m map[string]json.RawMessage) []string {
	return slices.Sorted(maps.Keys(m))
}

// RFC 9449 §4.2: a bare proof carries exactly jti, htm, htu, and iat; nonce
// and ath appear only when a nonce or access token is supplied.
func TestKey_ProofWireShape_BareClaimsOnly(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)
	target, err := url.Parse("https://Example.okta.com:443/api/v1/apps?limit=1#f")
	require.NoError(t, err)

	raw, err := key.Proof(http.MethodGet, target, ProofOptions{AccessToken: "", Nonce: "", IssuedAt: time.Unix(1_700_000_000, 0)})
	require.NoError(t, err)

	header, jwk, payload := proofWire(t, raw)
	require.Equal(t, []string{"alg", "jwk", "typ"}, wireKeys(header))
	require.JSONEq(t, `"dpop+jwt"`, string(header["typ"]))
	require.JSONEq(t, `"ES256"`, string(header["alg"]))
	require.Equal(t, []string{"crv", "kty", "x", "y"}, wireKeys(jwk))
	require.JSONEq(t, `"EC"`, string(jwk["kty"]))
	require.JSONEq(t, `"P-256"`, string(jwk["crv"]))

	require.Equal(t, []string{"htm", "htu", "iat", "jti"}, wireKeys(payload))
	require.JSONEq(t, `"GET"`, string(payload["htm"]))
	require.JSONEq(t, `"https://example.okta.com/api/v1/apps"`, string(payload["htu"]))
	require.Equal(t, "1700000000", string(payload["iat"]), "iat is a bare JSON integer")
	var jti string
	require.NoError(t, json.Unmarshal(payload["jti"], &jti))
	id, err := uuid.Parse(jti)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), id.Version())
}

// RFC 9449 §4.2, §7: ath is added for an access token and nothing else changes.
func TestKey_ProofWireShape_AddsATHWithAccessToken(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)
	target, err := url.Parse("https://example.okta.com/api/v1/apps")
	require.NoError(t, err)

	raw, err := key.Proof(http.MethodGet, target, ProofOptions{AccessToken: "Kz~8mXK1EalYznwH-LC-1fBAo.4Ljp~zsPE_NeO.gxU", Nonce: "", IssuedAt: time.Unix(1, 0)})
	require.NoError(t, err)

	header, jwk, payload := proofWire(t, raw)
	require.Equal(t, []string{"alg", "jwk", "typ"}, wireKeys(header))
	require.Equal(t, []string{"crv", "kty", "x", "y"}, wireKeys(jwk))
	require.Equal(t, []string{"ath", "htm", "htu", "iat", "jti"}, wireKeys(payload))
	require.JSONEq(t, `"fUHyO2r2Z3DZ53EsNrWBb0xWXoaNy59IiKCAqksmQEo"`, string(payload["ath"]))
}

// RFC 9449 §4.2, §8: nonce is added only when the server supplied one.
func TestKey_ProofWireShape_AddsNonceWhenSupplied(t *testing.T) {
	t.Parallel()
	key, err := NewKey()
	require.NoError(t, err)
	target, err := url.Parse("https://example.okta.com/oauth2/v1/token")
	require.NoError(t, err)

	raw, err := key.Proof(http.MethodPost, target, ProofOptions{AccessToken: "", Nonce: "n1", IssuedAt: time.Unix(1, 0)})
	require.NoError(t, err)

	header, jwk, payload := proofWire(t, raw)
	require.Equal(t, []string{"alg", "jwk", "typ"}, wireKeys(header))
	require.Equal(t, []string{"crv", "kty", "x", "y"}, wireKeys(jwk))
	require.Equal(t, []string{"htm", "htu", "iat", "jti", "nonce"}, wireKeys(payload))
	require.JSONEq(t, `"n1"`, string(payload["nonce"]))

	raw, err = key.Proof(http.MethodPost, target, ProofOptions{AccessToken: "tok", Nonce: "n1", IssuedAt: time.Unix(1, 0)})
	require.NoError(t, err)
	_, _, payload = proofWire(t, raw)
	require.Equal(t, []string{"ath", "htm", "htu", "iat", "jti", "nonce"}, wireKeys(payload))
}

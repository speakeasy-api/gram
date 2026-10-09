package agent

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/identity"
	"github.com/speakeasy-api/gram/tunnel/jwks"
)

func newTestVerifier(t *testing.T, s *testSigner, clock *testClock) *assertionVerifier {
	t.Helper()
	return newAssertionVerifier(s.config(), jwksHTTPClient(), clock.Now)
}

func assertionRequestHeader(raw string) http.Header {
	h := http.Header{}
	h.Set(identity.Header, raw)
	return h
}

func TestVerifierAcceptsValidAssertion(t *testing.T) {
	t.Parallel()
	s := newTestSigner(t)
	clock := newTestClock()
	v := newTestVerifier(t, s, clock)

	raw := s.sign(t, s.claims(clock.Now(), defaultTestPrincipal, credentialClaim(defaultTestGrant, testTokenA, time.Time{})))
	got, err := v.verify(t.Context(), assertionRequestHeader(raw))
	require.NoError(t, err)
	require.Equal(t, principal{issuer: testIssuer, subject: "user:alice", organizationID: testOrg, mcpServerID: defaultTestPrincipal.mcpServerID, consent: false}, got.principal)
	require.NotEmpty(t, got.credential)
	require.True(t, got.permits("tools/call"))
}

func TestVerifierRejectsInvalidClaims(t *testing.T) {
	t.Parallel()
	s := newTestSigner(t)
	clock := newTestClock()
	v := newTestVerifier(t, s, clock)
	now := clock.Now()

	cases := []struct {
		name   string
		mutate func(jwt.MapClaims)
	}{
		{"wrong issuer", func(c jwt.MapClaims) { c["iss"] = "https://evil.example.test" }},
		{"wrong audience", func(c jwt.MapClaims) { c["aud"] = "tunneled-mcp-server:other" }},
		{"two audiences", func(c jwt.MapClaims) { c["aud"] = []string{testAudience, "other"} }},
		{"wrong organization with same audience", func(c jwt.MapClaims) { c["organization_id"] = "org_other" }},
		{"missing organization", func(c jwt.MapClaims) { delete(c, "organization_id") }},
		{"version 2", func(c jwt.MapClaims) { c["version"] = 2 }},
		{"version string", func(c jwt.MapClaims) { c["version"] = "1" }},
		{"missing version", func(c jwt.MapClaims) { delete(c, "version") }},
		{"expired", func(c jwt.MapClaims) {
			c["iat"] = now.Add(-2 * time.Minute).Unix()
			c["exp"] = now.Add(-time.Minute).Unix()
		}},
		{"issued in the future", func(c jwt.MapClaims) {
			c["iat"] = now.Add(time.Minute).Unix()
			c["exp"] = now.Add(2 * time.Minute).Unix()
		}},
		{"missing iat", func(c jwt.MapClaims) { delete(c, "iat") }},
		{"missing exp", func(c jwt.MapClaims) { delete(c, "exp") }},
		{"not yet valid", func(c jwt.MapClaims) { c["nbf"] = now.Add(time.Minute).Unix() }},
		{"overlong lifetime", func(c jwt.MapClaims) { c["exp"] = now.Add(2 * time.Minute).Unix() }},
		{"zero lifetime", func(c jwt.MapClaims) { c["exp"] = c["iat"] }},
		{"untyped subject", func(c jwt.MapClaims) { c["sub"] = "alice" }},
		{"missing mcp server", func(c jwt.MapClaims) { delete(c, "mcp_server_id") }},
		{"nil mcp server", func(c jwt.MapClaims) { c["mcp_server_id"] = "00000000-0000-0000-0000-000000000000" }},
		{"noncanonical mcp server", func(c jwt.MapClaims) { c["mcp_server_id"] = strings.ToUpper(defaultTestPrincipal.mcpServerID) }},
		{"null allowed methods", func(c jwt.MapClaims) { c["allowed_methods"] = nil }},
		{"allowed methods wrong type", func(c jwt.MapClaims) { c["allowed_methods"] = "tools/list" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			claims := s.claims(now, defaultTestPrincipal, nil)
			tc.mutate(claims)
			_, err := v.verify(t.Context(), assertionRequestHeader(s.sign(t, claims)))
			require.ErrorIs(t, err, errAssertionInvalid)
		})
	}
}

func TestVerifierAcceptsSkewWithinTolerance(t *testing.T) {
	t.Parallel()
	s := newTestSigner(t)
	clock := newTestClock()
	v := newTestVerifier(t, s, clock)
	claims := s.claims(clock.Now().Add(3*time.Second), defaultTestPrincipal, nil)
	_, err := v.verify(t.Context(), assertionRequestHeader(s.sign(t, claims)))
	require.NoError(t, err)
}

func TestVerifierEmptyAllowedMethodsAdmitsNothing(t *testing.T) {
	t.Parallel()
	s := newTestSigner(t)
	clock := newTestClock()
	v := newTestVerifier(t, s, clock)
	claims := s.claims(clock.Now(), defaultTestPrincipal, nil)
	claims["allowed_methods"] = []string{}
	got, err := v.verify(t.Context(), assertionRequestHeader(s.sign(t, claims)))
	require.NoError(t, err)
	require.True(t, got.principal.consent)
	require.False(t, got.permits("initialize"))
}

func TestVerifierRejectsBadHeaders(t *testing.T) {
	t.Parallel()
	s := newTestSigner(t)
	clock := newTestClock()
	v := newTestVerifier(t, s, clock)
	raw := s.sign(t, s.claims(clock.Now(), defaultTestPrincipal, nil))

	_, err := v.verify(t.Context(), http.Header{})
	require.ErrorIs(t, err, errAssertionMissing)

	twice := http.Header{}
	twice.Add(identity.Header, raw)
	twice.Add(identity.Header, raw)
	_, err = v.verify(t.Context(), twice)
	require.ErrorIs(t, err, errAssertionInvalid)

	alias := assertionRequestHeader(raw)
	alias["X_Speakeasy_Identity"] = []string{raw}
	_, err = v.verify(t.Context(), alias)
	require.ErrorIs(t, err, errAssertionInvalid)

	aliasOnly := http.Header{"X_Speakeasy_Identity": []string{raw}}
	_, err = v.verify(t.Context(), aliasOnly)
	require.ErrorIs(t, err, errAssertionInvalid, "an underscore spelling alone is refused")

	lowercase := http.Header{"x-speakeasy-identity": []string{raw}}
	_, err = v.verify(t.Context(), lowercase)
	require.NoError(t, err, "the hyphenated name is matched in any case")

	noncanonical := assertionRequestHeader(raw)
	noncanonical["x-speakeasy-identity"] = []string{raw}
	_, err = v.verify(t.Context(), noncanonical)
	require.ErrorIs(t, err, errAssertionInvalid)

	_, err = v.verify(t.Context(), assertionRequestHeader(strings.Repeat("a", maxAssertionBytes+1)))
	require.ErrorIs(t, err, errAssertionInvalid)
}

func TestVerifierRejectsWrongAlgorithmAndType(t *testing.T) {
	t.Parallel()
	s := newTestSigner(t)
	clock := newTestClock()
	v := newTestVerifier(t, s, clock)
	claims := s.claims(clock.Now(), defaultTestPrincipal, nil)

	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	hs.Header["kid"] = s.kid
	hs.Header["typ"] = identity.TokenType
	raw, err := hs.SignedString([]byte("shared-secret"))
	require.NoError(t, err)
	_, err = v.verify(t.Context(), assertionRequestHeader(raw))
	require.ErrorIs(t, err, errAssertionInvalid)

	none := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	none.Header["kid"] = s.kid
	none.Header["typ"] = identity.TokenType
	raw, err = none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = v.verify(t.Context(), assertionRequestHeader(raw))
	require.ErrorIs(t, err, errAssertionInvalid)

	wrongType := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	wrongType.Header["kid"] = s.kid
	wrongType.Header["typ"] = "JWT"
	raw, err = wrongType.SignedString(s.key)
	require.NoError(t, err)
	_, err = v.verify(t.Context(), assertionRequestHeader(raw))
	require.ErrorIs(t, err, errAssertionInvalid)

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	forged := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	forged.Header["kid"] = s.kid
	forged.Header["typ"] = identity.TokenType
	raw, err = forged.SignedString(other)
	require.NoError(t, err)
	_, err = v.verify(t.Context(), assertionRequestHeader(raw))
	require.ErrorIs(t, err, errAssertionInvalid)
}

func TestVerifierUnknownKeyRefreshIsRateLimited(t *testing.T) {
	t.Parallel()
	s := newTestSigner(t)
	clock := newTestClock()
	v := newTestVerifier(t, s, clock)
	claims := s.claims(clock.Now(), defaultTestPrincipal, nil)
	_, err := v.verify(t.Context(), assertionRequestHeader(s.sign(t, claims)))
	require.NoError(t, err)
	require.Equal(t, int64(1), s.hits.Load())

	unknown := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	unknown.Header["kid"] = "unknown-kid"
	unknown.Header["typ"] = identity.TokenType
	raw, err := unknown.SignedString(s.key)
	require.NoError(t, err)

	clock.Advance(jwksUnknownKeyRetry)
	_, err = v.verify(t.Context(), assertionRequestHeader(raw))
	require.ErrorIs(t, err, errUnknownAssertKey)
	require.Equal(t, int64(2), s.hits.Load(), "an unknown kid refreshes once")

	_, err = v.verify(t.Context(), assertionRequestHeader(raw))
	require.ErrorIs(t, err, errUnknownAssertKey)
	require.Equal(t, int64(2), s.hits.Load(), "another unknown kid within the retry window does not refetch")

	s.down.Store(true)
	clock.Advance(jwksUnknownKeyRetry)
	_, err = v.verify(t.Context(), assertionRequestHeader(raw))
	require.Error(t, err)
	claims = s.claims(clock.Now(), defaultTestPrincipal, nil)
	_, err = v.verify(t.Context(), assertionRequestHeader(s.sign(t, claims)))
	require.NoError(t, err, "a failed refresh never evicts fresh keys")
}

func TestVerifierFailsClosedWithoutFreshKeys(t *testing.T) {
	t.Parallel()
	s := newTestSigner(t)
	clock := newTestClock()
	v := newTestVerifier(t, s, clock)

	s.down.Store(true)
	_, err := v.verify(t.Context(), assertionRequestHeader(s.sign(t, s.claims(clock.Now(), defaultTestPrincipal, nil))))
	require.ErrorIs(t, err, errVerificationKeys, "never fetched")

	s.down.Store(false)
	clock.Advance(jwksExpiredRetry)
	_, err = v.verify(t.Context(), assertionRequestHeader(s.sign(t, s.claims(clock.Now(), defaultTestPrincipal, nil))))
	require.NoError(t, err)

	s.down.Store(true)
	clock.Advance(jwksFreshFor)
	_, err = v.verify(t.Context(), assertionRequestHeader(s.sign(t, s.claims(clock.Now(), defaultTestPrincipal, nil))))
	require.ErrorIs(t, err, errVerificationKeys, "expired keys are not trusted while revalidation fails")
}

func TestVerifierRevalidatesWithETag(t *testing.T) {
	t.Parallel()
	s := newTestSigner(t)
	clock := newTestClock()
	v := newTestVerifier(t, s, clock)
	_, err := v.verify(t.Context(), assertionRequestHeader(s.sign(t, s.claims(clock.Now(), defaultTestPrincipal, nil))))
	require.NoError(t, err)

	clock.Advance(jwksFreshFor)
	_, err = v.verify(t.Context(), assertionRequestHeader(s.sign(t, s.claims(clock.Now(), defaultTestPrincipal, nil))))
	require.NoError(t, err, "a 304 renews freshness")
	require.Equal(t, int64(2), s.hits.Load())
}

func TestJWKSRejectsBadKeySets(t *testing.T) {
	t.Parallel()
	_, publicPEM := sharedTestKey(t)
	set, err := jwks.Parse(publicPEM)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	set.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, jwks.Path, nil))
	good := rec.Body.String()

	_, err = parseJWKS([]byte(good))
	require.NoError(t, err)
	_, err = parseJWKS([]byte(strings.Replace(good, `"kid":"`, `"kid":"x`, 1)))
	require.Error(t, err, "a kid that is not the key's thumbprint is ignored")
	_, err = parseJWKS([]byte(`{"keys":[]}`))
	require.Error(t, err)

	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"keys":[],"pad":"` + strings.Repeat("a", maxJWKSBytes) + `"}`))
	}))
	t.Cleanup(big.Close)
	_, _, _, err = newJWKSCache(big.URL, jwksHTTPClient(), time.Now).fetch("")
	require.ErrorContains(t, err, "too large")

	redirect := httptest.NewServer(http.RedirectHandler("https://evil.example.test/jwks.json", http.StatusFound))
	t.Cleanup(redirect.Close)
	_, _, _, err = newJWKSCache(redirect.URL, jwksHTTPClient(), time.Now).fetch("")
	require.ErrorContains(t, err, "status 302")
}

func TestCredentialsConfigValidation(t *testing.T) {
	t.Parallel()
	base := CredentialsConfig{Issuer: "https://tunnel.example.test", Audience: testAudience, OrganizationID: testOrg, JWKSURL: "", AllowInsecure: false, Root: "", MaxAge: 0}

	got, err := base.normalize()
	require.NoError(t, err)
	require.Equal(t, "https://tunnel.example.test/.well-known/jwks.json", got.JWKSURL)
	require.Equal(t, "/dev/shm", got.Root)
	require.Equal(t, time.Hour, got.MaxAge)

	cases := []struct {
		name   string
		mutate func(*CredentialsConfig)
	}{
		{"issuer trailing slash", func(c *CredentialsConfig) { c.Issuer += "/" }},
		{"issuer path", func(c *CredentialsConfig) { c.Issuer += "/x" }},
		{"issuer query", func(c *CredentialsConfig) { c.Issuer += "?a=b" }},
		{"issuer userinfo", func(c *CredentialsConfig) { c.Issuer = "https://u:p@tunnel.example.test" }},
		{"issuer http", func(c *CredentialsConfig) { c.Issuer = "http://tunnel.example.test" }},
		{"issuer http non-local with flag", func(c *CredentialsConfig) { c.Issuer = "http://tunnel.example.test"; c.AllowInsecure = true }},
		{"jwks http", func(c *CredentialsConfig) { c.JWKSURL = "http://localhost:8090/.well-known/jwks.json" }},
		{"jwks fragment", func(c *CredentialsConfig) { c.JWKSURL = "https://tunnel.example.test/jwks#x" }},
		{"missing audience", func(c *CredentialsConfig) { c.Audience = "" }},
		{"missing organization", func(c *CredentialsConfig) { c.OrganizationID = " " }},
		{"relative root", func(c *CredentialsConfig) { c.Root = "dev/shm" }},
		{"unclean root", func(c *CredentialsConfig) { c.Root = "/dev/shm/../tmp" }},
		{"negative max age", func(c *CredentialsConfig) { c.MaxAge = -time.Second }},
		{"max age over a day", func(c *CredentialsConfig) { c.MaxAge = 25 * time.Hour }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := base
			tc.mutate(&cfg)
			_, err := cfg.normalize()
			require.Error(t, err)
		})
	}

	local := base
	local.Issuer, local.JWKSURL, local.AllowInsecure = "http://localhost:8090", "http://host.docker.internal:8090/.well-known/jwks.json", true
	_, err = local.normalize()
	require.NoError(t, err)

	reserved := base
	reserved.Issuer, reserved.AllowInsecure = "http://tunnel.localhost:8090", true
	_, err = reserved.normalize()
	require.NoError(t, err, "RFC 6761 .localhost names are loopback")
}

//nolint:paralleltest // t.Setenv is process-global.
func TestCredentialsConfigRefusesOperatorTokenFile(t *testing.T) {
	t.Setenv(AccessTokenFileEnv, "")
	_, err := CredentialsConfig{Issuer: "https://tunnel.example.test", Audience: testAudience, OrganizationID: testOrg, JWKSURL: "", AllowInsecure: false, Root: "", MaxAge: 0}.normalize()
	require.ErrorContains(t, err, AccessTokenFileEnv)
}

func TestAdmitCredential(t *testing.T) {
	t.Parallel()
	now := time.Now()
	valid := func() map[string]any { return credentialClaim(defaultTestGrant, testTokenA, now.Add(time.Hour)) }
	cases := []struct {
		name   string
		header func(http.Header)
		claim  func() any
		err    error
	}{
		{"valid", nil, func() any { return valid() }, nil},
		{"unknown expiry", nil, func() any { c := valid(); delete(c, "token_expires_at"); return c }, nil},
		{"no bearer", func(h http.Header) { h.Del("Authorization") }, func() any { return valid() }, errCredentialRejected},
		{"two bearers", func(h http.Header) { h["authorization"] = []string{"Bearer " + testTokenA} }, func() any { return valid() }, errCredentialRejected},
		{"basic scheme", func(h http.Header) { h.Set("Authorization", "Basic "+testTokenA) }, func() any { return valid() }, errCredentialRejected},
		{"bearer with space", func(h http.Header) { h.Set("Authorization", "Bearer a b") }, func() any { return valid() }, errCredentialRejected},
		{"bearer with comma", func(h http.Header) { h.Set("Authorization", "Bearer a,b") }, func() any { return valid() }, errCredentialRejected},
		{"no claim", nil, func() any { return nil }, errCredentialRejected},
		{"self owner", nil, func() any { c := valid(); c["owner"] = identity.OwnerSelf; return c }, errCredentialRejected},
		{"missing grant id", nil, func() any { c := valid(); delete(c, "grant_id"); return c }, errCredentialRejected},
		{"nil client id", nil, func() any { c := valid(); c["client_id"] = "00000000-0000-0000-0000-000000000000"; return c }, errCredentialRejected},
		{"missing generation", nil, func() any { c := valid(); delete(c, "grant_generation"); return c }, errCredentialRejected},
		{"zero generation", nil, func() any { c := valid(); c["grant_generation"] = 0; return c }, errCredentialRejected},
		{"fractional generation", nil, func() any { c := valid(); c["grant_generation"] = 1.5; return c }, errCredentialRejected},
		{"uppercase digest", nil, func() any {
			c := valid()
			c["token_sha256"] = strings.ToUpper(identity.TokenSHA256(testTokenA))
			return c
		}, errCredentialRejected},
		{"digest of another token", nil, func() any { c := valid(); c["token_sha256"] = identity.TokenSHA256(testTokenB); return c }, errCredentialRejected},
		{"null expiry", nil, func() any { c := valid(); c["token_expires_at"] = nil; return c }, errCredentialRejected},
		{"string expiry", nil, func() any { c := valid(); c["token_expires_at"] = "soon"; return c }, errCredentialRejected},
		{"expired token", nil, func() any { c := valid(); c["token_expires_at"] = now.Add(-time.Second).Unix(); return c }, errCredentialExpired},
		{"token expiring now", nil, func() any { c := valid(); c["token_expires_at"] = now.Truncate(time.Second).Unix(); return c }, errCredentialExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := http.Header{}
			h.Set("Authorization", "Bearer "+testTokenA)
			if tc.header != nil {
				tc.header(h)
			}
			var raw []byte
			if claim := tc.claim(); claim != nil {
				encoded, err := json.Marshal(claim)
				require.NoError(t, err)
				raw = encoded
			}
			got, err := admitCredential(h, callerAssertion{principal: principal{}, expiresAt: now.Add(time.Minute), allowedMethods: nil, credential: raw}, now)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, credentialContext{clientID: defaultTestGrant.clientID, grantID: defaultTestGrant.grantID, grantGeneration: 1}, got.context)
		})
	}
}

func TestCredentialChildEnvReplacesInheritedValues(t *testing.T) {
	t.Parallel()
	env := credentialChildEnv([]string{"PATH=/bin", "HOME=/root", "OKTA_ACCESS_TOKEN_FILE=/shared/token", AccessTokenFileEnv + "=/shared/other", "XDG_CONFIG_HOME=/etc"}, "/s/token", "/s/home")
	require.Equal(t, []string{"PATH=/bin", "XDG_CACHE_HOME=/root/.cache", "npm_config_cache=/root/.cache/npm", AccessTokenFileEnv + "=/s/token", "HOME=/s/home", "XDG_CONFIG_HOME=/s/home/.config", "XDG_DATA_HOME=/s/home/.local/share", "XDG_STATE_HOME=/s/home/.local/state"}, env)
}

func TestCredentialChildEnvKeepsCachesOutOfTheSession(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		base []string
		want []string
	}{
		{"explicit caches kept", []string{"HOME=/root", "XDG_CACHE_HOME=/var/cache/mcp", "NPM_CONFIG_CACHE=/var/cache/npm"}, []string{"XDG_CACHE_HOME=/var/cache/mcp", "NPM_CONFIG_CACHE=/var/cache/npm"}},
		{"npm follows an explicit XDG cache", []string{"HOME=/root", "XDG_CACHE_HOME=/var/cache/mcp"}, []string{"XDG_CACHE_HOME=/var/cache/mcp", "npm_config_cache=/var/cache/mcp/npm"}},
		{"empty XDG cache treated as unset", []string{"HOME=/home/agent", "XDG_CACHE_HOME="}, []string{"XDG_CACHE_HOME=/home/agent/.cache", "npm_config_cache=/home/agent/.cache/npm"}},
		{"no agent home adds nothing", []string{"PATH=/bin"}, []string{"PATH=/bin"}},
		{"relative agent home adds nothing", []string{"HOME=home"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := credentialChildEnv(tc.base, "/s/token", "/s/home")
			require.Equal(t, tc.want, env[:len(env)-5], "cache settings never point into the session")
		})
	}
}

func TestCredentialDeadlineIsEarlierOfExpiryAndMaxAge(t *testing.T) {
	t.Parallel()
	now := time.Now()
	c := newSessionCredentials(principal{}, credentialContext{}, nil, time.Hour, 30*time.Second, time.Now)
	require.Equal(t, time.Hour, c.deadline(admittedCredential{token: "", context: credentialContext{}, expiresAt: time.Time{}, assertionExpiresAt: now}, now), "unknown expiry uses the maximum age")
	require.Equal(t, 10*time.Minute+30*time.Second, c.deadline(admittedCredential{token: "", context: credentialContext{}, expiresAt: now.Add(10 * time.Minute), assertionExpiresAt: now}, now))
	require.Equal(t, time.Hour, c.deadline(admittedCredential{token: "", context: credentialContext{}, expiresAt: now.Add(24 * time.Hour), assertionExpiresAt: now}, now), "a long-lived token still ends at the maximum age")
}

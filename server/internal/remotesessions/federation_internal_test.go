package remotesessions

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func federatedFixture(t *testing.T) *FederatedProvider {
	t.Helper()
	issuer := repo.RemoteSessionIssuer{ID: uuid.New(), Issuer: "https://idp.example.test/tenant"}
	client := repo.RemoteSessionClient{ID: uuid.New(), RemoteSessionIssuerID: issuer.ID, ClientID: "upstream-client", Scope: []string{"openid", "email", "profile", "offline_access"}, TokenEndpointAuthMethod: pgtype.Text{String: "client_secret_basic", Valid: true}, ClientSecretEncrypted: pgtype.Text{String: "fixture-secret", Valid: true}}
	doc := rfc8414Document{AuthorizationResponseIssParameterSupported: true, Issuer: issuer.Issuer, AuthorizationEndpoint: "https://idp.example.test/authorize", TokenEndpoint: "https://idp.example.test/token", JwksURI: "https://idp.example.test/jwks", CodeChallengeMethodsSupported: []string{"S256"}, IDTokenSigningAlgValuesSupported: []string{"RS256"}}
	p, err := newFederatedProvider("org-test", issuer, client, doc)
	require.NoError(t, err)
	return p
}

func TestFederatedAuthorization(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	verifier := strings.Repeat("a", 43)
	u, err := p.BuildAuthorizationURL("https://gram.example.test/callback", "state", "nonce", verifier)
	require.NoError(t, err)
	q := u.Query()
	sum := sha256.Sum256([]byte(verifier))
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, "upstream-client", q.Get("client_id"))
	require.Equal(t, "openid email profile", q.Get("scope"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), q.Get("code_challenge"))
	require.Equal(t, "nonce", q.Get("nonce"))
	require.Equal(t, "state", q.Get("state"))
	require.Empty(t, q.Get("prompt"))
	for _, verifier := range []string{"", "short", strings.Repeat("a", 129), strings.Repeat("/", 43)} {
		_, err := p.BuildAuthorizationURL("https://gram.example.test/callback", "state", "nonce", verifier)
		require.ErrorIs(t, err, ErrFederatedIdentity)
	}
}

func TestFederatedResponseIssuer(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	require.ErrorIs(t, p.ValidateResponseIssuer(""), ErrFederatedIdentity)
	require.NoError(t, p.ValidateResponseIssuer(p.issuer.Issuer))
	require.ErrorIs(t, p.ValidateResponseIssuer(p.issuer.Issuer+"/"), ErrFederatedIdentity)
	p.metadata.AuthorizationResponseIssParameterSupported = false
	require.ErrorIs(t, p.ValidateResponseIssuer(""), ErrFederatedIdentity)
	require.NoError(t, p.ValidateResponseIssuer(p.issuer.Issuer))
}

func TestFederatedConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mutate func(*FederatedProvider)
	}{
		{"exact discovery issuer", func(p *FederatedProvider) { p.metadata.Issuer += "/" }},
		{"missing response issuer support", func(p *FederatedProvider) { p.metadata.AuthorizationResponseIssParameterSupported = false }},
		{"missing jwks", func(p *FederatedProvider) { p.metadata.JwksURI = "" }},
		{"insecure token endpoint", func(p *FederatedProvider) { p.metadata.TokenEndpoint = "http://idp.example.test/token" }},
		{"endpoint fragment", func(p *FederatedProvider) { p.metadata.AuthorizationEndpoint += "#fragment" }},
		{"missing email scope", func(p *FederatedProvider) { p.client.Scope = []string{"openid"} }},
		{"missing openid scope", func(p *FederatedProvider) { p.client.Scope = []string{"email"} }},
		{"unsupported PKCE", func(p *FederatedProvider) { p.metadata.CodeChallengeMethodsSupported = []string{"plain"} }},
		{"symmetric algorithm", func(p *FederatedProvider) { p.metadata.IDTokenSigningAlgValuesSupported = []string{"HS256"} }},
		{"unknown client auth", func(p *FederatedProvider) { p.client.TokenEndpointAuthMethod.String = "invalid" }},
		{"missing client secret", func(p *FederatedProvider) { p.client.ClientSecretEncrypted = pgtype.Text{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p := federatedFixture(t)
			test.mutate(p)
			_, err := newFederatedProvider(p.organizationID, p.issuer, p.client, p.metadata)
			require.ErrorIs(t, err, ErrFederatedConfiguration)
		})
	}
}

func TestFederatedFingerprint(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	same, err := newFederatedProvider(p.organizationID, p.issuer, p.client, p.metadata)
	require.NoError(t, err)
	require.Equal(t, p.Fingerprint(), same.Fingerprint())
	for _, mutate := range []func(*FederatedProvider){
		func(p *FederatedProvider) {
			p.client.ClientSecretEncrypted = pgtype.Text{String: "encrypted-secret", Valid: true}
		},
		func(p *FederatedProvider) { p.metadata.TokenEndpoint += "/new" },
		func(p *FederatedProvider) { p.client.Scope = append(p.client.Scope, "extra") },
		func(p *FederatedProvider) { p.organizationID = "other-org" },
	} {
		changed := *p
		mutate(&changed)
		next, err := newFederatedProvider(changed.organizationID, changed.issuer, changed.client, changed.metadata)
		require.NoError(t, err)
		require.NotEqual(t, p.Fingerprint(), next.Fingerprint())
	}
	p.client.ClientSecretEncrypted = pgtype.Text{String: "encrypted-secret", Valid: true}
	encoded, err := json.Marshal(p)
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(encoded))
	require.NotContains(t, fmt.Sprintf("%+v %#v", p, p), "encrypted-secret")
}

func federatedClaims(t *testing.T, p *FederatedProvider, now time.Time) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(map[string]any{"iss": p.issuer.Issuer, "sub": "foreign-subject", "aud": p.client.ClientID, "exp": now.Add(time.Minute).Unix(), "iat": now.Unix(), "nonce": "nonce", "email": "person@example.test", "email_verified": true})
	require.NoError(t, err)
	var claims map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &claims))
	return claims
}

func TestFederatedClaims(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	now := time.Now()
	for _, test := range []struct{ name, claim, value string }{
		{"issuer trailing slash", "iss", `"https://idp.example.test/tenant/"`},
		{"wrong audience", "aud", `"downstream-client"`},
		{"multiple audience without azp", "aud", `["upstream-client","other"]`},
		{"wrong azp", "azp", `"other"`},
		{"null azp", "azp", `null`},
		{"missing iat", "iat", ``},
		{"missing exp", "exp", ``},
		{"expired", "exp", fmt.Sprint(now.Add(-2 * time.Minute).Unix())},
		{"future iat", "iat", fmt.Sprint(now.Add(2 * time.Minute).Unix())},
		{"wrong nonce", "nonce", `"different"`},
		{"missing subject", "sub", ``},
		{"missing email", "email", ``},
		{"string email_verified", "email_verified", `"true"`},
		{"null email_verified", "email_verified", `null`},
		{"wrong access hash", "at_hash", `"wrong"`},
		{"wrong code hash", "c_hash", `"wrong"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			claims := federatedClaims(t, p, now)
			if test.value == "" {
				delete(claims, test.claim)
			} else {
				claims[test.claim] = json.RawMessage(test.value)
			}
			_, err := validateFederatedClaims(claims, p, tokenResponse{AccessToken: "access"}, "code", "nonce", "RS256", now)
			require.ErrorIs(t, err, ErrFederatedIdentity)
		})
	}
	for _, verification := range []string{"true", "false", ""} {
		claims := federatedClaims(t, p, now)
		if verification == "" {
			delete(claims, "email_verified")
		} else {
			claims["email_verified"] = json.RawMessage(verification)
		}
		result, err := validateFederatedClaims(claims, p, tokenResponse{}, "code", "nonce", "RS256", now)
		require.NoError(t, err)
		require.Equal(t, "foreign-subject", result.Subject)
		if verification == "" {
			require.Nil(t, result.EmailVerified)
		} else {
			require.Equal(t, verification == "true", *result.EmailVerified)
		}
	}
}

func TestFederatedTokenHashes(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte("access"))
	claim := base64.RawURLEncoding.EncodeToString(sum[:len(sum)/2])
	require.True(t, validFederatedTokenHash(claim, "access", "RS256"))
	require.False(t, validFederatedTokenHash(claim, "other", "RS256"))
	require.False(t, validFederatedTokenHash(claim, "access", "HS256"))
	require.False(t, validFederatedTokenHash("", "access", "RS256"))
}

func TestFederatedEffectiveScopes(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	p.client.Scope = []string{"profile"}
	p.issuer.ScopeOverride = []string{"openid", "email", "override", "offline_access"}
	_, err := newFederatedProvider(p.organizationID, p.issuer, p.client, p.metadata)
	require.ErrorIs(t, err, ErrFederatedConfiguration, "issuer overrides cannot repair the client allowlist")
	p.client.Scope = []string{"openid", "email", "profile"}
	p.issuer.ScopeOverride = []string{"downstream:admin"}
	next, err := newFederatedProvider(p.organizationID, p.issuer, p.client, p.metadata)
	require.NoError(t, err)
	u, err := next.BuildAuthorizationURL("https://gram.example.test/callback", "state", "nonce", strings.Repeat("a", 43))
	require.NoError(t, err)
	require.Equal(t, "openid email profile", u.Query().Get("scope"))
}

func TestFederatedRuntimePolicy(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*FederatedProvider){
		"public client":        func(p *FederatedProvider) { p.client.TokenEndpointAuthMethod.String = "none" },
		"implicit response":    func(p *FederatedProvider) { p.metadata.ResponseTypesSupported = []string{"id_token"} },
		"empty response types": func(p *FederatedProvider) { p.metadata.ResponseTypesSupported = []string{} },
		"HTTP loopback JWKS":   func(p *FederatedProvider) { p.metadata.JwksURI = "http://127.0.0.1/jwks" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := federatedFixture(t)
			mutate(p)
			_, err := newFederatedProvider(p.organizationID, p.issuer, p.client, p.metadata)
			require.ErrorIs(t, err, ErrFederatedConfiguration)
		})
	}
}

type federatedAssertionSignerFunc func(context.Context, ClientAssertionRequest) (string, error)

func (f federatedAssertionSignerFunc) SignClientAssertion(ctx context.Context, r ClientAssertionRequest) (string, error) {
	return f(ctx, r)
}

func TestFederatedSignerPreflightAndExchange(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	p.client.TokenEndpointAuthMethod.String = "private_key_jwt"
	p.client.JsonWebKeySetID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	for _, signer := range []TokenEndpointAssertionSigner{nil, unavailableTokenEndpointAssertionSigner{}} {
		m := &ChallengeManager{assertions: signer}
		require.ErrorIs(t, m.preflightFederatedSigner(p), ErrFederatedUnavailable)
		_, err := newTokenEndpointRequest(t.Context(), p.metadata.TokenEndpoint, url.Values{}, tokenEndpointClientAuth{Method: TokenEndpointAuthMethodPrivateKeyJWT, AssertionSigner: signer})
		require.ErrorIs(t, classifyFederatedExchangeError(err), ErrFederatedUnavailable)
	}
	calls := 0
	signer := federatedAssertionSignerFunc(func(context.Context, ClientAssertionRequest) (string, error) { calls++; return "signed-assertion", nil })
	m := &ChallengeManager{assertions: signer}
	require.NoError(t, m.preflightFederatedSigner(p))
	require.Zero(t, calls, "authorize must not perform speculative signing")
	doer := federatedHTTPDoerFunc(func(r *http.Request) (*http.Response, error) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "signed-assertion", r.Form.Get("client_assertion"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"access"}`))}, nil
	})
	_, err := m.exchangeCode(t.Context(), doer, RemoteLoginState{TokenEndpoint: p.metadata.TokenEndpoint}, tokenEndpointClientAuth{Method: TokenEndpointAuthMethodPrivateKeyJWT, AssertionSigner: signer}, "", "code")
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	failing := federatedAssertionSignerFunc(func(context.Context, ClientAssertionRequest) (string, error) {
		return "", errors.New("sensitive signing detail")
	})
	_, err = m.exchangeCode(t.Context(), doer, RemoteLoginState{TokenEndpoint: p.metadata.TokenEndpoint}, tokenEndpointClientAuth{Method: TokenEndpointAuthMethodPrivateKeyJWT, AssertionSigner: failing}, "", "code")
	require.ErrorIs(t, classifyFederatedExchangeError(err), ErrFederatedSigning)
	require.NotContains(t, classifyFederatedExchangeError(err).Error(), "sensitive")
}

func TestFederatedExchangeErrorClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		err  error
		want error
	}{
		{&tokenExchangeUnavailableError{err: errors.New("transport detail")}, ErrFederatedUnavailable},
		{newTokenEndpointError(503, "503", []byte("upstream detail")), ErrFederatedUnavailable},
		{newTokenEndpointError(429, "429", nil), ErrFederatedUnavailable},
		{newTokenEndpointError(http.StatusRequestTimeout, "408", nil), ErrFederatedUnavailable},
		{newTokenEndpointError(400, "400", []byte(`{"error":"invalid_client","error_description":"secret"}`)), ErrFederatedConfiguration},
		{newTokenEndpointError(400, "400", []byte(`{"error":"invalid_grant","error_description":"secret"}`)), ErrFederatedIdentity},
	} {
		require.ErrorIs(t, classifyFederatedExchangeError(test.err), test.want)
		require.NotContains(t, classifyFederatedExchangeError(test.err).Error(), "detail")
	}
}

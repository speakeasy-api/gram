package remotesessions_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

func newJWTAccessTokenEnricher(t *testing.T, issuer *idTokenIssuer) *remotesessions.SessionEnricher {
	t.Helper()

	logger := testenv.NewLogger(t)
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	cache := jwks.NewMemoryCache()
	require.NoError(t, cache.Put(t.Context(), issuer.jwksURI, jwks.CacheState{
		Document: issuer.keySet, ExpiresAt: time.Now().Add(time.Hour), RefreshedAt: time.Now(),
	}))
	keys, err := jwks.NewKeyResolver(
		jwks.NewResolver(policy, testenv.NewMeterProvider(t), logger),
		cache,
		ratelimit.New(nil, "jwt_access_token_test_refresh", ratelimit.PerMinute(1)),
		nil,
		logger,
	)
	require.NoError(t, err)
	return remotesessions.NewSessionEnricher(logger, nil, policy, keys, nil)
}

func mintAccessToken(t *testing.T, issuer *idTokenIssuer, typ *string, claims map[string]any) string {
	t.Helper()

	opts := (&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), "synthetic-kid")
	if typ != nil {
		opts.WithType(jose.ContentType(*typ))
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: issuer.key}, opts)
	require.NoError(t, err)
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return raw
}

func mintAccessTokenWithTypeValue(t *testing.T, issuer *idTokenIssuer, typ any, claims map[string]any) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), "synthetic-kid").WithHeader(jose.HeaderType, typ)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: issuer.key}, opts)
	require.NoError(t, err)
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return raw
}

func accessTokenClaims(issuerURL string, audience any) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":       issuerURL,
		"sub":       "user-123",
		"aud":       audience,
		"exp":       now.Add(5 * time.Minute).Unix(),
		"iat":       now.Unix(),
		"client_id": "oauth-client",
		"jti":       uuid.NewString(),
		"email":     "owner@example.com",
		"name":      "Grant Owner",
	}
}

func TestSessionEnricherJWTAccessTokenTypesAndAudience(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	const clientID = "oauth-client"
	const resource = "https://api.example.com"

	tests := []struct {
		name     string
		typ      *string
		resource string
		audience any
		wantOK   bool
		reason   string
	}{
		{name: "at+jwt uses client audience without resource", typ: new("at+jwt"), audience: clientID, wantOK: true},
		{name: "at+jwt uses resource audience", typ: new("at+jwt"), resource: resource, audience: resource, wantOK: true},
		{name: "application at+jwt accepts resource in audience list", typ: new("application/at+jwt"), resource: resource, audience: []string{"other", resource}, wantOK: true},
		{name: "dedicated type rejects client audience when resource recorded", typ: new("at+jwt"), resource: resource, audience: clientID, reason: "audience mismatch"},
		{name: "JWT accepted for distinct resource", typ: new("JWT"), resource: resource, audience: resource, wantOK: true},
		{name: "application jwt accepted for distinct resource", typ: new("application/jwt"), resource: resource, audience: resource, wantOK: true},
		{name: "missing type accepted for distinct resource", resource: resource, audience: resource, wantOK: true},
		{name: "generic type requires resource", typ: new("JWT"), audience: clientID, reason: "ambiguous token type"},
		{name: "missing type requires resource", audience: clientID, reason: "ambiguous token type"},
		{name: "generic type requires resource distinct from client", typ: new("application/jwt"), resource: clientID, audience: clientID, reason: "ambiguous token type"},
		{name: "other explicit type rejected", typ: new("id+jwt"), resource: resource, audience: resource, reason: "wrong token type"},
		{name: "explicit empty type rejected", typ: new(""), resource: resource, audience: resource, reason: "wrong token type"},
		{name: "leading whitespace is not trimmed", typ: new(" at+jwt"), resource: resource, audience: resource, reason: "wrong token type"},
		{name: "trailing whitespace is not trimmed", typ: new("at+jwt "), resource: resource, audience: resource, reason: "wrong token type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := remotesessions.JWTAccessTokenTarget{
				IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI,
				ExternalClientID: clientID, Resource: tt.resource,
			}
			result := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, tt.typ, accessTokenClaims(issuer.issuerURL, tt.audience)))
			if !tt.wantOK {
				require.Equal(t, "failed", result.Status)
				require.Equal(t, tt.reason, result.Reason)
				require.Empty(t, result.Subject)
				return
			}
			require.True(t, result.Ran)
			require.Equal(t, "ok", result.Status)
			require.Equal(t, "user-123", result.Subject)
			require.Equal(t, "owner@example.com", result.Email)
			require.Equal(t, "Grant Owner", result.DisplayName)
			require.Equal(t, remotesessions.IdentitySourceJWTAccessToken, result.Source)
		})
	}
}

func TestSessionEnricherJWTAccessTokenClaimsSignatureAndScope(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	target := remotesessions.JWTAccessTokenTarget{
		IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client",
	}

	t.Run("required and temporal claims", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name   string
			mutate func(map[string]any)
			reason string
		}{
			{name: "wrong issuer", mutate: func(c map[string]any) { c["iss"] = "https://other.example.com" }, reason: "invalid claims"},
			{name: "missing issuer", mutate: func(c map[string]any) { delete(c, "iss") }, reason: "missing required claims"},
			{name: "missing subject", mutate: func(c map[string]any) { delete(c, "sub") }, reason: "missing required claims"},
			{name: "missing expiry", mutate: func(c map[string]any) { delete(c, "exp") }, reason: "missing required claims"},
			{name: "expired", mutate: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, reason: "invalid claims"},
			{name: "not valid yet", mutate: func(c map[string]any) { c["nbf"] = time.Now().Add(time.Hour).Unix() }, reason: "invalid claims"},
			{name: "wrong audience", mutate: func(c map[string]any) { c["aud"] = "other-client" }, reason: "audience mismatch"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				claims := accessTokenClaims(issuer.issuerURL, "oauth-client")
				tt.mutate(claims)
				result := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), claims))
				require.Equal(t, "failed", result.Status)
				require.Equal(t, tt.reason, result.Reason)
				require.Empty(t, result.Subject)
			})
		}
	})

	t.Run("malformed token type", func(t *testing.T) {
		t.Parallel()
		for _, typ := range []any{42, true} {
			result := enricher.JWTAccessToken(t.Context(), target, mintAccessTokenWithTypeValue(t, issuer, typ, accessTokenClaims(issuer.issuerURL, "oauth-client")))
			require.Equal(t, "failed", result.Status)
			require.Equal(t, "invalid token type", result.Reason)
		}
		// go-jose drops a null header member, so a null typ reads as absent.
		result := enricher.JWTAccessToken(t.Context(), target, mintAccessTokenWithTypeValue(t, issuer, json.RawMessage("null"), accessTokenClaims(issuer.issuerURL, "oauth-client")))
		require.Equal(t, "failed", result.Status)
		require.Equal(t, "ambiguous token type", result.Reason)
	})

	t.Run("signature", func(t *testing.T) {
		t.Parallel()
		other := newIDTokenIssuer(t)
		result := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, other, new("at+jwt"), accessTokenClaims(issuer.issuerURL, "oauth-client")))
		require.Equal(t, "failed", result.Status)
		require.Equal(t, "unverifiable token", result.Reason)
		require.Empty(t, result.Subject)
	})

	t.Run("scope presence", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name        string
			scope       any
			include     bool
			wantPresent bool
			wantScopes  []string
			wantReason  string
		}{
			{name: "absent"},
			{name: "empty", include: true, scope: "", wantPresent: true, wantReason: "invalid scope"},
			{name: "fields", include: true, scope: "read write admin", wantPresent: true, wantScopes: []string{"read", "write", "admin"}},
			{name: "whitespace runs", include: true, scope: " read\n\twrite  ", wantPresent: true, wantReason: "invalid scope"},
			{name: "invalid character", include: true, scope: "read\"write", wantPresent: true, wantReason: "invalid scope"},
			{name: "null", include: true, scope: nil, wantPresent: true, wantReason: "invalid scope"},
			{name: "non-string", include: true, scope: []string{"read"}, wantPresent: true, wantReason: "invalid scope"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				claims := accessTokenClaims(issuer.issuerURL, "oauth-client")
				if tt.include {
					claims["scope"] = tt.scope
				}
				result := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), claims))
				require.Equal(t, tt.wantPresent, result.ScopePresent)
				if tt.wantReason != "" {
					require.Equal(t, "failed", result.Status)
					require.Equal(t, tt.wantReason, result.Reason)
					require.Empty(t, result.Subject)
					return
				}
				require.Equal(t, "ok", result.Status)
				require.Equal(t, tt.wantScopes, result.Scopes)
			})
		}
	})
}

func TestSessionEnricherJWTAccessTokenGenericTypeClientBinding(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	const resource = "https://api.example.com"
	target := remotesessions.JWTAccessTokenTarget{
		IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client", Resource: resource,
	}
	for _, tt := range []struct {
		name   string
		claim  string
		value  any
		reason string
	}{
		{name: "client_id matches", claim: "client_id", value: "oauth-client"},
		{name: "azp matches", claim: "azp", value: "oauth-client"},
		{name: "client_id differs", claim: "client_id", value: "other-client", reason: "client mismatch"},
		{name: "azp differs", claim: "azp", value: "other-client", reason: "client mismatch"},
		{name: "client_id not a string", claim: "client_id", value: 7, reason: "client mismatch"},
	} {
		claims := accessTokenClaims(issuer.issuerURL, resource)
		delete(claims, "client_id")
		claims[tt.claim] = tt.value
		result := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("JWT"), claims))
		if tt.reason != "" {
			require.Equal(t, "failed", result.Status, tt.name)
			require.Equal(t, tt.reason, result.Reason, tt.name)
			require.Empty(t, result.Subject, tt.name)
			continue
		}
		require.Equal(t, "ok", result.Status, tt.name)
		require.Equal(t, "user-123", result.Subject, tt.name)
	}
}

func TestSessionEnricherJWTAccessTokenResourceIndicatorUnsupported(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	const resource = "https://api.example.com"
	target := remotesessions.JWTAccessTokenTarget{
		IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client",
		Resource: resource, ResourceIndicatorUnsupported: true,
	}
	// The resource never reached the issuer, so a dedicated token names the client.
	result := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), accessTokenClaims(issuer.issuerURL, "oauth-client")))
	require.Equal(t, "ok", result.Status)
	require.Equal(t, "user-123", result.Subject)

	result = enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), accessTokenClaims(issuer.issuerURL, resource)))
	require.Equal(t, "failed", result.Status)
	require.Equal(t, "audience mismatch", result.Reason)

	result = enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("JWT"), accessTokenClaims(issuer.issuerURL, resource)))
	require.Equal(t, "failed", result.Status)
	require.Equal(t, "ambiguous token type", result.Reason, "a generic token has no distinct audience to bind to")
}

func TestSessionEnricherJWTAccessTokenSkipsWithoutKeySet(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	raw := mintAccessToken(t, issuer, new("at+jwt"), accessTokenClaims(issuer.issuerURL, "oauth-client"))

	noJWKS := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, ExternalClientID: "oauth-client"}
	result := newJWTAccessTokenEnricher(t, issuer).JWTAccessToken(t.Context(), noJWKS, raw)
	require.False(t, result.Ran, "an issuer without jwks_uri is a configuration state, not a failed interface")
	require.Empty(t, result.Reason)

	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	noKeys := remotesessions.NewSessionEnricher(testenv.NewLogger(t), nil, policy, nil, nil)
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client"}
	result = noKeys.JWTAccessToken(t.Context(), target, raw)
	require.False(t, result.Ran, "an enricher without a key resolver never records the interface")

	result = newJWTAccessTokenEnricher(t, issuer).JWTAccessToken(t.Context(), target, "opaque-access-token")
	require.False(t, result.Ran, "an opaque token is not examined")
}

// failingJWKSCache is a key-set store whose reads fail, as an outage would.
type failingJWKSCache struct{}

func (failingJWKSCache) Get(context.Context, string) (jwks.CacheState, error) {
	return jwks.CacheState{Document: nil, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}}, errors.New("cache unavailable")
}

func (failingJWKSCache) Put(context.Context, string, jwks.CacheState) error { return nil }

func TestSessionEnricherJWTAccessTokenTransientKeySetFailureIsNotRecorded(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	logger := testenv.NewLogger(t)
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	keys, err := jwks.NewKeyResolver(
		jwks.NewResolver(policy, testenv.NewMeterProvider(t), logger),
		failingJWKSCache{},
		ratelimit.New(nil, "jwt_access_token_test_transient", ratelimit.PerMinute(1)),
		nil,
		logger,
	)
	require.NoError(t, err)
	enricher := remotesessions.NewSessionEnricher(logger, nil, policy, keys, nil)
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client"}
	result := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), accessTokenClaims(issuer.issuerURL, "oauth-client")))
	require.False(t, result.Ran, "a key set that could not be consulted says nothing about the token")
	require.Empty(t, result.Reason)
}

func TestSessionEnricherJWTAccessTokenRetainsAllowlistedClaimsOnly(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client"}
	claims := accessTokenClaims(issuer.issuerURL, "oauth-client")
	claims["groups"] = []string{"admins"}
	claims["roles"] = []string{"owner"}
	claims["scope"] = "read"
	claims["preferred_username"] = "owner"
	result := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), claims))
	require.Equal(t, "ok", result.Status)
	require.NotContains(t, result.Claims, "groups")
	require.NotContains(t, result.Claims, "roles")
	for _, name := range []string{"sub", "iss", "aud", "exp", "iat", "jti", "client_id", "scope", "email", "name", "preferred_username"} {
		require.Contains(t, result.Claims, name)
	}
}

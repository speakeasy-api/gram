package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestJWTAccessTokenRFC9068RequiredClaims(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client"}
	// A registered claim of the wrong shape fails claim decoding, so the token as a whole is unverifiable.
	for _, tc := range []struct {
		claim  string
		name   string
		value  any
		omit   bool
		reason string
	}{
		{claim: "client_id", name: "missing", omit: true, reason: "missing required profile claims"},
		{claim: "client_id", name: "null", value: nil, reason: "missing required profile claims"},
		{claim: "client_id", name: "empty", value: "", reason: "missing required profile claims"},
		{claim: "client_id", name: "wrong type", value: []string{"invalid"}, reason: "missing required profile claims"},
		{claim: "iat", name: "missing", omit: true, reason: "missing required profile claims"},
		{claim: "iat", name: "null", value: nil, reason: "missing required profile claims"},
		{claim: "iat", name: "empty", value: "", reason: "unverifiable token"},
		{claim: "iat", name: "wrong type", value: []string{"invalid"}, reason: "unverifiable token"},
		{claim: "jti", name: "missing", omit: true, reason: "missing required profile claims"},
		{claim: "jti", name: "null", value: nil, reason: "missing required profile claims"},
		{claim: "jti", name: "empty", value: "", reason: "missing required profile claims"},
		{claim: "jti", name: "wrong type", value: []string{"invalid"}, reason: "unverifiable token"},
	} {
		t.Run(tc.claim+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			claims := accessTokenClaims(issuer.issuerURL, "oauth-client")
			claims["client_id"], claims["jti"] = "oauth-client", "example-token-id"
			if tc.omit {
				delete(claims, tc.claim)
			} else {
				claims[tc.claim] = tc.value
			}
			out := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), claims))
			require.Equal(t, "failed", out.Status)
			require.Equal(t, tc.reason, out.Reason)
			require.Empty(t, out.Subject)
		})
	}
}

func TestJWTAccessTokenClientBindingPolicy(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client"}
	claims := accessTokenClaims(issuer.issuerURL, "oauth-client")
	claims["client_id"] = "other-client"
	out := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), claims))
	require.Equal(t, "failed", out.Status)
	require.Equal(t, "client mismatch", out.Reason)
}

func TestJWTAccessTokenGenericCompatibilityDoesNotRequireProfileClaims(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	const resource = "https://resource.example.com"
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client", Resource: resource}
	claims := accessTokenClaims(issuer.issuerURL, resource)
	delete(claims, "client_id")
	delete(claims, "iat")
	delete(claims, "jti")
	out := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("JWT"), claims))
	require.Equal(t, "ok", out.Status)
	require.Equal(t, "user-123", out.Subject)
	require.Equal(t, "owner@example.com", out.Email)
	require.Equal(t, "Grant Owner", out.DisplayName)
}

func TestJWTAccessTokenRequiresExactIssuer(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL + "/", JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client"}
	claims := accessTokenClaims(issuer.issuerURL, "oauth-client")
	claims["client_id"], claims["jti"] = "oauth-client", "example-token-id"
	out := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), claims))
	require.Equal(t, "failed", out.Status)
	require.Equal(t, "invalid claims", out.Reason)
}

func TestJWTAccessTokenRFC6749ScopeSyntax(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	enricher := newJWTAccessTokenEnricher(t, issuer)
	target := remotesessions.JWTAccessTokenTarget{IssuerID: uuid.New(), IssuerURL: issuer.issuerURL, JWKSURI: issuer.jwksURI, ExternalClientID: "oauth-client"}
	for _, scope := range []string{"", " read", "read ", "read  write", "read\twrite", "read\nwrite", "read\u00a0write", "réad", "read\"write", "read\\write", "read\x7fwrite"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			claims := accessTokenClaims(issuer.issuerURL, "oauth-client")
			claims["client_id"], claims["jti"], claims["scope"] = "oauth-client", "example-token-id", scope
			out := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), claims))
			require.Equal(t, "failed", out.Status)
			require.Equal(t, "invalid scope", out.Reason)
			require.Empty(t, out.Subject)
		})
	}
	for scope, want := range map[string][]string{"read": {"read"}, "read write": {"read", "write"}} {
		claims := accessTokenClaims(issuer.issuerURL, "oauth-client")
		claims["client_id"], claims["jti"], claims["scope"] = "oauth-client", "example-token-id", scope
		out := enricher.JWTAccessToken(t.Context(), target, mintAccessToken(t, issuer, new("at+jwt"), claims))
		require.Equal(t, "ok", out.Status, "%q", scope)
		require.True(t, out.ScopePresent, "%q", scope)
		require.Equal(t, want, out.Scopes, "%q", scope)
	}
}

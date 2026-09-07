package usersessions

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// validateAfterDefaults runs the production order — SetDefaults then
// Validate — on the supplied request against this server's own accepted
// method set, and returns the validation error.
func validateAfterDefaults(req *RegistrationRequest) error {
	req.SetDefaults()
	return req.Validate(SupportedAuthMethods)
}

func TestRegistrationRequest_Validate(t *testing.T) {
	t.Parallel()

	t.Run("accepts a fully populated request", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:              "Acme MCP Client",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "client_secret_basic",
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("accepts a minimal request with optional fields absent", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "minimal",
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("rejects missing client_name", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_client_metadata", "client_name")
	})

	t.Run("rejects missing redirect_uris", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{ClientName: "named"}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_redirect_uri", "redirect_uris")
	})

	t.Run("rejects empty redirect_uris slice", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{ClientName: "named", RedirectURIs: []string{}}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_redirect_uri", "redirect_uris")
	})

	t.Run("rejects relative redirect URI", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"/callback"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_redirect_uri", "absolute URL")
	})

	t.Run("rejects URI missing scheme", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"app.acme.test/callback"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_redirect_uri", "absolute URL")
	})

	t.Run("rejects URI missing host", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https:///callback"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_redirect_uri", "must include a host")
	})

	t.Run("rejects javascript: redirect URI", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"javascript://example.com/%0Aalert(document.domain)//"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_redirect_uri", `scheme "javascript" is not permitted`)
	})

	t.Run("rejects data: redirect URI", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"data://example.com/text/html,<script>alert(1)</script>"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_redirect_uri", `scheme "data" is not permitted`)
	})

	t.Run("rejects file: redirect URI", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"file://example.com/etc/passwd"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_redirect_uri", `scheme "file" is not permitted`)
	})

	t.Run("rejects http on non-loopback host", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"http://app.acme.test/callback"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_redirect_uri", "loopback")
	})

	t.Run("accepts http://127.0.0.1 loopback", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"http://127.0.0.1:8765/callback"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("accepts http://localhost loopback", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"http://localhost:8765/callback"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("accepts reverse-DNS native-app custom scheme", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"com.acme.app:/oauth/callback"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("accepts single-token custom scheme", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"myapp:/callback"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("rejects unsupported grant_type", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https://app.acme.test/callback"},
			GrantTypes:   []string{"password"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_client_metadata", `unsupported grant_type "password"`)
	})

	t.Run("rejects unsupported response_type", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:    "named",
			RedirectURIs:  []string{"https://app.acme.test/callback"},
			ResponseTypes: []string{"token"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_client_metadata", `unsupported response_type "token"`)
	})

	t.Run("rejects unsupported token_endpoint_auth_method", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:              "named",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			TokenEndpointAuthMethod: "client_secret_jwt",
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_client_metadata", `unsupported token_endpoint_auth_method "client_secret_jwt"`)
	})

	t.Run("accepts public client (none) auth method", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:              "public",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			TokenEndpointAuthMethod: "none",
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("rejects refresh_token alone (no authorization_code)", func(t *testing.T) {
		t.Parallel()
		// bflad's RFC 7591 §2.1 example
		req := &RegistrationRequest{
			ClientName:    "drift",
			RedirectURIs:  []string{"https://app.acme.test/callback"},
			GrantTypes:    []string{"refresh_token"},
			ResponseTypes: []string{"code"},
		}
		assertOAuthError(t, validateAfterDefaults(req), "invalid_client_metadata", `response_type "code" requires grant_type "authorization_code"`)
	})

	t.Run("accepts authorization_code with refresh_token", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:    "paired",
			RedirectURIs:  []string{"https://app.acme.test/callback"},
			GrantTypes:    []string{"authorization_code", "refresh_token"},
			ResponseTypes: []string{"code"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})
}

func TestRegistrationRequest_SetDefaults(t *testing.T) {
	t.Parallel()

	t.Run("populates all defaults when absent", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		req.SetDefaults()
		assert.Equal(t, []string{"authorization_code"}, req.GrantTypes)
		assert.Equal(t, []string{"code"}, req.ResponseTypes)
		assert.Equal(t, "client_secret_basic", req.TokenEndpointAuthMethod)
	})

	t.Run("does not overwrite supplied values", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:              "named",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			GrantTypes:              []string{"refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "none",
		}
		req.SetDefaults()
		assert.Equal(t, []string{"refresh_token"}, req.GrantTypes)
		assert.Equal(t, []string{"code"}, req.ResponseTypes)
		assert.Equal(t, "none", req.TokenEndpointAuthMethod)
	})

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		req.SetDefaults()
		first := *req
		req.SetDefaults()
		assert.Equal(t, first, *req)
	})
}

// testPublicJWKS is a single-key public JWK Set holding a real, freshly
// generated ES256 key: the validators refuse a set with no usable signing
// key, and a hand-written coordinate pair is not one.
func testPublicJWKS(t *testing.T) json.RawMessage {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: key.Public(), KeyID: "k1", Algorithm: string(jose.ES256), Use: "sig"}}}
	body, err := json.Marshal(set)
	require.NoError(t, err)
	return body
}

// A private_key_jwt registration is accepted with exactly one key source and
// refused without one or with both. The jwks_uri is syntax-checked and never
// fetched: registration must not depend on a third-party host or hand an
// unauthenticated caller an outbound request.
func TestRegistrationRequest_PrivateKeyJWTKeySource(t *testing.T) {
	t.Parallel()

	base := func() *RegistrationRequest {
		return &RegistrationRequest{
			ClientName:              "asymmetric",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			TokenEndpointAuthMethod: "private_key_jwt",
		}
	}

	inline := base()
	inline.JWKS = testPublicJWKS(t)
	require.NoError(t, validateAfterDefaults(inline))

	remote := base()
	remote.JWKSURI = "https://keys.acme.test/jwks.json"
	require.NoError(t, validateAfterDefaults(remote))

	assertOAuthError(t, validateAfterDefaults(base()), "invalid_client_metadata", "requires jwks or jwks_uri")

	both := base()
	both.JWKS = testPublicJWKS(t)
	both.JWKSURI = "https://keys.acme.test/jwks.json"
	assertOAuthError(t, validateAfterDefaults(both), "invalid_client_metadata", "must not both be present")

	insecure := base()
	insecure.JWKSURI = "http://keys.acme.test/jwks.json"
	assertOAuthError(t, validateAfterDefaults(insecure), "invalid_client_metadata", "jwks_uri")
}

// A jwks that is present but holds no key set is not a key source: null is
// treated as absent, and a set with no keys array is malformed. Either would
// otherwise register a client that can never authenticate.
func TestRegistrationRequest_DegenerateJWKS(t *testing.T) {
	t.Parallel()

	withKeys := func(jwks string) *RegistrationRequest {
		return &RegistrationRequest{
			ClientName:              "asymmetric",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			TokenEndpointAuthMethod: "private_key_jwt",
			JWKS:                    json.RawMessage(jwks),
		}
	}

	assertOAuthError(t, validateAfterDefaults(withKeys(`null`)), "invalid_client_metadata", "requires jwks or jwks_uri")
	assertOAuthError(t, validateAfterDefaults(withKeys(`{}`)), "invalid_client_metadata", "not a valid JWK Set")
	assertOAuthError(t, validateAfterDefaults(withKeys(`{"keys":null}`)), "invalid_client_metadata", "not a valid JWK Set")

	// A well-formed set that the resolver would parse to nothing usable:
	// empty, or holding only an encryption key.
	assertOAuthError(t, validateAfterDefaults(withKeys(`{"keys":[]}`)), "invalid_client_metadata", "no usable signing key")
	assertOAuthError(t, validateAfterDefaults(withKeys(`{"keys":[{"kty":"EC","crv":"P-256","kid":"k1","x":"AA","y":"AA","use":"enc"}]}`)), "invalid_client_metadata", "no usable signing key")

	nullWithURI := withKeys(`null`)
	nullWithURI.JWKSURI = "https://keys.acme.test/jwks.json"
	require.NoError(t, validateAfterDefaults(nullWithURI), "a null jwks next to a jwks_uri is one key source, not two")
	require.Nil(t, nullWithURI.JWKS, "the null must be normalized away so nothing downstream persists it")
}

// A published key set carrying private or symmetric material would let anyone
// impersonate the client; the registration is refused, not just the key.
func TestRegistrationRequest_JWKSMaterialScreened(t *testing.T) {
	t.Parallel()

	withKeys := func(jwks string) *RegistrationRequest {
		return &RegistrationRequest{
			ClientName:              "asymmetric",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			TokenEndpointAuthMethod: "private_key_jwt",
			JWKS:                    json.RawMessage(jwks),
		}
	}

	assertOAuthError(t, validateAfterDefaults(withKeys(`{"keys":[{"kty":"EC","crv":"P-256","x":"AA","y":"AA","d":"secret"}]}`)), "invalid_client_metadata", "private key material")
	assertOAuthError(t, validateAfterDefaults(withKeys(`{"keys":[{"kty":"oct","k":"c2VjcmV0"}]}`)), "invalid_client_metadata", "symmetric key material")
	assertOAuthError(t, validateAfterDefaults(withKeys(`[]`)), "invalid_client_metadata", "not a valid JWK Set")
}

// TestRegistrationRequest_ValidateHonoursCallerAuthMethods pins the per-server
// method policy: Validate accepts exactly what its caller passed, so a method
// one authorization server supports is still refused by another that does not
// list it. Several servers share this request type, and a package-level list
// would let a method added for one start being accepted by all of them.
func TestRegistrationRequest_ValidateHonoursCallerAuthMethods(t *testing.T) {
	t.Parallel()

	req := &RegistrationRequest{
		ClientName:              "shared-request-type",
		RedirectURIs:            []string{"https://app.acme.test/callback"},
		TokenEndpointAuthMethod: "client_secret_basic",
	}
	req.SetDefaults()

	require.NoError(t, req.Validate([]string{"client_secret_basic"}))
	require.Contains(t, SupportedAuthMethods, "client_secret_basic",
		"the method under test must be one this server does support, or the rejection below proves nothing")
	assertOAuthError(t, req.Validate([]string{"none"}), "invalid_client_metadata", `unsupported token_endpoint_auth_method "client_secret_basic"`)
}

// assertOAuthError fails the test unless err unwraps to a
// *oauthwire.Error with the expected code and a description containing
// the expected substring.
func assertOAuthError(t *testing.T, err error, wantCode, wantDescriptionSubstr string) {
	t.Helper()
	require.Error(t, err)
	var oauthErr *oauthwire.Error
	require.ErrorAs(t, err, &oauthErr, "expected *oauthwire.Error, got %T (%v)", err, err)
	assert.Equal(t, wantCode, oauthErr.Code)
	assert.Contains(t, oauthErr.Description, wantDescriptionSubstr)
}

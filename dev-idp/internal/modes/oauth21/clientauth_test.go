package oauth21

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/ema"
)

// privateKeyJWTFixture is a mint setup whose app authenticates with
// private_key_jwt rather than a shared secret.
type privateKeyJWTFixture struct {
	*mintFixture
	key *rsa.PrivateKey
	kid string
}

func newPrivateKeyJWTFixture(t *testing.T) *privateKeyJWTFixture {
	t.Helper()

	h := newDBHandler(t)
	app, key, kid := h.seedAppWithJWKS(t, testAppClientID)
	user := h.seedUser(t, "pkjwt@devidptest.local")
	resource := h.seedResource(t, testResourceSlug, testResourceID)
	assignment := h.seedAssignment(t, app, user, resource, "chat.read chat.history")

	return &privateKeyJWTFixture{
		mintFixture: &mintFixture{
			dbHandler:  h,
			app:        app,
			user:       user,
			resource:   resource,
			idToken:    h.signIDToken(t, user),
			audience:   ema.ResourceASIssuer(testExternalURL, testResourceSlug),
			assignment: assignment,
		},
		key: key,
		kid: kid,
	}
}

// form builds a mint request authenticated with a valid client assertion.
func (f *privateKeyJWTFixture) formWithAssertion(t *testing.T, opts clientAssertionOpts) url.Values {
	t.Helper()
	v := f.form()
	v.Del("client_secret")
	v.Set("client_assertion_type", ema.ClientAssertionTypeJWTBearer)
	v.Set("client_assertion", signClientAssertion(t, opts))
	return v
}

func TestMintAcceptsPrivateKeyJWT(t *testing.T) {
	t.Parallel()

	f := newPrivateKeyJWTFixture(t)
	form := f.formWithAssertion(t, f.defaultClientAssertion(testAppClientID, f.key, f.kid))

	rec := f.mint(t, form)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var body idJAGResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, ema.TokenTypeIDJAG, body.IssuedTokenType)
}

// The identity is in the assertion's iss/sub, so RFC 7523 lets a client omit
// the client_id form parameter entirely.
func TestMintAcceptsPrivateKeyJWTWithoutClientIDParam(t *testing.T) {
	t.Parallel()

	f := newPrivateKeyJWTFixture(t)
	form := f.formWithAssertion(t, f.defaultClientAssertion(testAppClientID, f.key, f.kid))
	form.Del("client_id")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

// An app registered with a JWKS must not be able to fall back to the weaker
// method, even if it also has a secret on file.
func TestMintRejectsSecretWhenAppRegisteredAJWKS(t *testing.T) {
	t.Parallel()

	f := newPrivateKeyJWTFixture(t)
	form := f.form() // client_secret, no assertion

	rec := f.mint(t, form)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "private_key_jwt")
}

func TestMintRejectsAssertionSignedByTheWrongKey(t *testing.T) {
	t.Parallel()

	f := newPrivateKeyJWTFixture(t)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	opts := f.defaultClientAssertion(testAppClientID, other, f.kid)
	rec := f.mint(t, f.formWithAssertion(t, opts))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "did not verify")
}

// The audience check is what stops an assertion minted for another
// authorization server from authenticating the app here.
func TestMintRejectsAssertionForAnotherAudience(t *testing.T) {
	t.Parallel()

	f := newPrivateKeyJWTFixture(t)
	opts := f.defaultClientAssertion(testAppClientID, f.key, f.kid)
	opts.Audience = "https://some-other-idp.example/oauth2"

	rec := f.mint(t, f.formWithAssertion(t, opts))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "names neither this issuer")
}

// Deployments differ on whether `aud` is the issuer or the token endpoint;
// both are accepted, because rejecting one reads as a signing bug.
func TestMintAcceptsAssertionAudiencedToTheTokenEndpoint(t *testing.T) {
	t.Parallel()

	f := newPrivateKeyJWTFixture(t)
	opts := f.defaultClientAssertion(testAppClientID, f.key, f.kid)
	opts.Audience = testExternalURL + Prefix + "/token"

	rec := f.mint(t, f.formWithAssertion(t, opts))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestMintRejectsAssertionImpersonatingAnotherClient(t *testing.T) {
	t.Parallel()

	f := newPrivateKeyJWTFixture(t)
	// Correctly signed by this app's key, but claiming to be someone else.
	opts := f.defaultClientAssertion(testAppClientID, f.key, f.kid)
	opts.Subject = "some-other-app"

	form := f.formWithAssertion(t, opts)
	form.Set("client_id", testAppClientID)

	rec := f.mint(t, form)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "iss and sub")
}

func TestMintRejectsExpiredAssertion(t *testing.T) {
	t.Parallel()

	f := newPrivateKeyJWTFixture(t)
	opts := f.defaultClientAssertion(testAppClientID, f.key, f.kid)
	opts.Expires = time.Now().Add(-1 * time.Hour)

	rec := f.mint(t, f.formWithAssertion(t, opts))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "did not verify")
}

func TestMintRejectsWrongClientAssertionType(t *testing.T) {
	t.Parallel()

	f := newPrivateKeyJWTFixture(t)
	form := f.formWithAssertion(t, f.defaultClientAssertion(testAppClientID, f.key, f.kid))
	form.Set("client_assertion_type", "urn:example:something-else")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "private_key_jwt")
}

func TestASMetadataAdvertisesPrivateKeyJWT(t *testing.T) {
	t.Parallel()

	h := newDBHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	rec := httptest.NewRecorder()
	h.Handler.Handler().ServeHTTP(rec, req)

	var doc struct {
		TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Contains(t, doc.TokenEndpointAuthMethodsSupported, "private_key_jwt")
}

package resourceas

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/ema"
	"github.com/speakeasy-api/gram/dev-idp/internal/jwks"
)

func TestRedeemIssuesAnAudienceRestrictedToken(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	resp := h.redeem(t, h.signJAG(t, h.defaultJAG()), testClientID)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	body := decodeJSON[tokenResponse](t, resp)
	require.NotEmpty(t, body.AccessToken)
	require.Equal(t, "Bearer", body.TokenType)
	require.Equal(t, "chat.read chat.history", body.Scope)

	// The audience restriction is the property this profile turns on, so
	// assert it over the wire rather than in the database.
	form := url.Values{}
	form.Set("token", body.AccessToken)
	introspection := decodeJSON[introspectionResponse](t, h.postForm(t, "/introspect", form))

	require.True(t, introspection.Active)
	require.Equal(t, testResourceID, introspection.Aud, "the token must be bound to the MCP server, not the authorization server")
	require.Equal(t, h.audience(), introspection.Iss)
	require.Equal(t, testClientID, introspection.ClientID)
	require.Equal(t, h.user.ID.String(), introspection.Sub)
	require.Equal(t, "chat.read chat.history", introspection.Scope)
}

func TestRedeemNarrowsScopeToTheTrustRuleCeiling(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "chat.read")

	resp := h.redeem(t, h.signJAG(t, h.defaultJAG()), testClientID)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body := decodeJSON[tokenResponse](t, resp)
	require.Equal(t, "chat.read", body.Scope, "chat.history is above this resource's ceiling")
}

func TestRedeemRejectsReplay(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")
	assertion := h.signJAG(t, h.defaultJAG())

	require.Equal(t, http.StatusOK, h.redeem(t, assertion, testClientID).StatusCode)

	resp := h.redeem(t, assertion, testClientID)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeJSON[map[string]string](t, resp)
	require.Equal(t, "invalid_grant", body["error"])
	require.Contains(t, body["error_description"], "already been redeemed")
}

func TestRedeemRejectsUntrustedIssuer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	// No trust rule at all: deny by default.

	resp := h.redeem(t, h.signJAG(t, h.defaultJAG()), testClientID)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, decodeJSON[map[string]string](t, resp)["error_description"], "no trust rule")
}

func TestRedeemRejectsDisabledTrustRule(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	rule := h.trustLocalIssuer(t, "")
	_, err := h.queries.UpdateEmaTrustRule(t.Context(), repo.UpdateEmaTrustRuleParams{
		ID:               rule.ID,
		TrustedIssuer:    nullString(""),
		AllowedClientIds: nullString(""),
		AllowedScopes:    nullString(""),
		Enabled:          false,
		Ts:               time.Now(),
	})
	require.NoError(t, err)

	resp := h.redeem(t, h.signJAG(t, h.defaultJAG()), testClientID)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, decodeJSON[map[string]string](t, resp)["error_description"], "disabled")
}

func TestRedeemRejectsClientNotOnTheAllowlist(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustIssuer(t, h.localIssuer(), "", `["some-other-app"]`)

	resp := h.redeem(t, h.signJAG(t, h.defaultJAG()), testClientID)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, decodeJSON[map[string]string](t, resp)["error_description"], "allowlist")
}

func TestRedeemRejectsAssertionForAnotherAudience(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	opts := h.defaultJAG()
	opts.Audience = ema.ResourceASIssuer(h.baseURL, "some-other-resource")

	resp := h.redeem(t, h.signJAG(t, opts), testClientID)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, decodeJSON[map[string]string](t, resp)["error_description"], "does not name this authorization server")
}

func TestRedeemRejectsPlainIDTokenTyp(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	// Same issuer, same key, same claims -- only the typ header differs.
	// Without the typ check this would be indistinguishable from a grant.
	opts := h.defaultJAG()
	opts.Typ = "JWT"

	resp := h.redeem(t, h.signJAG(t, opts), testClientID)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, decodeJSON[map[string]string](t, resp)["error_description"], "typ header")
}

func TestRedeemRejectsExpiredAssertion(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	opts := h.defaultJAG()
	opts.IssuedAt = time.Now().Add(-2 * time.Hour)
	opts.Expires = time.Now().Add(-1 * time.Hour)

	resp := h.redeem(t, h.signJAG(t, opts), testClientID)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, decodeJSON[map[string]string](t, resp)["error_description"], "did not verify")
}

func TestRedeemRejectsAssertionSignedByTheWrongKey(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	// Claims say the local issuer, but a foreign key signed it.
	foreign := newForeignIDP(t)
	opts := h.defaultJAG()
	opts.Key = foreign.key
	opts.KID = foreign.kid

	resp := h.redeem(t, h.signJAG(t, opts), testClientID)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, decodeJSON[map[string]string](t, resp)["error_description"], "did not verify")
}

func TestRedeemRejectsMismatchedClientID(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	resp := h.redeem(t, h.signJAG(t, h.defaultJAG()), "a-different-app")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Equal(t, "invalid_client", decodeJSON[map[string]string](t, resp)["error"])
}

func TestRedeemRejectsResourceThatIsNotBehindThisServer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	opts := h.defaultJAG()
	opts.Resource = "https://mcp.elsewhere.example/"

	resp := h.redeem(t, h.signJAG(t, opts), testClientID)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, decodeJSON[map[string]string](t, resp)["error_description"], "not the resource behind")
}

func TestRedeemRejectsUnsupportedGrantType(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "irrelevant")

	resp := h.postForm(t, "/token", form)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, "unsupported_grant_type", decodeJSON[map[string]string](t, resp)["error"])
}

// TestRedeemAcceptsAForeignIssuer is the reason trust rules are data. The
// assertion is signed by an issuer this dev-idp holds no key for, so it can
// only be accepted by discovering that issuer's metadata and JWKS.
func TestRedeemAcceptsAForeignIssuer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	foreign := newForeignIDP(t)
	h.trustIssuer(t, foreign.issuer, "", "[]")

	opts := h.defaultJAG()
	opts.Issuer = foreign.issuer
	opts.Key = foreign.key
	opts.KID = foreign.kid
	// A foreign issuer's subject means nothing locally; email is what
	// identifies the person across the trust boundary.
	opts.Subject = "U019488227"
	opts.Email = "crossdomain@partner.example"

	resp := h.redeem(t, h.signJAG(t, opts), testClientID)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body := decodeJSON[tokenResponse](t, resp)
	form := url.Values{}
	form.Set("token", body.AccessToken)
	introspection := decodeJSON[introspectionResponse](t, h.postForm(t, "/introspect", form))

	require.True(t, introspection.Active)
	require.Equal(t, "crossdomain@partner.example", introspection.Username,
		"a subject from a foreign trust domain should be provisioned from the email claim")
}

func TestUnknownResourceSlugIsNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		ema.ResourceASIssuer(h.baseURL, "no-such-resource")+"/token", nil)
	require.NoError(t, err)

	require.Equal(t, http.StatusNotFound, h.do(t, req).StatusCode)
}

func TestASMetadataAdvertisesTheIDJAGProfile(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "chat.read chat.history")

	resp := h.get(t, h.baseURL+"/.well-known/oauth-authorization-server"+Prefix+"/"+testResourceSlug)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	doc := decodeJSON[asMetadata](t, resp)
	require.Equal(t, h.audience(), doc.Issuer)
	require.Contains(t, doc.GrantTypesSupported, ema.GrantTypeJWTBearer)
	require.Contains(t, doc.AuthorizationGrantProfilesSupported, ema.GrantProfileIDJAG,
		"this field is how an MCP client detects enterprise-managed authorization support")
	require.Equal(t, []string{"chat.history", "chat.read"}, doc.ScopesSupported)
}

func TestProtectedResourceMetadataPointsAtTheAuthorizationServer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	resp := h.get(t, h.baseURL+"/.well-known/oauth-protected-resource"+Prefix+"/"+testResourceSlug)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	doc := decodeJSON[protectedResourceMetadata](t, resp)
	require.Equal(t, testResourceID, doc.Resource)
	require.Equal(t, []string{h.audience()}, doc.AuthorizationServers)
}

func TestIntrospectRejectsATokenFromAnotherResource(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	body := decodeJSON[tokenResponse](t, h.redeem(t, h.signJAG(t, h.defaultJAG()), testClientID))

	// A second resource on the same dev-idp must not vouch for the first
	// resource's tokens.
	other, err := h.queries.CreateEmaResource(t.Context(), repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               "billing",
		Name:               "Billing",
		ResourceIdentifier: "https://mcp.billing.example/",
	})
	require.NoError(t, err)

	form := url.Values{}
	form.Set("token", body.AccessToken)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		ema.ResourceASIssuer(h.baseURL, other.Slug)+"/introspect", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp := h.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.False(t, decodeJSON[introspectionResponse](t, resp).Active)
}

// TestRedeemIssuesAVerifiableJWT is the point of issuing JWTs rather than
// opaque strings: a resource server can enforce the audience restriction
// itself, from the signature and the claims, without calling back here.
func TestRedeemIssuesAVerifiableJWT(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")

	body := decodeJSON[tokenResponse](t, h.redeem(t, h.signJAG(t, h.defaultJAG()), testClientID))
	require.NotEmpty(t, body.AccessToken)

	// Verify the way a third party would: fetch the JWKS the metadata
	// document advertises, then check the signature against it.
	meta := decodeJSON[asMetadata](t, h.get(t,
		h.baseURL+"/.well-known/oauth-authorization-server"+Prefix+"/"+testResourceSlug))
	require.NotEmpty(t, meta.JwksURI, "the metadata must advertise where the keys are")

	doc, err := jwks.Parse(h.get(t, meta.JwksURI).Body)
	require.NoError(t, err, "parse published jwks")

	var claims AccessTokenClaims
	token, err := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"})).ParseWithClaims(
		body.AccessToken, &claims,
		func(tok *jwt.Token) (any, error) {
			kid, _ := tok.Header["kid"].(string)
			return doc.FindRSA(kid)
		},
	)
	require.NoError(t, err, "verify access token against the published jwks")
	require.True(t, token.Valid)

	require.Equal(t, accessTokenJWTType, token.Header["typ"],
		"the typ header is what distinguishes an access token from the ID-JAG that bought it")
	require.Equal(t, jwt.ClaimStrings{testResourceID}, claims.Audience,
		"aud must be the MCP server, not the authorization server")
	require.Equal(t, h.audience(), claims.Issuer)
	require.Equal(t, h.user.ID.String(), claims.Subject)
	require.Equal(t, testClientID, claims.ClientID)
	require.Equal(t, "chat.read chat.history", claims.Scope)
	require.NotEmpty(t, claims.ID, "jti is required so the token can be revoked")
}

// A token minted for one resource must not verify against another, even
// though a single dev-idp signs both with the same key. The audience claim is
// what separates them.
func TestAccessTokenDoesNotVerifyForAnotherResource(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.trustLocalIssuer(t, "")
	body := decodeJSON[tokenResponse](t, h.redeem(t, h.signJAG(t, h.defaultJAG()), testClientID))

	other, err := h.queries.CreateEmaResource(t.Context(), repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               "billing",
		Name:               "Billing",
		ResourceIdentifier: "https://mcp.billing.example/",
	})
	require.NoError(t, err)

	_, err = h.parseAccessToken(&other, body.AccessToken)
	require.Error(t, err, "a chat token must not verify as a billing token")
}

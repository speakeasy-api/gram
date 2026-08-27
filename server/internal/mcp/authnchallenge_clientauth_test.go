package mcp_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// assertionSigner is a synthetic private_key_jwt client: the private key it
// mints assertions with, and the JWK Set publishing the public half.
type assertionSigner struct {
	jwks   []byte
	signer jose.Signer
}

func newAssertionSigner(t *testing.T) *assertionSigner {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	const kid = "client-key-1"

	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: key.Public(), KeyID: kid, Algorithm: string(jose.ES256), Use: "sig"}}}
	jwks, err := json.Marshal(set)
	require.NoError(t, err)

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), kid),
	)
	require.NoError(t, err)
	return &assertionSigner{jwks: jwks, signer: signer}
}

// assertion mints an RFC 7523 §2.2 client assertion for clientID naming aud.
func (s *assertionSigner) assertion(t *testing.T, clientID string, aud ...string) string {
	t.Helper()

	now := time.Now()
	raw, err := jwt.Signed(s.signer).Claims(jwt.Claims{
		Issuer:    clientID,
		Subject:   clientID,
		Audience:  jwt.Audience(aud),
		Expiry:    jwt.NewNumericDate(now.Add(5 * time.Minute)),
		NotBefore: jwt.NewNumericDate(now),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        "jti-" + uuid.NewString(),
	}).Serialize()
	require.NoError(t, err)
	return raw
}

// seedAssertionClient registers a private_key_jwt client publishing the
// signer's key set inline, the shape the DCR path will persist once it
// accepts the method.
func seedAssertionClient(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, s *assertionSigner) usersessions_repo.UserSessionClient {
	t.Helper()

	row, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                "client_" + uuid.NewString(),
		ClientSecretHash:        pgtype.Text{String: "", Valid: false},
		ClientName:              "assertion client",
		RedirectUris:            []string{"http://127.0.0.1:51423/callback"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{},
		TokenEndpointAuthMethod: "private_key_jwt",
		ClientJwks:              s.jwks,
		ClientJwksUri:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return row
}

// seedAuthorizationCode stores a redeemable grant for client and returns the
// code with the PKCE verifier that matches it.
func seedAuthorizationCode(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, client usersessions_repo.UserSessionClient) (string, string) {
	t.Helper()

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	code := "code-" + uuid.NewString()
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code:                        code,
		FlowID:                      "",
		UserSessionIssuerID:         toolset.UserSessionIssuerID.UUID,
		UserSessionClientID:         client.ID,
		ClientID:                    client.ClientID,
		RedirectURI:                 client.RedirectUris[0],
		CodeChallenge:               base64.RawURLEncoding.EncodeToString(sum[:]),
		CodeChallengeMethod:         "S256",
		Subject:                     urn.NewUserSubject("assertion-user-" + uuid.NewString()),
		DesiredSessionDurationHours: 0,
		ToolSelection:               nil,
		CreatedAt:                   time.Now(),
	}))
	return code, verifier
}

// postForm drives an issuer-gated OAuth endpoint with a form body.
func postForm(t *testing.T, ti *testInstance, mcpSlug, endpoint string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/"+endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	switch endpoint {
	case "token":
		require.NoError(t, ti.service.HandleToken(w, req))
	case "revoke":
		require.NoError(t, ti.service.HandleRevoke(w, req))
	default:
		t.Fatalf("unsupported endpoint %q", endpoint)
	}
	return w
}

// codeGrantForm is the authorization_code exchange every test below starts
// from; callers add the client authentication they are exercising.
func codeGrantForm(client usersessions_repo.UserSessionClient, code, verifier string) url.Values {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", client.RedirectUris[0])
	form.Set("client_id", client.ClientID)
	form.Set("code_verifier", verifier)
	return form
}

// withAssertion adds RFC 7523 §2.2 client authentication to a form.
func withAssertion(form url.Values, assertion string) url.Values {
	form.Set("client_assertion_type", clientauth.AssertionType)
	form.Set("client_assertion", assertion)
	return form
}

// requireInvalidClient asserts the single response every client
// authentication failure produces.
func requireInvalidClient(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_client")
}

// The load-bearing property: a client whose stored method is private_key_jwt
// authenticates with a valid assertion and with nothing else.
func TestHandleToken_PrivateKeyJWT_AuthorizationCodeGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	// No assertion: the row committed to one, so the request is refused
	// however valid its code and PKCE are.
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))
	requireInvalidClient(t, w)

	// A secret alongside the assertion is a client that does not match its
	// registration.
	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, client)
	form := withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, advertisedIssuer))
	form.Set("client_secret", "not-a-thing-this-client-has")
	w = postForm(t, ti, toolset.McpSlug.String, "token", form)
	requireInvalidClient(t, w)

	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, client)
	w = postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, advertisedIssuer)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")
}

// A key the client never published cannot authenticate it, which is what
// resolving keys from the row's own key set is for.
func TestHandleToken_PrivateKeyJWT_ForeignKeyRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, newAssertionSigner(t))
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	impostor := newAssertionSigner(t)
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	w := postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(client, code, verifier), impostor.assertion(t, client.ClientID, advertisedIssuer)))
	requireInvalidClient(t, w)
}

// Both accepted audience forms authenticate, and one naming a different MCP
// server does not: the accepted values are derived from the endpoint the
// request addressed, so an assertion minted for a neighbour never verifies.
func TestHandleToken_PrivateKeyJWT_Audiences(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	w := postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, advertisedIssuer+"/token")))
	require.Equal(t, http.StatusOK, w.Code, "the token endpoint URL is an accepted audience: %s", w.Body.String())

	// A sibling endpoint's URL is not this endpoint's audience: an assertion
	// addressed to /revoke does not authenticate a token request.
	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, client)
	w = postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, advertisedIssuer+"/revoke")))
	requireInvalidClient(t, w)

	// A real neighbour: a second MCP server on the same deployment, with
	// its own advertised issuer. An assertion minted for it must not verify
	// here, which a global or deployment-wide audience allowlist would let
	// through and a fabricated string would not catch.
	neighbour, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	neighbourIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, neighbour.McpSlug.String)
	require.NotEqual(t, advertisedIssuer, neighbourIssuer)
	for _, foreign := range []string{neighbourIssuer, neighbourIssuer + "/token"} {
		code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, client)
		w = postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, foreign)))
		requireInvalidClient(t, w)
	}
}

// RFC 7521 §4.2: client_id may be omitted when an assertion is present, the
// assertion's sub selecting the client. The signature check against that
// row's own key set is what makes the selection safe.
func TestHandleToken_PrivateKeyJWT_OmittedClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	form := withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, advertisedIssuer))
	form.Del("client_id")
	w := postForm(t, ti, toolset.McpSlug.String, "token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// An assertion whose sub names a client this key does not belong to
	// selects that row and then fails its signature check.
	_, _, publicClient := seedPrivateToolsetWithIssuer(t, ctx, ti)
	form = withAssertion(url.Values{}, signer.assertion(t, publicClient.ClientID, advertisedIssuer))
	form.Set("grant_type", "authorization_code")
	w = postForm(t, ti, toolset.McpSlug.String, "token", form)
	requireInvalidClient(t, w)
}

// The refresh_token grant is client-authenticated like any other token
// request; the assertion rule is enforced once before grant dispatch, so
// both grants share it by construction.
func TestHandleToken_PrivateKeyJWT_RefreshGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	refreshToken := seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, nil)

	w := postRefreshGrant(t, ti, toolset.McpSlug.String, client.ClientID, refreshToken, "")
	requireInvalidClient(t, w)

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", client.ClientID)
	w = postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(form, signer.assertion(t, client.ClientID, advertisedIssuer)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")
}

// Revocation is client-authenticated by the same rule, and shares the replay
// keyspace with the token endpoint: an assertion spent at /token cannot be
// re-presented at /revoke, and an unauthenticated caller cannot revoke an
// assertion client's tokens by naming its client_id.
func TestHandleRevoke_PrivateKeyJWT(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	refreshToken := seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, nil)

	revokeForm := func() url.Values {
		form := url.Values{}
		form.Set("token", refreshToken)
		form.Set("token_type_hint", "refresh_token")
		form.Set("client_id", client.ClientID)
		return form
	}

	w := postForm(t, ti, toolset.McpSlug.String, "revoke", revokeForm())
	requireInvalidClient(t, w)

	// Spend an assertion at /token, then replay it at /revoke.
	spent := signer.assertion(t, client.ClientID, advertisedIssuer)
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	w = postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(client, code, verifier), spent))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = postForm(t, ti, toolset.McpSlug.String, "revoke", withAssertion(revokeForm(), spent))
	requireInvalidClient(t, w)

	// The revocation endpoint's own URL and the issuer are accepted at
	// /revoke; the token endpoint's URL is not, since it names a different
	// endpoint than the one posted to.
	w = postForm(t, ti, toolset.McpSlug.String, "revoke", withAssertion(revokeForm(), signer.assertion(t, client.ClientID, advertisedIssuer+"/token")))
	requireInvalidClient(t, w)
	w = postForm(t, ti, toolset.McpSlug.String, "revoke", withAssertion(revokeForm(), signer.assertion(t, client.ClientID, advertisedIssuer)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = postForm(t, ti, toolset.McpSlug.String, "revoke", withAssertion(revokeForm(), signer.assertion(t, client.ClientID, advertisedIssuer+"/revoke")))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A public client presenting any credential is refused: an unrequested
// credential is not an upgrade, it is a client that does not match its
// registration, and a stored-none row has no key source to verify an
// assertion against anyway.
func TestHandleToken_PublicClientPresentingCredentialsRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)
	signer := newAssertionSigner(t)

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	w := postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, advertisedIssuer)))
	requireInvalidClient(t, w)

	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, client)
	form := codeGrantForm(client, code, verifier)
	form.Set("client_secret", "unexpected")
	w = postForm(t, ti, toolset.McpSlug.String, "token", form)
	requireInvalidClient(t, w)

	// And nothing presented still works, so the tightening did not break
	// the ordinary public client.
	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, client)
	w = postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// Rows that predate the method column keep authenticating exactly as they
// did: a stored secret means the secret is required, otherwise the client is
// public. This is the whole production population at the time the column
// landed.
func TestHandleToken_LegacyNullMethodRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, publicClient := seedPrivateToolsetWithIssuer(t, ctx, ti)
	queries := usersessions_repo.New(ti.conn)

	legacyPublic, err := queries.ClearUserSessionClientAuthMethod(ctx, publicClient.ID)
	require.NoError(t, err)
	require.False(t, legacyPublic.TokenEndpointAuthMethod.Valid)

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, legacyPublic)
	w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(legacyPublic, code, verifier))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	const secret = "s3cret"
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
	require.NoError(t, err)
	hashed := string(hashedBytes)
	confidential, err := queries.CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "client_" + uuid.NewString(),
		ClientSecretHash:        pgtype.Text{String: hashed, Valid: true},
		ClientName:              "legacy confidential",
		RedirectUris:            []string{"http://127.0.0.1:51423/callback"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{},
		TokenEndpointAuthMethod: "client_secret_post",
		ClientJwks:              nil,
		ClientJwksUri:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	legacyConfidential, err := queries.ClearUserSessionClientAuthMethod(ctx, confidential.ID)
	require.NoError(t, err)

	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, legacyConfidential)
	w = postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(legacyConfidential, code, verifier))
	requireInvalidClient(t, w)

	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, legacyConfidential)
	form := codeGrantForm(legacyConfidential, code, verifier)
	form.Set("client_secret", secret)
	w = postForm(t, ti, toolset.McpSlug.String, "token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// Revocation applies the legacy NULL derivation the same way the token
// endpoint does: a public legacy row revokes with nothing, a secret-bearing
// one only with its secret.
func TestHandleRevoke_LegacyNullMethodRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, publicClient := seedPrivateToolsetWithIssuer(t, ctx, ti)
	queries := usersessions_repo.New(ti.conn)

	legacyPublic, err := queries.ClearUserSessionClientAuthMethod(ctx, publicClient.ID)
	require.NoError(t, err)
	publicToken := seedUserSessionWithSelection(t, ctx, ti, issuer.ID, legacyPublic.ID, nil)

	form := url.Values{}
	form.Set("token", publicToken)
	form.Set("token_type_hint", "refresh_token")
	form.Set("client_id", legacyPublic.ClientID)
	w := postForm(t, ti, toolset.McpSlug.String, "revoke", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	const secret = "s3cret"
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
	require.NoError(t, err)
	confidential, err := queries.CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "client_" + uuid.NewString(),
		ClientSecretHash:        pgtype.Text{String: string(hashedBytes), Valid: true},
		ClientName:              "legacy confidential",
		RedirectUris:            []string{"http://127.0.0.1:51423/callback"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{},
		TokenEndpointAuthMethod: "client_secret_post",
		ClientJwks:              nil,
		ClientJwksUri:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	legacyConfidential, err := queries.ClearUserSessionClientAuthMethod(ctx, confidential.ID)
	require.NoError(t, err)
	confidentialToken := seedUserSessionWithSelection(t, ctx, ti, issuer.ID, legacyConfidential.ID, nil)

	form = url.Values{}
	form.Set("token", confidentialToken)
	form.Set("token_type_hint", "refresh_token")
	form.Set("client_id", legacyConfidential.ClientID)
	w = postForm(t, ti, toolset.McpSlug.String, "revoke", form)
	requireInvalidClient(t, w)

	form.Set("client_secret", secret)
	w = postForm(t, ti, toolset.McpSlug.String, "revoke", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// HTTP Basic credentials alongside an assertion are refused at the handler:
// the extractor labels the mix, and the assertion client's rule rejects any
// presented secret.
func TestHandleToken_PrivateKeyJWT_BasicAlongsideAssertionRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	client := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	// With a password, and with an empty one: an empty Basic password is
	// still a second authentication method on the request (RFC 6749
	// §2.3), and it must not slip past a check that only looks at whether
	// a secret value was supplied.
	for _, password := range []string{"a-secret-this-client-does-not-have", ""} {
		code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
		form := withAssertion(codeGrantForm(client, code, verifier), signer.assertion(t, client.ClientID, advertisedIssuer))
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(client.ClientID, password)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleToken(w, req))
		requireInvalidClient(t, w)
	}
}

// Every client authentication failure answers with one body, on both
// client-authenticated endpoints. A CIMD client_id is a URL, so a response
// that distinguished "no such client" from "bad credential" would tell an
// unauthenticated caller which vendors' clients an issuer has seen. The
// distinctions live in the log reasons, not on the wire.
func TestClientAuthFailures_IndistinguishableOnTheWire(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, publicClient := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	assertionClient := seedAssertionClient(t, ctx, ti, issuer.ID, signer)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	require.NoError(t, err)
	secretClient, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "client_" + uuid.NewString(),
		ClientSecretHash:        pgtype.Text{String: string(hashedBytes), Valid: true},
		ClientName:              "confidential",
		RedirectUris:            []string{"http://127.0.0.1:51423/callback"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{},
		TokenEndpointAuthMethod: "client_secret_post",
		ClientJwks:              nil,
		ClientJwksUri:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	// Each form fails client authentication for a different reason.
	impostor := newAssertionSigner(t)
	failures := map[string]url.Values{
		"unregistered client":            {"client_id": {"client_" + uuid.NewString()}},
		"wrong secret":                   {"client_id": {secretClient.ClientID}, "client_secret": {"wrong"}},
		"public client with a secret":    {"client_id": {publicClient.ClientID}, "client_secret": {"unexpected"}},
		"assertion client, no assertion": {"client_id": {assertionClient.ClientID}},
		"assertion from a foreign key":   withAssertion(url.Values{"client_id": {assertionClient.ClientID}}, impostor.assertion(t, assertionClient.ClientID, advertisedIssuer)),
	}

	for _, endpoint := range []string{"token", "revoke"} {
		var canonical string
		for name, form := range failures {
			form = maps.Clone(form)
			form.Set("grant_type", "authorization_code")
			form.Set("token", "irrelevant")
			w := postForm(t, ti, toolset.McpSlug.String, endpoint, form)
			require.Equal(t, http.StatusUnauthorized, w.Code, "%s: %s", endpoint, name)
			if canonical == "" {
				canonical = w.Body.String()
				require.Contains(t, canonical, "invalid_client")
				continue
			}
			require.Equal(t, canonical, w.Body.String(), "%s: %q must be indistinguishable from the other failures", endpoint, name)
		}
	}
}

// A stored method this code does not recognize fails closed rather than
// degrading to a public client, which would be the downgrade the column
// exists to prevent.
func TestHandleToken_UnrecognizedStoredMethodFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	client, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "client_" + uuid.NewString(),
		ClientSecretHash:        pgtype.Text{String: "", Valid: false},
		ClientName:              "future method",
		RedirectUris:            []string{"http://127.0.0.1:51423/callback"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{},
		TokenEndpointAuthMethod: "tls_client_auth",
		ClientJwks:              nil,
		ClientJwksUri:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))
	requireInvalidClient(t, w)
}

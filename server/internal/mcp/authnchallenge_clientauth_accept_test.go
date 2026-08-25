package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// registerJSON drives /register and decodes the response.
func registerJSON(t *testing.T, ti *testInstance, slug, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleRegister(w, newRegisterRequest(t, slug, body)))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded), w.Body.String())
	return w, decoded
}

// A dynamically registered private_key_jwt client gets no secret, has its key
// source echoed and persisted, and then authenticates with an assertion.
func TestHandleRegister_PrivateKeyJWT(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	body := `{"client_name":"asymmetric","redirect_uris":["http://127.0.0.1:51423/callback"],"token_endpoint_auth_method":"private_key_jwt","jwks":` + string(signer.jwks) + `}`
	w, resp := registerJSON(t, ti, toolset.McpSlug.String, body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "private_key_jwt", resp["token_endpoint_auth_method"])
	require.NotContains(t, resp, "client_secret", "an asymmetric client must never be handed a secret")
	require.NotContains(t, resp, "client_secret_expires_at")
	require.Contains(t, resp, "jwks", "RFC 7591 §3.2.1: the response reflects the registered key source")

	clientID, ok := resp["client_id"].(string)
	require.True(t, ok)
	row, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: issuer.ID,
		ClientID:            clientID,
	})
	require.NoError(t, err)
	require.False(t, row.ClientSecretHash.Valid)
	require.Equal(t, "private_key_jwt", row.TokenEndpointAuthMethod.String)
	require.JSONEq(t, string(signer.jwks), string(row.ClientJwks))
	require.False(t, row.ClientJwksUri.Valid)

	// The registered client authenticates with an assertion, and only with
	// one.
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, row)
	tw := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(row, code, verifier))
	requireInvalidClient(t, tw)

	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, row)
	tw = postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(row, code, verifier), signer.assertion(t, row.ClientID, advertisedIssuer)))
	require.Equal(t, http.StatusOK, tw.Code, tw.Body.String())
}

// A remote key source is persisted as such, and the jwks_uri is never fetched
// at registration.
func TestHandleRegister_PrivateKeyJWTRemoteKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	// A host that does not exist: registration must succeed anyway.
	body := `{"client_name":"remote-keys","redirect_uris":["http://127.0.0.1:51423/callback"],"token_endpoint_auth_method":"private_key_jwt","jwks_uri":"https://keys.invalid.example/jwks.json"}`
	w, resp := registerJSON(t, ti, toolset.McpSlug.String, body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "https://keys.invalid.example/jwks.json", resp["jwks_uri"])
	require.NotContains(t, resp, "jwks")

	clientID, ok := resp["client_id"].(string)
	require.True(t, ok)
	row, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: issuer.ID,
		ClientID:            clientID,
	})
	require.NoError(t, err)
	require.Equal(t, "https://keys.invalid.example/jwks.json", row.ClientJwksUri.String)
	require.Empty(t, row.ClientJwks)
}

// The registration rules for an asymmetric client, each producing the RFC
// 7591 §3.2.2 error rather than a stored row.
func TestHandleRegister_PrivateKeyJWTRejections(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	signer := newAssertionSigner(t)

	for name, body := range map[string]string{
		"no key source":    `{"client_name":"x","redirect_uris":["http://127.0.0.1:51423/callback"],"token_endpoint_auth_method":"private_key_jwt"}`,
		"both key sources": `{"client_name":"x","redirect_uris":["http://127.0.0.1:51423/callback"],"token_endpoint_auth_method":"private_key_jwt","jwks":` + string(signer.jwks) + `,"jwks_uri":"https://keys.example.com/jwks.json"}`,
		"http jwks_uri":    `{"client_name":"x","redirect_uris":["http://127.0.0.1:51423/callback"],"token_endpoint_auth_method":"private_key_jwt","jwks_uri":"http://keys.example.com/jwks.json"}`,
		"private material": `{"client_name":"x","redirect_uris":["http://127.0.0.1:51423/callback"],"token_endpoint_auth_method":"private_key_jwt","jwks":{"keys":[{"kty":"EC","crv":"P-256","x":"AA","y":"AA","d":"secret"}]}}`,
	} {
		w, resp := registerJSON(t, ti, toolset.McpSlug.String, body)
		require.Equal(t, http.StatusBadRequest, w.Code, "%s: %s", name, w.Body.String())
		require.Equal(t, "invalid_client_metadata", resp["error"], name)
	}
}

// Symmetric registrations are untouched by the new arm: they still receive a
// secret, and nothing else does.
func TestHandleRegister_SecretMintingByMethod(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	_, confidential := registerJSON(t, ti, toolset.McpSlug.String, `{"client_name":"c","redirect_uris":["http://127.0.0.1:51423/callback"],"token_endpoint_auth_method":"client_secret_post"}`)
	require.NotEmpty(t, confidential["client_secret"])
	require.Contains(t, confidential, "client_secret_expires_at")

	_, public := registerJSON(t, ti, toolset.McpSlug.String, dcrPublicClientBody)
	require.NotContains(t, public, "client_secret")
}

// A CIMD document declaring private_key_jwt with an inline key set resolves
// to a row the token endpoint holds to an assertion.
func TestOAuthCIMD_PrivateKeyJWTInlineKeys(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	signer := newAssertionSigner(t)
	var keys map[string]any
	require.NoError(t, json.Unmarshal(signer.jwks, &keys))
	ds.set(t, func(ds *cimdDocServer) {
		ds.doc["token_endpoint_auth_method"] = "private_key_jwt"
		ds.doc["jwks"] = keys
	})
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())

	row, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            ds.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, "private_key_jwt", row.TokenEndpointAuthMethod.String)
	require.JSONEq(t, string(signer.jwks), string(row.ClientJwks))
	require.False(t, row.ClientSecretHash.Valid)

	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, row)
	tw := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(row, code, verifier))
	requireInvalidClient(t, tw)

	code, verifier = seedAuthorizationCode(t, ctx, ti, toolset, row)
	tw = postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(row, code, verifier), signer.assertion(t, row.ClientID, advertisedIssuer)))
	require.Equal(t, http.StatusOK, tw.Code, tw.Body.String())
}

// A CIMD document naming a jwks_uri has its key set fetched at the token
// endpoint, once, through the same guardian-policied client the document
// itself came through; a warm key cache costs no further request.
func TestOAuthCIMD_PrivateKeyJWTRemoteKeys(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	signer := newAssertionSigner(t)
	ds.set(t, func(ds *cimdDocServer) {
		ds.doc["token_endpoint_auth_method"] = "private_key_jwt"
		ds.doc["jwks_uri"] = ds.jwksURI()
		ds.jwks = signer.jwks
	})
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, "http://127.0.0.1:33418/callback", pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	require.Equal(t, int64(0), ds.jwksRequests.Load(), "resolving the document must not fetch the key set")

	row, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            ds.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, ds.jwksURI(), row.ClientJwksUri.String)
	require.Empty(t, row.ClientJwks)

	for range 2 {
		code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, row)
		tw := postForm(t, ti, toolset.McpSlug.String, "token", withAssertion(codeGrantForm(row, code, verifier), signer.assertion(t, row.ClientID, advertisedIssuer)))
		require.Equal(t, http.StatusOK, tw.Code, tw.Body.String())
	}
	require.Equal(t, int64(1), ds.jwksRequests.Load(), "one cold fetch, then the key cache serves")
}

// A document that drops private_key_jwt after committing to it is refused at
// refresh time and the stored method stands: the downgrade is
// indistinguishable from a domain takeover. A public client upgrading to
// private_key_jwt is the change the column exists to record, and goes
// through.
func TestOAuthCIMD_AuthMethodDowngradeRefused(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	setIssuerAdmissionMode(t, ctx, ti, toolset, admission.ModeOpen)
	signer := newAssertionSigner(t)
	var keys map[string]any
	require.NoError(t, json.Unmarshal(signer.jwks, &keys))
	queries := usersessions_repo.New(ti.conn)
	redirectURI := "http://127.0.0.1:33418/callback"

	// Upgrade: public first, then asymmetric.
	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	row, err := queries.GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: toolset.UserSessionIssuerID.UUID, ClientID: ds.clientID})
	require.NoError(t, err)
	require.Equal(t, "none", row.TokenEndpointAuthMethod.String)

	ds.set(t, func(ds *cimdDocServer) {
		ds.doc["token_endpoint_auth_method"] = "private_key_jwt"
		ds.doc["jwks"] = keys
	})
	lapseCIMDCache(t, ctx, ti, row)
	w = doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code, "upgrading to private_key_jwt is accepted: %s", w.Body.String())
	row, err = queries.GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: toolset.UserSessionIssuerID.UUID, ClientID: ds.clientID})
	require.NoError(t, err)
	require.Equal(t, "private_key_jwt", row.TokenEndpointAuthMethod.String)

	// Downgrade: refused, and nothing about the row moves.
	ds.set(t, func(ds *cimdDocServer) {
		ds.doc["token_endpoint_auth_method"] = "none"
		delete(ds.doc, "jwks")
		ds.doc["client_name"] = "Renamed During Downgrade"
	})
	lapseCIMDCache(t, ctx, ti, row)
	w = doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	requireAuthorizeOAuthError(t, w, http.StatusBadRequest, "invalid_client_metadata")

	after, err := queries.GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: toolset.UserSessionIssuerID.UUID, ClientID: ds.clientID})
	require.NoError(t, err)
	require.Equal(t, "private_key_jwt", after.TokenEndpointAuthMethod.String, "the stored method must survive a downgrade attempt")
	require.JSONEq(t, string(signer.jwks), string(after.ClientJwks), "the stored key set must survive too")
	require.NotEqual(t, "Renamed During Downgrade", after.ClientName, "a refused refresh must not persist any of the new document")
}

// A CIMD row that predates the method column refreshes normally. This is every
// production CIMD client at the time the column landed, and the downgrade
// guard on the upsert must read its NULL as "not committed to private_key_jwt"
// rather than as unknown: a guard written as a bare comparison evaluates to
// NULL for such a row, updates nothing, and turns the client into "unknown
// client_id" on its first document refresh.
func TestOAuthCIMD_LegacyNullMethodRowRefreshes(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, toolset := newTestCIMDService(t)
	queries := usersessions_repo.New(ti.conn)
	redirectURI := "http://127.0.0.1:33418/callback"
	lookup := usersessions_repo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: toolset.UserSessionIssuerID.UUID, ClientID: ds.clientID}

	w := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code)
	row, err := queries.GetUserSessionClientByClientID(ctx, lookup)
	require.NoError(t, err)

	legacy, err := queries.ClearUserSessionClientAuthMethod(ctx, row.ID)
	require.NoError(t, err)
	require.False(t, legacy.TokenEndpointAuthMethod.Valid)
	lapseCIMDCache(t, ctx, ti, legacy)

	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_name"] = "Refreshed Legacy Client" })
	w = doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, redirectURI, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusFound, w.Code, "a legacy row must refresh, not vanish: %s", w.Body.String())

	after, err := queries.GetUserSessionClientByClientID(ctx, lookup)
	require.NoError(t, err)
	require.Equal(t, "Refreshed Legacy Client", after.ClientName, "the refresh must actually have been written")
	require.Equal(t, "none", after.TokenEndpointAuthMethod.String, "the refresh stamps the method the document declares")
}

// The metadata document advertises the method and the algorithms an
// assertion may use, so clients can select it from the list rather than guess.
func TestHandleGetAuthorizationServer_AdvertisesPrivateKeyJWT(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+toolset.McpSlug.String, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleGetAuthorizationServer(w, req))
	require.Equal(t, http.StatusOK, w.Code)

	var meta struct {
		Methods []string `json:"token_endpoint_auth_methods_supported"`
		Algs    []string `json:"token_endpoint_auth_signing_alg_values_supported"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	require.Contains(t, meta.Methods, "private_key_jwt")
	require.Contains(t, meta.Algs, "RS256")
	require.Contains(t, meta.Algs, "ES256")
	require.NotContains(t, meta.Algs, "HS256")
	require.NotContains(t, meta.Algs, "none")
}

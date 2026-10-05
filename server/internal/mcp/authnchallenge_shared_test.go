package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const sharedTestRedirectURI = "http://localhost:3000/callback"

// sharedIssuerFixture is a user session issuer in shared mode with a
// registered public client and two public MCP servers attached to it.
type sharedIssuerFixture struct {
	issuer    usersessions_repo.UserSessionIssuer
	client    usersessions_repo.UserSessionClient
	slugA     string
	slugB     string
	issuerURL string
}

func (f sharedIssuerFixture) resource(ti *testInstance, slug string) string {
	return ti.serverURL.JoinPath("mcp", slug).String()
}

// seedSharedIssuer creates an issuer with two public toolset-backed MCP
// servers and switches it to shared mode with no pinned issuer.
func seedSharedIssuer(t *testing.T, ctx context.Context, ti *testInstance) sharedIssuerFixture {
	t.Helper()

	authCtx := requireProjectAuthContext(t, ctx)
	issuer, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		OrganizationID:     conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:               "shared-usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)

	client, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "shared-client-" + uuid.NewString()[:8],
		ClientName:              "shared test client",
		RedirectUris:            []string{sharedTestRedirectURI},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)

	slugs := make([]string, 0, 2)
	for range 2 {
		slugs = append(slugs, seedPublicServerOnIssuer(t, ctx, ti, issuer.ID))
	}
	setIssuerMode(t, ctx, ti, issuer.ID, "shared", "")

	return sharedIssuerFixture{
		issuer:    issuer,
		client:    client,
		slugA:     slugs[0],
		slugB:     slugs[1],
		issuerURL: ti.serverURL.String() + "/oauth/usi/" + issuer.ID.String(),
	}
}

// seedPublicServerOnIssuer creates a public toolset-backed MCP server attached
// to issuerID and returns its slug.
func seedPublicServerOnIssuer(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) string {
	t.Helper()

	authCtx := requireProjectAuthContext(t, ctx)
	slug := "shared-mcp-" + uuid.NewString()[:8]
	toolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Shared MCP " + slug,
		Slug:                   slug,
		Description:            conv.ToPGText("MCP server behind a shared authorization server"),
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)
	createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuerID)
	return slug
}

func setIssuerMode(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, mode, pinnedIssuerURL string) {
	t.Helper()

	rows, err := testrepo.New(ti.conn).SetUserSessionIssuerAuthorizationServerModeFixture(ctx, testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{
		AuthorizationServerMode: mode,
		PinnedIssuerUrl:         conv.ToPGTextEmpty(pinnedIssuerURL),
		ID:                      issuerID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

func sharedRequest(ctx context.Context, method, target string, issuerID string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("issuerID", issuerID)
	return req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
}

func sharedAuthorize(t *testing.T, ctx context.Context, ti *testInstance, f sharedIssuerFixture, challenge string, resources ...string) *httptest.ResponseRecorder {
	t.Helper()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {f.client.ClientID},
		"redirect_uri":          {sharedTestRedirectURI},
		"state":                 {"client-state"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	for _, resource := range resources {
		q.Add("resource", resource)
	}
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedAuthorize(w, sharedRequest(ctx, http.MethodGet, "/oauth/usi/"+f.issuer.ID.String()+"/authorize?"+q.Encode(), f.issuer.ID.String(), nil)))
	return w
}

// sharedAuthorizationCode runs authorize and consent on the shared
// authorization server for resource and returns the code and the client
// redirect's iss.
func sharedAuthorizationCode(t *testing.T, ctx context.Context, ti *testInstance, f sharedIssuerFixture, verifier, resource string) (string, string) {
	t.Helper()

	authorized := sharedAuthorize(t, ctx, ti, f, pkceChallenge(verifier), resource)
	require.Equal(t, http.StatusFound, authorized.Code, authorized.Body.String())
	consentURL, err := url.Parse(authorized.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, f.issuerURL+"/connect", consentURL.Scheme+"://"+consentURL.Host+consentURL.Path, "consent is a page of the shared authorization server")
	stateID := consentURL.Query().Get("state")

	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	require.True(t, stored.Endpoint.SharedAuthorizationServer)

	form := url.Values{"state": {stateID}, "csrf_token": {stored.CSRFToken}, "action": {"approve"}}
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedConsent(w, sharedRequest(ctx, http.MethodPost, "/oauth/usi/"+f.issuer.ID.String()+"/connect", f.issuer.ID.String(), strings.NewReader(form.Encode()))))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	redirect, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := redirect.Query().Get("code")
	require.NotEmpty(t, code, "consent must redirect with a code: %s", redirect.String())
	return code, redirect.Query().Get("iss")
}

func sharedToken(t *testing.T, ctx context.Context, ti *testInstance, issuerID string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedToken(w, sharedRequest(ctx, http.MethodPost, "/oauth/usi/"+issuerID+"/token", issuerID, strings.NewReader(form.Encode()))))
	return w
}

type sharedTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
}

func decodeSharedTokenResponse(t *testing.T, w *httptest.ResponseRecorder) sharedTokenResponse {
	t.Helper()

	var resp sharedTokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return resp
}

func unverifiedClaims(t *testing.T, token string) jwt.MapClaims {
	t.Helper()

	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(token, claims)
	require.NoError(t, err)
	return claims
}

func TestSharedAuthorizationServer_ServesMetadataAtBothLocations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	for _, target := range []string{
		"/.well-known/oauth-authorization-server/oauth/usi/" + f.issuer.ID.String(),
		"/oauth/usi/" + f.issuer.ID.String() + "/.well-known/oauth-authorization-server",
	} {
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleSharedAuthorizationServerMetadata(w, sharedRequest(ctx, http.MethodGet, target, f.issuer.ID.String(), nil)))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var meta map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
		require.Equal(t, f.issuerURL, meta["issuer"])
		require.Equal(t, f.issuerURL+"/authorize", meta["authorization_endpoint"])
		require.Equal(t, f.issuerURL+"/token", meta["token_endpoint"])
		require.Equal(t, f.issuerURL+"/register", meta["registration_endpoint"])
		require.Equal(t, f.issuerURL+"/revoke", meta["revocation_endpoint"])
		require.ElementsMatch(t, []any{"authorization_code", "refresh_token"}, meta["grant_types_supported"])
		require.Equal(t, true, meta["authorization_response_iss_parameter_supported"])
	}
}

func TestSharedAuthorizationServer_NotFoundForUnknownOrEndpointModeIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	setIssuerMode(t, ctx, ti, f.issuer.ID, "endpoint", "")

	for _, issuerID := range []string{f.issuer.ID.String(), uuid.NewString(), "not-a-uuid"} {
		err := ti.service.HandleSharedAuthorizationServerMetadata(httptest.NewRecorder(), sharedRequest(ctx, http.MethodGet, "/oauth/usi/"+issuerID+"/.well-known/oauth-authorization-server", issuerID, nil))
		requireOopsCode(t, err, oops.CodeNotFound)
	}
}

func TestSharedAuthorizationServer_ProtectedResourceMetadataNamesSharedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+f.slugA, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", f.slugA)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleGetProtectedResource(w, req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	require.Equal(t, f.resource(ti, f.slugA), meta["resource"])
	require.Equal(t, []any{f.issuerURL}, meta["authorization_servers"])
}

// The acceptance flow: one client, one issuer, a token per MCP server, and a
// token for one server refused at the other.
func TestSharedAuthorizationServer_AuthorizesTokensBoundToOneServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	verifier := pkceVerifier(t)
	code, iss := sharedAuthorizationCode(t, ctx, ti, f, verifier, f.resource(ti, f.slugA))
	require.Equal(t, f.issuerURL, iss)

	// The token request omits resource: the code already names the server.
	w := sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {sharedTestRedirectURI},
		"client_id":     {f.client.ClientID},
		"code_verifier": {verifier},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	tokens := decodeSharedTokenResponse(t, w)
	require.NotEmpty(t, tokens.RefreshToken)
	claims := unverifiedClaims(t, tokens.AccessToken)
	require.Equal(t, f.issuerURL, claims["iss"])
	aud, err := claims.GetAudience()
	require.NoError(t, err)
	require.Equal(t, jwt.ClaimStrings{f.resource(ti, f.slugA)}, aud)

	served, err := servePublicHTTP(t, context.Background(), ti, f.slugA, makeInitializeBody(), tokens.AccessToken, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, served.Code, served.Body.String())

	_, err = servePublicHTTP(t, context.Background(), ti, f.slugB, makeInitializeBody(), tokens.AccessToken, nil)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// A second authorization for server B through the same client and issuer.
	verifierB := pkceVerifier(t)
	codeB, _ := sharedAuthorizationCode(t, ctx, ti, f, verifierB, f.resource(ti, f.slugB))
	wB := sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {codeB},
		"redirect_uri":  {sharedTestRedirectURI},
		"client_id":     {f.client.ClientID},
		"code_verifier": {verifierB},
		"resource":      {f.resource(ti, f.slugB)},
	})
	require.Equal(t, http.StatusOK, wB.Code, wB.Body.String())
	servedB, err := servePublicHTTP(t, context.Background(), ti, f.slugB, makeInitializeBody(), decodeSharedTokenResponse(t, wB).AccessToken, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, servedB.Code, servedB.Body.String())
}

func TestSharedAuthorizationServer_RefreshStaysBoundToTheSessionResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	verifier := pkceVerifier(t)
	code, _ := sharedAuthorizationCode(t, ctx, ti, f, verifier, f.resource(ti, f.slugA))
	w := sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {sharedTestRedirectURI},
		"client_id":     {f.client.ClientID},
		"code_verifier": {verifier},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	refreshToken := decodeSharedTokenResponse(t, w).RefreshToken

	// A per-endpoint token route would mint an issuer-scoped token from it, so
	// it refuses, without spending the refresh token.
	perEndpoint := performRefreshRequest(ctx, ti, f.slugA, f.client.ClientID, refreshToken)
	require.NoError(t, perEndpoint.err)
	require.Equal(t, http.StatusBadRequest, perEndpoint.code, perEndpoint.body)
	require.Contains(t, perEndpoint.body, "invalid_grant")

	mismatched := sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {f.client.ClientID},
		"resource":      {f.resource(ti, f.slugB)},
	})
	require.Equal(t, http.StatusBadRequest, mismatched.Code, mismatched.Body.String())
	require.Equal(t, "invalid_target", decodeSharedTokenResponse(t, mismatched).Error)

	refreshed := sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {f.client.ClientID},
	})
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	rotated := decodeSharedTokenResponse(t, refreshed)
	require.NotEqual(t, refreshToken, rotated.RefreshToken)
	claims := unverifiedClaims(t, rotated.AccessToken)
	require.Equal(t, f.issuerURL, claims["iss"])
	aud, err := claims.GetAudience()
	require.NoError(t, err)
	require.Equal(t, jwt.ClaimStrings{f.resource(ti, f.slugA)}, aud)

	served, err := servePublicHTTP(t, context.Background(), ti, f.slugA, makeInitializeBody(), rotated.AccessToken, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, served.Code, served.Body.String())
}

func TestSharedAuthorizationServer_CodeIsRefusedAtThePerEndpointTokenRoute(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	verifier := pkceVerifier(t)
	code, _ := sharedAuthorizationCode(t, ctx, ti, f, verifier, f.resource(ti, f.slugA))

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {sharedTestRedirectURI},
		"client_id":     {f.client.ClientID},
		"code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+f.slugA+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", f.slugA)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleToken(w, req))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "invalid_grant", decodeSharedTokenResponse(t, w).Error)

	// The refusal did not spend the code: the shared token endpoint still
	// redeems it.
	redeemed := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
	require.Equal(t, http.StatusOK, redeemed.Code, redeemed.Body.String())
}

func TestSharedAuthorizationServer_AuthorizeRefusesMissingOrForeignResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	other, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          f.issuer.ProjectID.UUID,
		Slug:               "other-usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	foreignSlug := seedPublicServerOnIssuer(t, ctx, ti, other.ID)

	for name, resources := range map[string][]string{
		"missing":  nil,
		"multiple": {f.resource(ti, f.slugA), f.resource(ti, f.slugB)},
		"foreign":  {f.resource(ti, foreignSlug)},
		"unknown":  {f.resource(ti, "no-such-server-"+uuid.NewString()[:8])},
		"host":     {"https://unknown.example.com/mcp/" + f.slugA},
		"case":     {strings.ToUpper(f.resource(ti, f.slugA))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := sharedAuthorize(t, ctx, ti, f, pkceChallenge(pkceVerifier(t)), resources...)
			require.Equal(t, http.StatusFound, w.Code, w.Body.String())
			redirect, err := url.Parse(w.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, sharedTestRedirectURI, redirect.Scheme+"://"+redirect.Host+redirect.Path)
			require.Equal(t, "invalid_target", redirect.Query().Get("error"))
			require.Equal(t, f.issuerURL, redirect.Query().Get("iss"))
			require.Equal(t, "client-state", redirect.Query().Get("state"))
		})
	}
}

func TestSharedAuthorizationServer_AuthorizeRendersRefusalForUnknownClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	f.client.ClientID = "unregistered-" + uuid.NewString()[:8]

	w := sharedAuthorize(t, ctx, ti, f, pkceChallenge(pkceVerifier(t)))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "invalid_target", decodeSharedTokenResponse(t, w).Error)
}

// A per-endpoint authorization server stays in service for the servers of a
// shared-mode issuer, so clients that cached it keep working.
func TestSharedAuthorizationServer_PerEndpointAuthorizationServerStillServes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	advertised, _ := fetchAdvertisedIssuer(t, ctx, ti, f.slugA)
	require.Equal(t, f.resource(ti, f.slugA), advertised)

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {f.client.ClientID},
		"redirect_uri":          {sharedTestRedirectURI},
		"code_challenge":        {pkceChallenge(pkceVerifier(t))},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+f.slugA+"/authorize?"+q.Encode(), nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", f.slugA)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	consentURL, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/mcp/"+f.slugA+"/connect", consentURL.Path)

	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentURL.Query().Get("state"))
	require.NoError(t, err)
	require.False(t, stored.Endpoint.SharedAuthorizationServer)

	// Its challenge does not continue on the shared consent page.
	err = ti.service.HandleSharedConsent(httptest.NewRecorder(), sharedRequest(ctx, http.MethodGet, "/oauth/usi/"+f.issuer.ID.String()+"/connect?state="+stored.ID, f.issuer.ID.String(), nil))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// A pinned issuer the deployment does not serve leaves the issuer's servers on
// their per-endpoint authorization servers instead of pointing clients at a
// 404.
func TestSharedAuthorizationServer_UnservablePinnedIssuerFallsBackToPerEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	for name, pinned := range map[string]string{
		"unserved host": "https://pinned.example.com/oauth/usi/" + f.issuer.ID.String(),
		"wrong path":    ti.serverURL.String() + "/oauth/usi/" + uuid.NewString(),
	} {
		setIssuerMode(t, ctx, ti, f.issuer.ID, "shared", pinned)

		err := ti.service.HandleSharedAuthorizationServerMetadata(httptest.NewRecorder(), sharedRequest(ctx, http.MethodGet, "/oauth/usi/"+f.issuer.ID.String()+"/.well-known/oauth-authorization-server", f.issuer.ID.String(), nil))
		requireOopsCode(t, err, oops.CodeNotFound)
		require.Equal(t, []string{f.resource(ti, f.slugA)}, protectedResourceAuthorizationServers(t, ctx, ti, f.slugA), name)
	}
}

func TestSharedAuthorizationServer_PinnedIssuerOnServedHostIsCanonical(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	setIssuerMode(t, ctx, ti, f.issuer.ID, "shared", strings.ToUpper(ti.serverURL.Scheme)+"://"+strings.ToUpper(ti.serverURL.Host)+"/oauth/usi/"+f.issuer.ID.String())

	require.Equal(t, []string{f.issuerURL}, protectedResourceAuthorizationServers(t, ctx, ti, f.slugA))
}

func protectedResourceAuthorizationServers(t *testing.T, ctx context.Context, ti *testInstance, slug string) []string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+slug, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleGetProtectedResource(w, req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var meta struct {
		AuthorizationServers []string `json:"authorization_servers"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	return meta.AuthorizationServers
}

// Custom domains and directly addressed toolsets are both MCP servers a shared
// authorization server resolves from a resource indicator.
func TestSharedAuthorizationServer_ResolvesCustomDomainToolsetResource(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.com/authorize")
	require.NoError(t, err)
	mock := &mockIdentityResolver{buildAuthURLResult: idpURL, hasAccessOK: true}
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	authCtx := requireProjectAuthContext(t, ctx)

	slug := "shared-cd-" + uuid.NewString()[:8]
	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	domainName := "shared-" + uuid.NewString()[:8] + ".example.com"
	toolset, _ = attachCustomDomainToToolset(t, ctx, ti, authCtx, toolset, domainName)
	clientID := "shared-cd-client-" + uuid.NewString()[:8]
	insertUserSessionClient(t, ctx, ti.conn, issuer.ID, clientID)
	setIssuerMode(t, ctx, ti, issuer.ID, "shared", "")

	f := sharedIssuerFixture{
		issuer:    issuer,
		client:    usersessions_repo.UserSessionClient{ClientID: clientID},
		issuerURL: ti.serverURL.String() + "/oauth/usi/" + issuer.ID.String(),
	}
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {"http://example.com/cb"},
		"code_challenge":        {pkceChallenge(pkceVerifier(t))},
		"code_challenge_method": {"S256"},
		"resource":              {"https://" + domainName + "/mcp/" + slug},
	}
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedAuthorize(w, sharedRequest(ctx, http.MethodGet, "/oauth/usi/"+f.issuer.ID.String()+"/authorize?"+q.Encode(), f.issuer.ID.String(), nil)))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	require.Equal(t, ti.serverURL.String()+"/mcp/idp_callback", mock.buildAuthURLParams.CallbackURL)

	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+mock.buildAuthURLParams.State)
	require.NoError(t, err)
	require.True(t, stored.Endpoint.SharedAuthorizationServer)
	require.Equal(t, "https://"+domainName, stored.Endpoint.BaseURL)
	require.Equal(t, toolset.CustomDomainID, stored.Endpoint.CustomDomainID)
	require.Equal(t, uuid.NullUUID{UUID: toolset.ID, Valid: true}, stored.Endpoint.ToolsetID)

}

// sharedTokens redeems a fresh authorization for resource on the shared
// authorization server.
func sharedTokens(t *testing.T, ctx context.Context, ti *testInstance, f sharedIssuerFixture, resource string) sharedTokenResponse {
	t.Helper()

	verifier := pkceVerifier(t)
	code, _ := sharedAuthorizationCode(t, ctx, ti, f, verifier, resource)
	w := sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {sharedTestRedirectURI},
		"client_id":     {f.client.ClientID},
		"code_verifier": {verifier},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return decodeSharedTokenResponse(t, w)
}

// A refresh token rotated moments ago still reaches its replay on the shared
// token endpoint, and never on a per-endpoint one.
func TestSharedAuthorizationServer_RefreshReplayStaysOnTheSharedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	refreshToken := sharedTokens(t, ctx, ti, f, f.resource(ti, f.slugA)).RefreshToken
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {f.client.ClientID}}

	first := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())

	replayed := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
	require.Equal(t, http.StatusOK, replayed.Code, replayed.Body.String())
	require.Equal(t, decodeSharedTokenResponse(t, first).RefreshToken, decodeSharedTokenResponse(t, replayed).RefreshToken)

	perEndpoint := performRefreshRequest(ctx, ti, f.slugA, f.client.ClientID, refreshToken)
	require.NoError(t, perEndpoint.err)
	require.Equal(t, http.StatusBadRequest, perEndpoint.code, perEndpoint.body)
	require.Contains(t, perEndpoint.body, "issued for a different resource")
}

func TestSharedAuthorizationServer_RefreshDuringReplayPublicationIsRetryable(t *testing.T) {
	t.Parallel()

	publishing := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		return blockedSharedReplayCache{Cache: delegate, publishing: publishing, release: release}
	})
	f := seedSharedIssuer(t, ctx, ti)
	refreshToken := sharedTokens(t, ctx, ti, f, f.resource(ti, f.slugA)).RefreshToken
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {f.client.ClientID}}
	winner := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() {
		done <- ti.service.HandleSharedToken(winner, sharedRequest(ctx, http.MethodPost, f.issuerURL+"/token", f.issuer.ID.String(), strings.NewReader(form.Encode())))
	}()
	select {
	case <-publishing:
	case err := <-done:
		t.Fatalf("rotation ended before publishing its replay: %v; %s", err, winner.Body.String())
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	// The old row is gone, but the winner still owns the rotation lease.
	retry := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
	unblock()
	require.NoError(t, <-done)
	require.Equal(t, http.StatusOK, winner.Code, winner.Body.String())
	require.Equal(t, http.StatusServiceUnavailable, retry.Code, retry.Body.String())
	require.Equal(t, "temporarily_unavailable", decodeSharedTokenResponse(t, retry).Error)
	replayed := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
	require.Equal(t, http.StatusOK, replayed.Code, replayed.Body.String())
	assertSameTokenPair(t, winner.Body.String(), replayed.Body.String())
}

func TestSharedAuthorizationServer_RefreshReplayCacheErrorIsRetryable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		return failingReplayGetCache{Cache: delegate}
	})
	f := seedSharedIssuer(t, ctx, ti)
	refreshToken := sharedTokens(t, ctx, ti, f, f.resource(ti, f.slugA)).RefreshToken
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {f.client.ClientID}}
	rotated := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
	require.Equal(t, http.StatusOK, rotated.Code, rotated.Body.String())
	retry := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
	require.Equal(t, http.StatusServiceUnavailable, retry.Code, retry.Body.String())
	require.Equal(t, "temporarily_unavailable", decodeSharedTokenResponse(t, retry).Error)
}

// blockedSharedReplayCache holds publication after the database commit so a
// second request can observe the missing live row without a replay yet.
type blockedSharedReplayCache struct {
	cache.Cache
	publishing chan struct{}
	release    chan struct{}
}

func (c blockedSharedReplayCache) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	return acquireLease(ctx, c.Cache, key, owner, ttl)
}

func (c blockedSharedReplayCache) ReleaseLeaseIfOwner(ctx context.Context, key, owner string) (bool, error) {
	return releaseLease(ctx, c.Cache, key, owner)
}

func (c blockedSharedReplayCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if strings.HasPrefix(key, "userSessionRefreshReplay:") {
		close(c.publishing)
		select {
		case <-c.release:
		case <-ctx.Done():
			return fmt.Errorf("wait to publish replay: %w", ctx.Err())
		}
	}
	if err := c.Cache.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("publish replay: %w", err)
	}
	return nil
}

func (c blockedSharedReplayCache) SetIfAbsent(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return setIfAbsent(ctx, c.Cache, key, value, ttl)
}

func TestSharedAuthorizationServer_RegistersAndRevokesForTheIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	registerReq := httptest.NewRequest(http.MethodPost, "/oauth/usi/"+f.issuer.ID.String()+"/register", strings.NewReader(`{"client_name":"shared dcr","redirect_uris":["`+sharedTestRedirectURI+`"],"token_endpoint_auth_method":"none"}`))
	registerReq.Header.Set("Content-Type", "application/json")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("issuerID", f.issuer.ID.String())
	registerReq = registerReq.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	registered := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedRegister(registered, registerReq))
	require.Equal(t, http.StatusCreated, registered.Code, registered.Body.String())
	var registration struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &registration))
	_, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: f.issuer.ID,
		ClientID:            registration.ClientID,
	})
	require.NoError(t, err, "a client registered on the shared authorization server belongs to the issuer")

	refreshToken := sharedTokens(t, ctx, ti, f, f.resource(ti, f.slugA)).RefreshToken
	revoked := httptest.NewRecorder()
	revokeForm := url.Values{"token": {refreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {f.client.ClientID}}
	require.NoError(t, ti.service.HandleSharedRevoke(revoked, sharedRequest(ctx, http.MethodPost, "/oauth/usi/"+f.issuer.ID.String()+"/revoke", f.issuer.ID.String(), strings.NewReader(revokeForm.Encode()))))
	require.Equal(t, http.StatusOK, revoked.Code, revoked.Body.String())

	refreshed := sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {f.client.ClientID}})
	require.Equal(t, http.StatusBadRequest, refreshed.Code, refreshed.Body.String())
	require.Equal(t, "invalid_grant", decodeSharedTokenResponse(t, refreshed).Error)
}

// An issuer switched out of shared mode closes the flows its shared
// authorization server started.
func TestSharedAuthorizationServer_ModeSwitchClosesInFlightFlows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)

	authorized := sharedAuthorize(t, ctx, ti, f, pkceChallenge(pkceVerifier(t)), f.resource(ti, f.slugA))
	require.Equal(t, http.StatusFound, authorized.Code, authorized.Body.String())
	consentURL, err := url.Parse(authorized.Header().Get("Location"))
	require.NoError(t, err)
	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentURL.Query().Get("state"))
	require.NoError(t, err)

	setIssuerMode(t, ctx, ti, f.issuer.ID, "endpoint", "")

	form := url.Values{"state": {stored.ID}, "csrf_token": {stored.CSRFToken}, "action": {"approve"}}
	err = ti.service.HandleSharedConsent(httptest.NewRecorder(), sharedRequest(ctx, http.MethodPost, "/oauth/usi/"+f.issuer.ID.String()+"/connect", f.issuer.ID.String(), strings.NewReader(form.Encode())))
	requireOopsCode(t, err, oops.CodeNotFound)

	// Nor can a per-endpoint consent page pick it up.
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+f.slugA+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", f.slugA)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	err = ti.service.HandleConsent(httptest.NewRecorder(), req)
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestSharedAuthorizationServer_ResolvesXMCPResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	resource := ti.serverURL.JoinPath("x", "mcp", f.slugA).String()

	verifier := pkceVerifier(t)
	code, _ := sharedAuthorizationCode(t, ctx, ti, f, verifier, resource)
	w := sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {sharedTestRedirectURI},
		"client_id":     {f.client.ClientID},
		"code_verifier": {verifier},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	aud, err := unverifiedClaims(t, decodeSharedTokenResponse(t, w).AccessToken).GetAudience()
	require.NoError(t, err)
	require.Equal(t, jwt.ClaimStrings{resource}, aud)
}

// A remote session connect from the shared consent page returns there, while
// keeping its consent parent: the callback still checks that parent's browser
// binding.
func TestSharedAuthorizationServer_RemoteConnectReturnsToSharedConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	f := seedSharedIssuer(t, ctx, ti)
	remoteClientID := createConsentRemoteClient(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "shared-rc-"+uuid.NewString()[:8], "", []uuid.UUID{f.issuer.ID})

	authorized := sharedAuthorize(t, ctx, ti, f, pkceChallenge(pkceVerifier(t)), f.resource(ti, f.slugA))
	require.Equal(t, http.StatusFound, authorized.Code, authorized.Body.String())
	consentURL, err := url.Parse(authorized.Header().Get("Location"))
	require.NoError(t, err)
	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentURL.Query().Get("state"))
	require.NoError(t, err)

	form := url.Values{"state": {stored.ID}, "csrf_token": {stored.CSRFToken}, "action": {"connect"}, "client_id": {remoteClientID.String()}}
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedConsentAction(w, sharedRequest(ctx, http.MethodPost, "/oauth/usi/"+f.issuer.ID.String()+"/connect/remote-session", f.issuer.ID.String(), strings.NewReader(form.Encode()))))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	upstream, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)

	loginCache := cache.NewTypedObjectCache[remotesessions.RemoteLoginState](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	login, err := loginCache.Get(ctx, "remoteLogin:"+upstream.Query().Get("state"))
	require.NoError(t, err)
	require.Equal(t, f.issuerURL+"/connect?state="+stored.ID, login.ConsentURL)
	require.Empty(t, login.FinalRedirectURI, "a consent parent's login must not take the parentless shortcut")
	require.Equal(t, stored.ID, login.ParentChallengeID)
}

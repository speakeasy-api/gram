package mcp_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// platformHostChecklistExtraHost is the extra platform host (--platform-hosts)
// the checklist runs against next to the server URL's own host.
const platformHostChecklistExtraHost = "ai.example.test"

// platformHostChecklistEnvironment is any environment other than "local":
// local development stamps the server URL on every request and skips host
// classification, which is exactly what the checklist must exercise.
const platformHostChecklistEnvironment = "prod"

// platformHostChecklistRedirectURI is the MCP client's loopback callback.
const platformHostChecklistRedirectURI = "http://localhost:3000/callback"

// platformHostTarget is one first-party host an MCP client can be pointed at.
type platformHostTarget struct {
	// host is the Host header the client sends.
	host string

	// baseURL is the externally visible origin the server must render for it.
	baseURL string
}

// newPlatformHostMux mounts the MCP routes behind the host-dependent part of
// the MCP tier's middleware chain (MCP security, host classification, and the
// session cookie), configured with the harness server URL as the canonical
// host plus one extra platform host.
func newPlatformHostMux(t *testing.T, ti *testInstance) (http.Handler, platformHostTarget, platformHostTarget) {
	t.Helper()

	platformHosts, err := customdomains.ParsePlatformHosts([]string{platformHostChecklistExtraHost})
	require.NoError(t, err)

	metadataService := mcpmetadata.NewService(
		ti.logger,
		ti.tracerProvider,
		testenv.NewMeterProvider(t),
		ti.conn,
		ti.sessionManager,
		ti.serverURL,
		ti.siteURL,
		ti.cacheAdapter,
		authz.NewEngine(ti.logger, ti.conn, nil, workos.NewStubClient()),
		ti.audit,
		nil,
	)

	mcpSecurity, err := middleware.MCPSecurity(ti.logger, append([]string{ti.serverURL.String()}, slices.Sorted(maps.Values(platformHosts))...), mcp.ServesInstallPage)
	require.NoError(t, err)

	mux := goahttp.NewMuxer()
	mux.Use(mcpSecurity)
	mux.Use(customdomains.Middleware(ti.logger, ti.conn, platformHostChecklistEnvironment, ti.serverURL, platformHosts))
	mux.Use(middleware.SessionMiddleware)
	mcp.Attach(mux, ti.service, metadataService)

	canonical := platformHostTarget{host: ti.serverURL.Host, baseURL: strings.TrimSuffix(ti.serverURL.String(), "/")}
	extra := platformHostTarget{host: platformHostChecklistExtraHost, baseURL: platformHosts[platformHostChecklistExtraHost]}
	return mux, canonical, extra
}

// serveOnHost sends one request through handler with the given Host header.
func serveOnHost(t *testing.T, handler http.Handler, method, host, target string, body []byte, header http.Header) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	req.Host = host
	maps.Copy(req.Header, header)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func decodeJSONObject(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body
}

func formHeader() http.Header {
	return http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
}

func mcpRuntimeHeader(accessToken string) http.Header {
	header := http.Header{
		"Accept":       {"application/json, text/event-stream"},
		"Content-Type": {"application/json"},
	}
	if accessToken != "" {
		header.Set("Authorization", "Bearer "+accessToken)
	}
	return header
}

// TestPlatformHostVerificationChecklist runs the "Deprecate app.getgram.ai"
// verification checklist for a per-endpoint MCP server from an MCP client's
// point of view, once per platform host: discovery metadata and the
// WWW-Authenticate challenge name the requested host, a full registration,
// authorization, consent, and token flow issues tokens whose issuer is that
// host and which the runtime accepts there before and after refresh, and the
// private install page sends a signed-out visitor to that host's login page.
//
// Cross-host refusal is not asserted: authorization-code and refresh grants
// mint the issuer-scoped audience (user_session_issuer:<id>), which the
// runtime accepts on every host. The Platform MCP checklist asserts it.
func TestPlatformHostVerificationChecklist(t *testing.T) {
	t.Parallel()

	for _, requested := range []string{"canonical", "extra"} {
		t.Run(requested, func(t *testing.T) {
			t.Parallel()

			idpURL, err := url.Parse("https://idp.example.test/authorize")
			require.NoError(t, err)
			resolver := &mockIdentityResolver{
				buildAuthURLResult: idpURL,
				exchangeResult:     &identity.IDPUserInfo{Sub: "platform-host-checklist", Email: "checklist@example.test", Name: "Checklist User"},
				hasAccessOK:        true,
			}
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, resolver)
			handler, canonical, extra := newPlatformHostMux(t, ti)
			on, other := canonical, extra
			if requested == "extra" {
				on, other = extra, canonical
			}

			toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := toolset.McpSlug.String
			mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, "private", uuid.NullUUID{}, issuer.ID)
			// The identity provider signs in the harness's organization member,
			// who holds mcp:connect on the server.
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			resolver.upsertResult = authCtx.UserID
			seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, mcpServer.ID.String())
			resource := on.baseURL + "/mcp/" + slug
			protectedResourceURL := on.baseURL + "/.well-known/oauth-protected-resource/mcp/" + slug

			// 1. Discovery names the requested host.
			protected := serveOnHost(t, handler, http.MethodGet, on.host, "/.well-known/oauth-protected-resource/mcp/"+slug, nil, nil)
			require.Equal(t, http.StatusOK, protected.Code, protected.Body.String())
			protectedMeta := decodeJSONObject(t, protected)
			require.Equal(t, resource, protectedMeta["resource"])
			require.Equal(t, []any{resource}, protectedMeta["authorization_servers"])

			server := serveOnHost(t, handler, http.MethodGet, on.host, "/.well-known/oauth-authorization-server/mcp/"+slug, nil, nil)
			require.Equal(t, http.StatusOK, server.Code, server.Body.String())
			serverMeta := decodeJSONObject(t, server)
			require.Equal(t, resource, serverMeta["issuer"])
			require.Equal(t, resource+"/authorize", serverMeta["authorization_endpoint"])
			require.Equal(t, resource+"/token", serverMeta["token_endpoint"])
			require.Equal(t, resource+"/register", serverMeta["registration_endpoint"])

			challenge := serveOnHost(t, handler, http.MethodPost, on.host, "/mcp/"+slug, makeInitializeBody(), mcpRuntimeHeader(""))
			require.Equal(t, http.StatusUnauthorized, challenge.Code, challenge.Body.String())
			require.Equal(t, mcp.AuthenticateChallengeHeader(protectedResourceURL), challenge.Header().Get("WWW-Authenticate"))

			// 2. Registration, authorization, consent, and token exchange.
			register := serveOnHost(t, handler, http.MethodPost, on.host, "/mcp/"+slug+"/register", []byte(`{"client_name":"platform host checklist","redirect_uris":["`+platformHostChecklistRedirectURI+`"],"token_endpoint_auth_method":"none"}`), http.Header{"Content-Type": {"application/json"}})
			require.Equal(t, http.StatusCreated, register.Code, register.Body.String())
			clientID, ok := decodeJSONObject(t, register)["client_id"].(string)
			require.True(t, ok)
			require.NotEmpty(t, clientID)

			verifier := pkceVerifier(t)
			authorizeQuery := url.Values{
				"response_type":         {"code"},
				"client_id":             {clientID},
				"redirect_uri":          {platformHostChecklistRedirectURI},
				"state":                 {"client-state"},
				"code_challenge":        {pkceChallenge(verifier)},
				"code_challenge_method": {"S256"},
				"resource":              {resource},
			}
			authorize := serveOnHost(t, handler, http.MethodGet, on.host, "/mcp/"+slug+"/authorize?"+authorizeQuery.Encode(), nil, nil)
			require.Equal(t, http.StatusFound, authorize.Code, authorize.Body.String())
			require.Equal(t, idpURL.String(), authorize.Header().Get("Location"))
			// The identity provider only knows the server URL's callback.
			require.Equal(t, ti.serverURL.String()+"/mcp/idp_callback", resolver.buildAuthURLParams.CallbackURL)
			require.NotEmpty(t, resolver.buildAuthURLParams.State)

			callback := serveOnHost(t, handler, http.MethodGet, canonical.host, "/mcp/idp_callback?"+url.Values{"state": {resolver.buildAuthURLParams.State}, "code": {"idp-code"}}.Encode(), nil, nil)
			require.Equal(t, http.StatusFound, callback.Code, callback.Body.String())
			consentURL, err := url.Parse(callback.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, resource+"/connect", consentURL.Scheme+"://"+consentURL.Host+consentURL.Path, "consent must return the browser to the requested host")
			consentState := consentURL.Query().Get("state")
			require.NotEmpty(t, consentState)

			stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentState)
			require.NoError(t, err)
			consent := serveOnHost(t, handler, http.MethodPost, on.host, "/mcp/"+slug+"/connect", []byte(url.Values{"state": {consentState}, "csrf_token": {stored.CSRFToken}, "action": {"approve"}}.Encode()), formHeader())
			require.Equal(t, http.StatusSeeOther, consent.Code, consent.Body.String())
			clientRedirect, err := url.Parse(consent.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, resource, clientRedirect.Query().Get("iss"))
			code := clientRedirect.Query().Get("code")
			require.NotEmpty(t, code)

			token := serveOnHost(t, handler, http.MethodPost, on.host, "/mcp/"+slug+"/token", []byte(url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {code},
				"redirect_uri":  {platformHostChecklistRedirectURI},
				"client_id":     {clientID},
				"code_verifier": {verifier},
				"resource":      {resource},
			}.Encode()), formHeader())
			require.Equal(t, http.StatusOK, token.Code, token.Body.String())
			require.Equal(t, resource, accessTokenClaims(t, token.Body.Bytes())["iss"])
			tokens := decodeJSONObject(t, token)
			accessToken, ok := tokens["access_token"].(string)
			require.True(t, ok)
			refreshToken, ok := tokens["refresh_token"].(string)
			require.True(t, ok)
			require.NotEmpty(t, refreshToken)

			accepted := serveOnHost(t, handler, http.MethodPost, on.host, "/mcp/"+slug, makeInitializeBody(), mcpRuntimeHeader(accessToken))
			require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())

			refresh := serveOnHost(t, handler, http.MethodPost, on.host, "/mcp/"+slug+"/token", []byte(url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {refreshToken},
				"client_id":     {clientID},
			}.Encode()), formHeader())
			require.Equal(t, http.StatusOK, refresh.Code, refresh.Body.String())
			require.Equal(t, resource, accessTokenClaims(t, refresh.Body.Bytes())["iss"])
			refreshedAccessToken, ok := decodeJSONObject(t, refresh)["access_token"].(string)
			require.True(t, ok)
			refreshedAccepted := serveOnHost(t, handler, http.MethodPost, on.host, "/mcp/"+slug, makeInitializeBody(), mcpRuntimeHeader(refreshedAccessToken))
			require.Equal(t, http.StatusOK, refreshedAccepted.Code, refreshedAccepted.Body.String())

			// 3. The private install page sends a signed-out visitor to this
			// host's login page with a relative return path.
			installPath := "/mcp/" + slug + "/install"
			install := serveOnHost(t, handler, http.MethodGet, on.host, installPath, nil, nil)
			require.Equal(t, http.StatusFound, install.Code, install.Body.String())
			login, err := url.Parse(install.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, on.baseURL+"/login", login.Scheme+"://"+login.Host+login.Path)
			require.Equal(t, installPath, login.Query().Get("redirect"))

			// The other host still challenges with its own metadata.
			crossHost := serveOnHost(t, handler, http.MethodPost, other.host, "/mcp/"+slug, makeInitializeBody(), mcpRuntimeHeader(""))
			require.Equal(t, http.StatusUnauthorized, crossHost.Code, crossHost.Body.String())
			require.Equal(t, mcp.AuthenticateChallengeHeader(other.baseURL+"/.well-known/oauth-protected-resource/mcp/"+slug), crossHost.Header().Get("WWW-Authenticate"))
		})
	}
}

// The per-client IdP callback is mounted beside the shared one and admits the
// cross-site navigation an upstream IdP redirect arrives as.
func TestPlatformHostMountsPerClientIDPCallback(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	handler, canonical, _ := newPlatformHostMux(t, ti)
	crossSite := http.Header{"Sec-Fetch-Site": {"cross-site"}, "Sec-Fetch-Mode": {"navigate"}}
	path := "/mcp/idp_callback/" + uuid.NewString()

	missing := serveOnHost(t, handler, http.MethodGet, canonical.host, path, nil, crossSite)
	require.Equal(t, http.StatusBadRequest, missing.Code, "the handler, not the router, rejects a stateless callback")
	unknown := serveOnHost(t, handler, http.MethodGet, canonical.host, path+"?"+url.Values{"state": {uuid.NewString()}, "code": {"idp-code"}}.Encode(), nil, crossSite)
	require.Equal(t, http.StatusUnauthorized, unknown.Code)
	nested := serveOnHost(t, handler, http.MethodGet, canonical.host, path+"/extra", nil, crossSite)
	require.Equal(t, http.StatusNotFound, nested.Code)
}

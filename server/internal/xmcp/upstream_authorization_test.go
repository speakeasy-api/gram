// upstream_authorization_test.go covers mcp_servers.visibility = 'upstream':
// the server advertises its own upstream authorization server in the well-known
// documents and forwards the inbound bearer to it unchanged.
//
// The interesting cases are all about the fronting mcp_servers row overriding
// the backing toolset. A converted server keeps a toolset whose mcp_is_public
// is false and whose external_oauth_server_id is NULL, and every gate on the
// toolset-backed serve path keys on exactly those two columns, so a test that
// seeded an already-public toolset would pass without proving anything.
package xmcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/xmcp"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const upstreamIssuerURL = "https://idp.example.com"

// upstreamDiscoveryDocument is what an issuer's own well-known endpoint served,
// including fields remote_session_issuers has no column for. Those are the
// reason the snapshot exists, so they are what the assertions look for.
func upstreamDiscoveryDocument() json.RawMessage {
	return json.RawMessage(`{
		"issuer": "https://idp.example.com",
		"authorization_endpoint": "https://idp.example.com/authorize",
		"token_endpoint": "https://idp.example.com/token",
		"registration_endpoint": "https://idp.example.com/register",
		"response_types_supported": ["code"],
		"grant_types_supported": ["authorization_code", "refresh_token"],
		"code_challenge_methods_supported": ["S256"],
		"userinfo_endpoint": "https://idp.example.com/userinfo",
		"introspection_endpoint": "https://idp.example.com/introspect",
		"claims_supported": ["sub", "email"],
		"id_token_signing_alg_values_supported": ["RS256"]
	}`)
}

func seedRemoteSessionIssuer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, organizationID string, snapshot json.RawMessage) uuid.UUID {
	t.Helper()

	issuer, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              "upstream-idp-" + uuid.NewString()[:8],
		Issuer:                            upstreamIssuerURL,
		Name:                              conv.ToPGText("Upstream IdP"),
		AuthorizationEndpoint:             conv.ToPGText(upstreamIssuerURL + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(upstreamIssuerURL + "/token"),
		RegistrationEndpoint:              conv.ToPGText(upstreamIssuerURL + "/register"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		Metadata:                          snapshot,
	})
	require.NoError(t, err)
	return issuer.ID
}

// seedUpstreamToolsetMCPEndpoint wires a full /x/mcp resolution chain for a
// hosted server serving direct upstream authorization. The backing toolset is
// left non-public on purpose, which is the state a converted server is in.
//
// The issuer id is stamped after creation the way the binding resync does it,
// because a fresh server has no client bindings to derive it from.
func seedUpstreamToolsetMCPEndpoint(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	projectID uuid.UUID,
	organizationID string,
	snapshot json.RawMessage,
) (slug string, mcpServer mcpserversrepo.McpServer, issuerID uuid.UUID) {
	t.Helper()

	issuerID = seedRemoteSessionIssuer(t, ctx, ti, projectID, organizationID, snapshot)

	toolset := seedBareToolset(t, ctx, ti, projectID, organizationID, "ts-upstream-"+uuid.NewString()[:8])
	slug, mcpServer = seedToolsetMCPEndpoint(t, ctx, ti, projectID, toolset, mcpservers.VisibilityUpstream)

	stamped, err := testrepo.New(ti.conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		ID:                    mcpServer.ID,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped, "the issuer stamp must land on exactly one live server")

	return slug, mcpServer, issuerID
}

// The document a client fetches is the upstream's, with only `issuer` rewritten
// to the Gram URL it was fetched from (RFC 8414 3.3). This is the behavior the
// toolset external-OAuth branch had, now keyed on mcp_servers.
func TestUpstream_WellKnownAuthorizationServerServesTheIssuerSnapshot(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedUpstreamToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, upstreamDiscoveryDocument())

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, 200, w.Code)

	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	issuer, isString := got["issuer"].(string)
	require.True(t, isString)
	require.Contains(t, issuer, "/x/mcp/"+slug, "the served issuer must be the Gram URL the client fetched this from")

	require.Equal(t, upstreamIssuerURL+"/authorize", got["authorization_endpoint"])
	require.Equal(t, upstreamIssuerURL+"/token", got["token_endpoint"])

	// Fields no remote_session_issuers column models. Reconstructing from the
	// typed columns would silently drop these.
	require.Equal(t, upstreamIssuerURL+"/userinfo", got["userinfo_endpoint"])
	require.Equal(t, upstreamIssuerURL+"/introspect", got["introspection_endpoint"])
	require.Equal(t, []any{"sub", "email"}, got["claims_supported"])
	require.Equal(t, []any{"RS256"}, got["id_token_signing_alg_values_supported"])
}

// An issuer that has not been refreshed since the snapshot column existed still
// serves a spec-valid document, built from the typed columns.
func TestUpstream_WellKnownAuthorizationServerReconstructsWithoutASnapshot(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedUpstreamToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, nil)

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, 200, w.Code)

	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	issuer, isString := got["issuer"].(string)
	require.True(t, isString)
	require.Contains(t, issuer, "/x/mcp/"+slug)
	require.Equal(t, upstreamIssuerURL+"/authorize", got["authorization_endpoint"])
	require.Equal(t, upstreamIssuerURL+"/token", got["token_endpoint"])

	// The degradation this path costs, asserted so the difference from the
	// snapshot path stays visible rather than being assumed.
	require.NotContains(t, got, "userinfo_endpoint")
	require.NotContains(t, got, "claims_supported")
}

// The protected-resource document points a client at the Gram URL it arrived
// at, whose authorization-server document is the upstream's. The legacy toolset
// resolver would 404 here, since a converted server's toolset has no
// external_oauth_server_id.
func TestUpstream_WellKnownProtectedResourceAdvertisesTheGramURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedUpstreamToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, upstreamDiscoveryDocument())

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, 200, w.Code)

	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	resource, isString := got["resource"].(string)
	require.True(t, isString)
	require.Contains(t, resource, "/x/mcp/"+slug)
	require.Equal(t, []any{resource}, got["authorization_servers"])
}

// A row that names no issuer has no authorization server to advertise, so it is
// not served at all rather than served as something else.
func TestUpstream_WithoutAnIssuerIsNotServed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := seedBareToolset(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "ts-upstream-noissuer-"+uuid.NewString()[:8])
	slug, _ := seedToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, toolset, mcpservers.VisibilityUpstream)

	_, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.Error(t, err)
}

// A tokenless call must be challenged, not refused. Two separate bugs produce a
// response that looks almost right here: without oauthRequired the 401 carries
// no WWW-Authenticate, leaving a client with no way to discover the
// authorization server, and without the upstream guard on the toolset gate the
// non-public backing toolset answers 404, which reads as "no such server"
// rather than "authenticate first".
func TestUpstream_RuntimeChallengesATokenlessCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedUpstreamToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, upstreamDiscoveryDocument())

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code, "body=%s", rr.Body.String())

	challenge := rr.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, challenge, "a tokenless call must be told where to authenticate")
	require.Contains(t, challenge, "resource_metadata=")
	require.Contains(t, challenge, "/x/mcp/"+slug)
}

// The bearer is forwarded verbatim: Gram validates nothing, so any bearer gets
// past the gate. What this pins is that neither the non-public backing toolset
// nor the mcp:connect RBAC check refuses a caller Gram never authenticated.
func TestUpstream_RuntimeAcceptsAnInboundBearer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedUpstreamToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, upstreamDiscoveryDocument())

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer("upstream-issued-token"), []byte(initializeBody))
	require.NotEqual(t, http.StatusNotFound, rr.Code, "a non-public backing toolset must not 404 an upstream server; body=%s", rr.Body.String())
	require.NotEqual(t, http.StatusUnauthorized, rr.Code, "Gram must not validate a bearer it forwards; body=%s", rr.Body.String())
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
}

// Gram is not the authorization server for these clients, so it must not offer
// an authorize endpoint they could be redirected to. ResolvedMcpEndpoint.IsPublic
// is false for upstream, so reaching it would send the caller to the Speakeasy
// IDP for a server whose well-known documents point somewhere else entirely.
func TestUpstream_ExposesNoGramAuthorizeSurface(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedUpstreamToolsetMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, upstreamDiscoveryDocument())

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", "some-client")
	q.Set("redirect_uri", "https://client.example.com/callback")
	q.Set("code_challenge", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
	q.Set("code_challenge_method", "S256")
	q.Set("state", "upstream-authorize-state")

	mux := goahttp.NewMuxer()
	xmcp.Attach(mux, ti.service, nil)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/x/mcp/"+slug+"/authorize?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "upstream servers must expose no Gram authorize endpoint; body=%s", w.Body.String())
	require.NotContains(t, w.Header().Get("Location"), "/connect", "a redirect here would send the caller into a Gram consent flow")
}

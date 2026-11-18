package mcp_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

// Typed personal credentials need no legacy issuer binding, including when a
// consent grant becomes a runtime session. Selecting one must not widen its catalog.
func TestOrganizationGatewayTypedPersonalConsentExecution(t *testing.T) {
	t.Parallel()
	for _, clientType := range []string{authz.AuthClientTypeRemoteSession, authz.AuthClientTypeEMA} {
		t.Run(clientType, func(t *testing.T) {
			t.Parallel()
			testOrganizationGatewayTypedPersonalConsentExecution(t, clientType)
		})
	}
}

func testOrganizationGatewayTypedPersonalConsentExecution(t *testing.T, clientType string) {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *ac.ProjectID
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, ac.ActiveOrganizationID, "typed-decoy-"+uuid.NewString(), uuid.Nil)
	upstream := newRecordingUpstream(t, "ping")
	excluded := newRecordingUpstream(t, "other")
	serverID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Typed personal", "typed-personal", 0, upstream.url)
	excludedID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Excluded", "typed-excluded", 1, excluded.url)
	for _, id := range []uuid.UUID{serverID, excludedID} {
		seedUserMCPConnectGrant(t, ctx, ti.conn, ac.ActiveOrganizationID, fx.subject.ID, id.String())
	}
	ti.service.manual = true
	ti.service.SetAuthClientPermitted(true)
	_, err := featuresrepo.New(ti.conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{OrganizationID: ac.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	mcp.ResetAuthEnforcementForTest(ti.service.Service)
	clients := authclientsrepo.New(ti.conn)
	var selectedClient uuid.UUID
	for i, token := range []string{"unselected-personal-token", "selected-personal-token"} {
		// No registration is attached to the organization issuer.
		registration := createConsentRemoteClient(t, ctx, ti.conn, projectID, ac.ActiveOrganizationID, "typed-"+uuid.NewString(), "", nil)
		require.NoError(t, testrepo.New(ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID, McpServerID: serverID, RemoteSessionClientID: registration}))
		client, err := clients.CreateAuthClient(ctx, authclientsrepo.CreateAuthClientParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: conv.ToNullUUID(projectID), Name: "Personal choice", ClientType: clientType, RemoteSessionClientID: conv.ToNullUUID(registration), ClientAttachmentScope: conv.ToPGText("project:" + projectID.String())})
		require.NoError(t, err)
		if clientType == authz.AuthClientTypeEMA {
			require.NoError(t, authclientfixtures.New(ti.conn).SeedAuthClientEMAFixture(ctx, authclientfixtures.SeedAuthClientEMAFixtureParams{AuthClientID: client.ID, Resource: upstream.url}))
		}
		_, err = clients.AttachAuthClientToServer(ctx, authclientsrepo.AttachAuthClientToServerParams{OrganizationID: ac.ActiveOrganizationID, McpServerID: serverID, AuthClientID: client.ID})
		require.NoError(t, err)
		if clientType == authz.AuthClientTypeRemoteSession {
			insertQualifiedRemoteSessionToken(t, ctx, ti, fx.issuerID, registration, fx.subject, token, upstream.url)
		}
		if i == 1 {
			selectedClient = client.ID
			if clientType == authz.AuthClientTypeEMA {
				ti.service.SetIdentityChainer(&singleUpstreamChainer{issuer: clientRemoteIssuerID(t, ctx, ti.conn, projectID, ac.ActiveOrganizationID, registration), upstream: upstream.url, usable: true})
			}
		}
	}
	client, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{UserSessionIssuerID: fx.issuerID, ClientID: "typed-consent-" + uuid.NewString(), ClientName: "Typed consent", RedirectUris: []string{"http://localhost:3000/callback"}, TokenEndpointAuthMethod: "none"})
	require.NoError(t, err)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	stateID := uuid.NewString()
	challenge := sha256.Sum256([]byte(gatewayConsentVerifier))
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{ID: stateID, UserSessionIssuerID: fx.issuerID, Endpoint: mcp.EndpointRef{McpSlug: endpoint.Slug, RouteBase: endpoint.RouteBase, MetaMcpServerID: endpoint.MetaMcpServerID}, ClientID: client.ClientID, RedirectURI: client.RedirectUris[0], CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: "S256", CSRFToken: "csrf-token", Subject: &fx.subject, CreatedAt: time.Now(), AutoConnectDone: true}))
	path := "/mcp/gateway/" + fx.organizationSlug
	query := url.Values{"state": {stateID}, "auth_client.typed-personal": {selectedClient.String()}}
	page := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(page, httptest.NewRequest(http.MethodGet, path+"/connect?"+query.Encode(), nil).WithContext(ctx), endpoint))
	require.Equal(t, http.StatusOK, page.Code)
	inventory := gatewayInventoryFromPage(t, page.Body.String())
	names := make([]string, 0, len(inventory.Tools))
	for _, tool := range inventory.Tools {
		names = append(names, tool.Name)
	}
	require.Contains(t, names, "typed-personal--ping")
	require.Contains(t, names, "typed-excluded--other")
	form := url.Values{"state": {stateID}, "csrf_token": {"csrf-token"}, "action": {"approve"}, "tool_inventory_id": {inventory.Attempt}, "discovery_mode": {string(metamcp.DiscoveryModeProgressive)}, "tool_catalog_mode": {string(toolfilter.CatalogModeCached)}, "tool_filtering": {"on"}, "tools": {"typed-personal--ping"}, "auth_client.typed-personal": {selectedClient.String()}}
	request := httptest.NewRequest(http.MethodPost, path+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approved := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(approved, request, endpoint))
	require.Equal(t, http.StatusSeeOther, approved.Code, approved.Body.String())
	location, err := url.Parse(approved.Header().Get("Location"))
	require.NoError(t, err)
	code := location.Query().Get("code")
	require.NotEmpty(t, code)
	grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grants.Get(ctx, "gatewayCatalogGrant:"+fx.issuerID.String()+":"+code)
	require.NoError(t, err)
	require.NotNil(t, grant.GatewayPolicy)
	rawPolicy, err := json.Marshal(grant.GatewayPolicy)
	require.NoError(t, err)
	policy, err := toolfilter.ParseSessionPolicy(rawPolicy)
	require.NoError(t, err)
	require.NotNil(t, policy.Catalog)
	require.NotNil(t, policy.Selection)
	require.True(t, policy.Selection.AllowsName("typed-personal--ping"))
	require.False(t, policy.Selection.AllowsName("typed-excluded--other"))
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ClientID}, "redirect_uri": {client.RedirectUris[0]}, "code_verifier": {gatewayConsentVerifier}}
	request = httptest.NewRequest(http.MethodPost, path+"/token", strings.NewReader(tokenForm.Encode())).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	issued := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeToken(issued, request, endpoint))
	require.Equal(t, http.StatusOK, issued.Code, issued.Body.String())
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(issued.Body.Bytes(), &tokens))
	require.NotEmpty(t, tokens.AccessToken)
	for _, name := range []string{"typed-personal--ping", "typed-excluded--other"} {
		response, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, tokens.AccessToken, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute_tool", "arguments": map[string]any{"name": name, "arguments": map[string]any{}}}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		text, isError := metaToolResultText(t, decodeRPCResponse(t, response))
		require.Equal(t, name != "typed-personal--ping", isError, text)
		if !isError {
			require.Contains(t, text, "pong from ping")
		}
	}
	expectedToken := "selected-personal-token"
	if clientType == authz.AuthClientTypeEMA {
		expectedToken = chainedMemberToken
	}
	upstream.requireToolCallAuth(t, "Bearer "+expectedToken)
	upstream.requireNeverCarried(t, "Bearer unselected-personal-token")
	for _, request := range excluded.journal() {
		require.NotEqual(t, "tools/call", request.rpcMethod, "excluded catalog tool must never execute")
	}
}

package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type catalogUpstream struct {
	url         string
	requests    atomic.Int32
	calls       atomic.Int32
	unavailable atomic.Bool
	future      atomic.Bool
	tools       int
	// holdNextList parks the next tools/list until its release closes.
	holdNextList atomic.Pointer[catalogListHold]
}

type catalogListHold struct {
	reached chan struct{}
	release chan struct{}
}

func newCatalogUpstream(t *testing.T, tools int) *catalogUpstream {
	t.Helper()
	upstream := &catalogUpstream{tools: tools}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if upstream.unavailable.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		upstream.requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch request.Method {
		case "server/discover":
			w.WriteHeader(http.StatusNotFound)
			return
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "catalog-fixture", "version": "1"}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			if hold := upstream.holdNextList.Swap(nil); hold != nil {
				close(hold.reached)
				<-hold.release
			}
			tools := []map[string]any{}
			count := upstream.tools
			if upstream.future.Load() {
				count++
			}
			for i := range count {
				tools = append(tools, map[string]any{"name": fmt.Sprintf("tool_%03d", i), "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}})
			}
			result = map[string]any{"tools": tools}
		case "tools/call":
			upstream.calls.Add(1)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "executed live"}}}
		case "ping":
			result = map[string]any{}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	upstream.url = server.URL
	return upstream
}

type catalogConnection struct {
	token  string
	policy *toolfilter.SessionPolicy
}

func TestConnectionCatalogPersistsTypedAuthClientWithoutBackfill(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind  string
		mode  toolfilter.CatalogMode
		oauth string
	}{
		{"remote_session", toolfilter.CatalogModeCached, ""}, {"static_credential", toolfilter.CatalogModeCached, ""},
		{"remote_session", toolfilter.CatalogModeLive, ""}, {"static_credential", toolfilter.CatalogModeLive, ""},
		{"static_credential", toolfilter.CatalogModeCached, "bound"}, {"static_credential", toolfilter.CatalogModeCached, "unbound"},
	} {
		t.Run(tc.kind+"/"+string(tc.mode)+"/"+tc.oauth, func(t *testing.T) {
			kind := tc.kind
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			auth, _ := contextvalues.GetAuthContext(ctx)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "catalog-typed-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
			upstream := newCatalogUpstream(t, 1)
			serverID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
			registration := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
			if kind == "remote_session" {
				registration = uuid.NullUUID{UUID: createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "typed-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID}), Valid: true}
				_, err := upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, McpServerID: serverID, RemoteSessionClientID: registration.UUID})
				require.NoError(t, err)
				insertQualifiedRemoteSessionToken(t, ctx, ti, issuer.ID, registration.UUID, urn.NewUserSubject(mockidp.MockUserID), "fixture-token", upstream.url)
			}
			if tc.oauth != "" {
				var bound []uuid.UUID
				if tc.oauth == "bound" {
					bound = []uuid.UUID{issuer.ID}
				}
				extra := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "other-"+uuid.NewString()[:8], "", bound)
				_, err := upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, McpServerID: serverID, RemoteSessionClientID: extra})
				require.NoError(t, err)
			}
			fixtures := authclientfixtures.New(ti.conn)
			authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, ClientType: kind, Source: "admin", RemoteSessionClientID: registration})
			require.NoError(t, err)
			if kind == "static_credential" {
				server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: *auth.ProjectID})
				require.NoError(t, err)
				_, err = fixtures.SeedRemoteHeaderFixture(ctx, authclientfixtures.SeedRemoteHeaderFixtureParams{RemoteMcpServerID: server.RemoteMcpServerID.UUID, Name: "X-Fixture-Key", Value: pgtype.Text{String: "fixture-key", Valid: true}})
				require.NoError(t, err)
				require.NoError(t, fixtures.SeedAuthClientHeaderFixture(ctx, authclientfixtures.SeedAuthClientHeaderFixtureParams{AuthClientID: authClientID, OrganizationID: auth.ActiveOrganizationID, Name: "X-Fixture-Key", Value: pgtype.Text{String: "fixture-key", Valid: true}}))
			}
			require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: serverID, AuthClientID: authClientID}))
			ti.service.manual = true
			connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, tc.mode, metamcp.DiscoveryModeDirect, nil)
			claims, err := usersessions.NewSigner("test-jwt-secret").Validate(connection.token, urn.NewUserSessionIssuer(issuer.ID).String())
			require.NoError(t, err)
			session, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
			require.NoError(t, err)
			selected, err := upstreamclientsrepo.New(ti.conn).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{OrganizationID: auth.ActiveOrganizationID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: session.ID, McpServerIds: []uuid.UUID{serverID}})
			require.NoError(t, err)
			require.Len(t, selected, 1)
			require.Equal(t, uuid.NullUUID{UUID: authClientID, Valid: true}, selected[0].AuthClientID)
			require.Contains(t, callGatewayTool(t, ti, slug, connection.token, "source--tool_000", map[string]any{}), "executed live")
			require.EqualValues(t, 1, upstream.calls.Load())
		})
	}
}

func TestGatewayConsentAmbiguousTypedClientRequiresSetup(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-ambiguous-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	serverID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	registration := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "ambiguous-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
	_, err := upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, McpServerID: serverID, RemoteSessionClientID: registration})
	require.NoError(t, err)
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuer.ID, registration, urn.NewUserSubject(mockidp.MockUserID), "fixture-token", upstream.url)
	fixtures := authclientfixtures.New(ti.conn)
	for range 2 {
		id, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
		require.NoError(t, err)
		require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: serverID, AuthClientID: id}))
	}
	ti.service.manual = true
	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	page := consentGetPageWithContext(t, ctx, ti, slug, stateID)
	require.Contains(t, page, "No servers could be verified")
	outcomes := gatewayOutcomesFromPage(page)
	require.Len(t, outcomes, 1)
	require.Equal(t, "source", outcomes[0].Slug)
	require.Equal(t, "not_set_up", outcomes[0].Status)
	require.Zero(t, upstream.requests.Load(), "an ambiguous typed choice never reaches an upstream through legacy auth")
}

func approveGatewayCatalog(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, client usersessionsrepo.UserSessionClient, gatewayID uuid.UUID, slug string, mode toolfilter.CatalogMode, discovery metamcp.DiscoveryMode, tools []string) catalogConnection {
	t.Helper()
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, ti, issuerID, client, gatewayID, slug)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	inventory := gatewayInventoryFromPage(t, consentGetPageWithContext(t, ctx, ti, slug, stateID))
	attempt := inventory.Attempt
	form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {"approve"}, "tool_inventory_id": {attempt}, "tool_filtering": {"off"}, "tool_catalog_mode": {string(mode)}, "discovery_mode": {string(discovery)}}
	if tools != nil {
		form.Set("tool_filtering", "on")
		form["tools"] = tools
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(response, request, endpoint))
	require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
	redirect, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	code := redirect.Query().Get("code")
	grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grants.Get(ctx, "gatewayCatalogGrant:"+issuerID.String()+":"+code)
	require.NoError(t, err)
	require.NotNil(t, grant.GatewayPolicy.Catalog)
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ClientID}, "redirect_uri": {client.RedirectUris[0]}, "code_verifier": {gatewayConsentVerifier}}
	issued := postForm(t, ti, slug, "token", tokenForm)
	require.Equal(t, http.StatusOK, issued.Code, issued.Body.String())
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(issued.Body.Bytes(), &tokens))
	return catalogConnection{token: tokens.AccessToken, policy: grant.GatewayPolicy}
}

func TestConnectionCatalogCachedDiscoveryNeverContactsUpstream(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		specific  bool
		discovery metamcp.DiscoveryMode
	}{
		{false, metamcp.DiscoveryModeDirect}, {true, metamcp.DiscoveryModeDirect},
		{false, metamcp.DiscoveryModeProgressive}, {true, metamcp.DiscoveryModeProgressive},
	} {
		t.Run(fmt.Sprintf("specific=%t/%s", tc.specific, tc.discovery), func(t *testing.T) {
			specific := tc.specific
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			auth, _ := contextvalues.GetAuthContext(ctx)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "catalog-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
			upstream := newCatalogUpstream(t, 150)
			memberSlug := "source"
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", memberSlug, 1, upstream.url)
			var selected []string
			if specific {
				selected = []string{memberSlug + "--tool_000"}
			}
			connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, tc.discovery, selected)
			tokenRequests := seedExpiredCatalogCredential(t, ctx, ti, issuer.ID, urn.NewUserSubject(mockidp.MockUserID))
			before := upstream.requests.Load()
			for _, method := range []string{"initialize", "ping", "tools/list", "tools/list"} {
				params := map[string]any{}
				if method == "initialize" {
					params = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "fixture", "version": "1"}}
				}
				response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, method, params), connection.token, nil)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				if method == "tools/list" && tc.discovery == metamcp.DiscoveryModeDirect {
					var listed struct {
						Result struct {
							Tools      []json.RawMessage `json:"tools"`
							NextCursor string            `json:"nextCursor"`
						} `json:"result"`
					}
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &listed))
					if specific {
						require.Len(t, listed.Result.Tools, 1)
						require.Empty(t, listed.Result.NextCursor)
					} else {
						require.Len(t, listed.Result.Tools, 100)
						page, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{"cursor": listed.Result.NextCursor}), connection.token, nil)
						require.NoError(t, err)
						listed.Result.NextCursor = ""
						require.NoError(t, json.Unmarshal(page.Body.Bytes(), &listed))
						require.Len(t, listed.Result.Tools, 50)
						require.Empty(t, listed.Result.NextCursor)
					}
				}
			}
			if tc.discovery == metamcp.DiscoveryModeProgressive {
				for _, call := range []struct {
					name string
					args map[string]any
				}{
					{"list_servers", map[string]any{}},
					{"describe_server", map[string]any{"server": memberSlug}},
					{"describe_tools", map[string]any{"tools": []string{memberSlug + "--tool_000"}}},
					{"connection_status", map[string]any{}},
				} {
					result := callGatewayTool(t, ti, slug, connection.token, call.name, call.args)
					require.NotContains(t, result, `"isError":true`)
					if call.name == "connection_status" {
						require.Contains(t, result, `"status":"verified_at_consent"`)
						require.NotContains(t, result, "servers ready")
						require.Contains(t, result, "current sign-in status has not been checked")
					}
				}
			}
			require.Equal(t, before, upstream.requests.Load(), "discovery must issue zero upstream requests")
			require.Zero(t, tokenRequests.Load(), "discovery must never refresh upstream credentials")
			upstream.future.Store(true)
			if tc.discovery == metamcp.DiscoveryModeDirect {
				require.Contains(t, callGatewayTool(t, ti, slug, connection.token, memberSlug+"--tool_150", map[string]any{}), `"isError":true`)
			} else {
				require.Contains(t, callGatewayTool(t, ti, slug, connection.token, "execute_tool", map[string]any{"name": memberSlug + "--tool_150", "arguments": map[string]any{}}), `"isError":true`)
			}
			require.Equal(t, before, upstream.requests.Load(), "an unseen read-only tool is rejected without discovery or execution")

			execute := func(name string) string {
				if tc.discovery == metamcp.DiscoveryModeProgressive {
					return callGatewayTool(t, ti, slug, connection.token, "execute_tool", map[string]any{"name": name, "arguments": map[string]any{}})
				}
				return callGatewayTool(t, ti, slug, connection.token, name, map[string]any{})
			}
			require.Contains(t, execute(memberSlug+"--tool_000"), "executed live")
			require.EqualValues(t, 1, upstream.calls.Load())
			if specific {
				require.NotContains(t, execute(memberSlug+"--tool_001"), "executed live")
				require.EqualValues(t, 1, upstream.calls.Load())
			}
		})
	}
}

func TestConnectionCatalogLiveExcludesNewAndPreviouslyUnavailableMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-live-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	approved := newCatalogUpstream(t, 1)
	unavailable := newCatalogUpstream(t, 1)
	unavailable.unavailable.Store(true)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Approved", "approved", 1, approved.url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Unavailable", "unavailable", 2, unavailable.url)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeLive, metamcp.DiscoveryModeDirect, nil)
	require.NotNil(t, connection.policy.Catalog)
	var raw []byte
	require.NoError(t, ti.cacheAdapter.Get(ctx, "mcpConnectionCatalog:v1:"+auth.ActiveOrganizationID+":"+connection.policy.Catalog.ID.String(), &raw))
	require.Contains(t, string(raw), `"members"`)
	require.NotContains(t, string(raw), `"tools"`, "live records retain only the approved member boundary")
	require.NotContains(t, string(raw), `"member_outcomes"`)
	approved.future.Store(true)
	unavailable.unavailable.Store(false)
	added := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Added", "added", 3, added.url)
	beforeUnavailable := unavailable.requests.Load()
	response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "approved--tool_001", "live includes later tools of approved members")
	require.NotContains(t, response.Body.String(), "unavailable--")
	require.NotContains(t, response.Body.String(), "added--")
	for _, name := range []string{"unavailable--tool_000", "added--tool_000"} {
		require.NotContains(t, callGatewayTool(t, ti, slug, connection.token, name, map[string]any{}), "executed live")
	}
	require.Equal(t, beforeUnavailable, unavailable.requests.Load())
	require.Zero(t, added.requests.Load())
	require.Contains(t, callGatewayTool(t, ti, slug, connection.token, "approved--tool_001", map[string]any{}), "executed live")
}

func TestConnectionCatalogEvictionRequiresConsent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-eviction-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
	require.NoError(t, ti.cacheAdapter.Delete(ctx, "mcpConnectionCatalog:v1:"+auth.ActiveOrganizationID+":"+connection.policy.Catalog.ID.String()))
	before := upstream.requests.Load()
	response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "give consent again to approve tools")
	require.Contains(t, response.Header().Get("WWW-Authenticate"), "resource_metadata")
	require.Equal(t, before, upstream.requests.Load())
}

func TestConnectionCatalogBlocksZeroApprovedMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-empty-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	upstream.unavailable.Store(true)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	page := consentGetPageWithContext(t, ctx, ti, slug, stateID)
	require.Contains(t, page, "No servers could be verified")
	require.NotContains(t, page, `id="consent-tools-root"`)
	var captured struct{ Attempt string }
	require.NoError(t, ti.cacheAdapter.Get(ctx, "consentToolInventory:"+stateID+":gateway:", &captured))
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {"approve"}, "tool_inventory_id": {captured.Attempt}, "tool_catalog_mode": {"cached"}, "tool_filtering": {"off"}}
	request := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	err = ti.service.ServeConsent(httptest.NewRecorder(), request, endpoint)
	require.ErrorContains(t, err, "No servers could be verified")
}

func TestGatewayConsentReloadOverwritesCaptureAndInvalidatesOldPage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-reload-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	first := gatewayInventoryFromPage(t, consentGetPageWithContext(t, ctx, ti, slug, stateID))
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	transport := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).WithContext(ctx)
	transport.Header.Set("Gram-Consent-State", stateID)
	transport.Header.Set("Gram-Consent-Csrf", csrf)
	transport.Header.Set("Gram-Consent-Inventory-Attempt", first.Attempt)
	transportErr := ti.service.ServeConsentMCP(httptest.NewRecorder(), transport, endpoint)
	var rejected *oops.ShareableError
	require.ErrorAs(t, transportErr, &rejected)
	require.Equal(t, oops.CodeBadRequest, rejected.Code, "gateway rejection precedes loading a single-server draft")
	second := gatewayInventoryFromPage(t, consentGetPageWithContext(t, ctx, ti, slug, stateID))
	require.NotEqual(t, first.Attempt, second.Attempt)
	var captured struct{ Attempt string }
	require.NoError(t, ti.cacheAdapter.Get(ctx, "consentToolInventory:"+stateID+":gateway:", &captured))
	require.Equal(t, second.Attempt, captured.Attempt)
	require.Error(t, ti.cacheAdapter.Get(ctx, "consentToolInventory:"+stateID+":"+first.Attempt+":", &captured))
	form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {"approve"}, "tool_inventory_id": {first.Attempt}, "tool_catalog_mode": {"cached"}, "tool_filtering": {"off"}}
	request := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(recorder, request, endpoint))
	requireConsentRestarted(t, recorder, slug, stateID, "an older tab cannot approve a replaced capture")
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err, "a refused approval must not consume the challenge")
}

// requireConsentRestarted asserts an approval was refused by sending the
// browser back to the consent page instead of to the client's redirect URI.
func requireConsentRestarted(t *testing.T, recorder *httptest.ResponseRecorder, slug, stateID, msg string) {
	t.Helper()
	require.Equal(t, http.StatusSeeOther, recorder.Code, msg)
	location, err := url.Parse(recorder.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/mcp/"+slug+"/connect", location.Path, msg)
	require.Equal(t, stateID, location.Query().Get("state"), msg)
}

func TestGatewayConsentStaleCaptureCannotReplaceNewerReload(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-stale-capture-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)

	// The older tab is still discovering when the newer one loads.
	hold := &catalogListHold{reached: make(chan struct{}), release: make(chan struct{})}
	upstream.holdNextList.Store(hold)
	type rendered struct {
		body string
		err  error
	}
	stale := make(chan rendered, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect?state="+stateID, nil)
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("mcpSlug", slug)
		w := httptest.NewRecorder()
		err := ti.service.HandleConsent(w, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx)))
		stale <- rendered{body: w.Body.String(), err: err}
	}()
	select {
	case <-hold.reached:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the older capture never reached discovery")
	}
	newer := gatewayInventoryFromPage(t, consentGetPageWithContext(t, ctx, ti, slug, stateID))
	close(hold.release)
	var older rendered
	select {
	case older = <-stale:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the older capture never finished")
	}
	require.NoError(t, older.err)
	require.Contains(t, older.body, "This page was replaced by a newer one")

	var captured struct{ Attempt string }
	require.NoError(t, ti.cacheAdapter.Get(ctx, "consentToolInventory:"+stateID+":gateway:", &captured))
	require.Equal(t, newer.Attempt, captured.Attempt, "the stale capture finishing last does not replace the newer one")
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {"approve"}, "tool_inventory_id": {newer.Attempt}, "tool_catalog_mode": {"cached"}, "tool_filtering": {"off"}}
	request := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approve := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(approve, request, endpoint))
	require.Equal(t, http.StatusSeeOther, approve.Code, approve.Body.String())
}

func TestGatewayConsentFailedReloadInvalidatesOldPage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-failed-reload-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	first := gatewayInventoryFromPage(t, consentGetPageWithContext(t, ctx, ti, slug, stateID))
	denied := authztest.WithExactGrants(t, ctx)
	page := consentGetPageWithContext(t, denied, ti, slug, stateID)
	require.Contains(t, page, "You do not have access")
	var capture any
	require.Error(t, ti.cacheAdapter.Get(ctx, "consentToolInventory:"+stateID+":gateway:", &capture))
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {"approve"}, "tool_inventory_id": {first.Attempt}, "tool_catalog_mode": {"cached"}, "tool_filtering": {"off"}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(recorder, req, endpoint))
	requireConsentRestarted(t, recorder, slug, stateID, "a failed reload invalidates approval from earlier tabs")
}

func TestConnectionCatalogPublicationFailurePreservesChallenge(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		cas, ok := delegate.(cache.CompareAndSwapCache)
		require.True(t, ok)
		return catalogPublicationFailureCache{failingConditionalCache: failingConditionalCache{Cache: delegate}, CompareAndSwapCache: cas}
	})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-store-outage-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	inventory := gatewayInventoryFromPage(t, consentGetPageWithContext(t, ctx, ti, slug, stateID))
	attempt := inventory.Attempt
	form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {"approve"}, "tool_inventory_id": {attempt}, "tool_filtering": {"off"}, "tool_catalog_mode": {"cached"}}
	request := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	err = ti.service.ServeConsent(httptest.NewRecorder(), request, endpoint)
	var unavailable *oops.ShareableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, oops.CodeUnavailable, unavailable.Code)
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err, "publication failure must leave approval retryable")
}

// catalogPublicationFailureCache fails catalog publication while keeping the
// ordered gateway capture the consent page publishes.
type catalogPublicationFailureCache struct {
	failingConditionalCache
	cache.CompareAndSwapCache
}

type catalogCountingCache struct {
	cache.Cache
	cache.ConditionalCache
	cache.CompareAndSwapCache
	loads atomic.Int32
}

func (c *catalogCountingCache) Get(ctx context.Context, key string, value any) error {
	if strings.HasPrefix(key, "mcpConnectionCatalog:") {
		c.loads.Add(1)
	}
	if err := c.Cache.Get(ctx, key, value); err != nil {
		return fmt.Errorf("load counted cache: %w", err)
	}
	return nil
}

func TestConnectionCatalogProtocolLoadCost(t *testing.T) {
	t.Parallel()
	var counted *catalogCountingCache
	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		conditional, ok := delegate.(cache.ConditionalCache)
		require.True(t, ok)
		cas, ok := delegate.(cache.CompareAndSwapCache)
		require.True(t, ok)
		counted = &catalogCountingCache{Cache: delegate, ConditionalCache: conditional, CompareAndSwapCache: cas}
		return counted
	})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-cost-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 150)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
	// The fixture's migration wrapper is outside the production request path.
	ti.service.manual = true
	counted.loads.Store(0)
	beforeUpstream := upstream.requests.Load()
	for _, method := range []string{"initialize", "ping"} {
		params := map[string]any{}
		if method == "initialize" {
			params = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "fixture", "version": "1"}}
		}
		before := ti.conn.Stat().AcquireCount()
		response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, method, params), connection.token, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Zero(t, counted.loads.Load(), "protocol setup does not read the catalog")
		require.LessOrEqual(t, ti.conn.Stat().AcquireCount()-before, int64(20), "protocol setup DB work is independent of the catalog size")
	}
	beforeDB := ti.conn.Stat().AcquireCount()
	response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "source--tool_000")
	require.Equal(t, int32(1), counted.loads.Load(), "one immutable record per discovery request")
	require.LessOrEqual(t, ti.conn.Stat().AcquireCount()-beforeDB, int64(20), "discovery uses a single batch configuration check")
	require.Equal(t, beforeUpstream, upstream.requests.Load())
}

func seedExpiredCatalogCredential(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, subject urn.SessionSubject) *atomic.Int32 {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	var tokenRequests atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(tokenServer.Close)
	remoteClient := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "refresh-fixture", tokenServer.URL, []uuid.UUID{issuerID})
	access, err := ti.enc.Encrypt([]byte("expired"))
	require.NoError(t, err)
	refresh, err := ti.enc.Encrypt([]byte("refresh-fixture"))
	require.NoError(t, err)
	_, err = remotesessionsrepo.New(ti.conn).UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{SubjectUrn: subject, UserSessionIssuerID: issuerID, RemoteSessionClientID: remoteClient, Scopes: []string{}, AccessTokenEncrypted: access, AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}, RefreshTokenEncrypted: pgtype.Text{String: refresh, Valid: true}, RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, Resource: pgtype.Text{String: "https://other.example.test/mcp", Valid: true}})
	require.NoError(t, err)
	return &tokenRequests
}

func TestConnectionCatalogHostedDiscoveryRechecksPermissionsWithoutTokens(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-hosted-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted", 1, mcpservers.VisibilityPublic, "allowed", "denied")
	setToolsetMcpPrivate(t, ctx, ti, member.toolsetID, *auth.ProjectID)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
	tokens := seedExpiredCatalogCredential(t, ctx, ti, issuer.ID, urn.NewUserSubject(mockidp.MockUserID))
	selector := authz.NewSelector(authz.ScopeMCPConnect, member.toolsetID.String())
	selector[authz.SelectorKeyTool] = "allowed"
	grants := []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, gateway.ID.String()), authz.NewGrantWithSelector(authz.ScopeMCPConnect, selector)}
	caller := authztest.WithExactGrants(t, t.Context(), grants...)
	response, err := servePublicHTTP(t, caller, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), member.slug+"--allowed")
	require.NotContains(t, response.Body.String(), member.slug+"--denied")
	selector[authz.SelectorKeyTool] = "other"
	caller = authztest.WithExactGrants(t, t.Context(), authz.NewGrant(authz.ScopeMCPConnect, gateway.ID.String()), authz.NewGrantWithSelector(authz.ScopeMCPConnect, selector))
	response, err = servePublicHTTP(t, caller, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.NoError(t, err)
	require.NotContains(t, response.Body.String(), member.slug+"--allowed")
	require.Zero(t, tokens.Load())
}

func TestGatewayConsentUnavailableOutcomes(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"timeout", "server_error"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPServiceWithMetaRuntime(t, testenv.NewMeterProvider(t), mcp.MetaRuntimeConfig{MemberCallTimeout: 100 * time.Millisecond})
			auth, _ := contextvalues.GetAuthContext(ctx)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "catalog-unavailable-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
			failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "timeout" {
					select {
					case <-r.Context().Done():
					case <-time.After(250 * time.Millisecond):
					}
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(failed.Close)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Failed", "failed", 1, failed.URL)
			stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
			outcomes := gatewayOutcomesFromPage(consentGetPageWithContext(t, ctx, ti, slug, stateID))
			require.Len(t, outcomes, 1)
			require.Equal(t, "failed", outcomes[0].Slug)
			require.Equal(t, "unavailable", outcomes[0].Status)
		})
	}
}

// Progressive discovery lists only the servers with an approved tool, the same
// servers Direct listing exposes for a Specific tools consent.
func TestConnectionCatalogProgressiveListsOnlySelectedServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-progressive-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Chosen", "chosen", 1, newCatalogUpstream(t, 2).url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Other", "other", 2, newCatalogUpstream(t, 2).url)
	specific := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive, []string{"chosen--tool_000"})
	all := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive, nil)

	listed := callGatewayTool(t, ti, slug, specific.token, "list_servers", map[string]any{})
	require.Contains(t, listed, `\"slug\":\"chosen\"`)
	require.NotContains(t, listed, `\"slug\":\"other\"`)
	require.Contains(t, callGatewayTool(t, ti, slug, specific.token, "describe_server", map[string]any{"server": "other"}), "unknown server")
	require.Contains(t, callGatewayTool(t, ti, slug, all.token, "list_servers", map[string]any{}), `\"slug\":\"other\"`)
}

func TestConnectionCatalogConfigurationChangeStopsAdvertisingTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-config-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
	progressive := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive, nil)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: memberID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	remote, err := remotemcprepo.New(ti.conn).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: server.RemoteMcpServerID.UUID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	replacement := newCatalogUpstream(t, 1)
	_, err = remotemcprepo.New(ti.conn).UpdateServer(ctx, remotemcprepo.UpdateServerParams{ID: remote.ID, ProjectID: remote.ProjectID, Name: remote.Name, Slug: remote.Slug, TransportType: remote.TransportType, Url: replacement.url})
	require.NoError(t, err)
	before := upstream.requests.Load()
	listed, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.NoError(t, err)
	require.NotContains(t, listed.Body.String(), `"name":"source--tool_000"`)
	require.Equal(t, before, upstream.requests.Load())
	require.Zero(t, replacement.requests.Load())
	require.Contains(t, callGatewayTool(t, ti, slug, connection.token, "source--tool_000", map[string]any{}), "configuration changed")
	require.Zero(t, replacement.calls.Load())
	status, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "connection_status", "arguments": map[string]any{}}), progressive.token, nil)
	require.NoError(t, err)
	require.Contains(t, status.Body.String(), "configuration changed")

}

// The transport is part of a member's approved route: switching it leaves the
// catalog unusable until consent is given again.
func TestConnectionCatalogTransportChangeRequiresConsent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-transport-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: memberID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	remote, err := remotemcprepo.New(ti.conn).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: server.RemoteMcpServerID.UUID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.NotEqual(t, "sse", remote.TransportType)
	_, err = remotemcprepo.New(ti.conn).UpdateServer(ctx, remotemcprepo.UpdateServerParams{ID: remote.ID, ProjectID: remote.ProjectID, Name: remote.Name, Slug: remote.Slug, TransportType: "sse", Url: remote.Url})
	require.NoError(t, err)
	require.Contains(t, callGatewayTool(t, ti, slug, connection.token, "source--tool_000", map[string]any{}), "configuration changed")
	require.Zero(t, upstream.calls.Load())
}

// A cached connection reports the members its consent did not approve,
// whether unavailable at consent or added since, as needing new consent,
// while discovery still offers only the approved member.
func TestConnectionCatalogStatusReportsUnapprovedMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-unapproved-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, newCatalogUpstream(t, 1).url)
	broken := newCatalogUpstream(t, 1)
	broken.unavailable.Store(true)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Broken", "broken", 2, broken.url)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive, nil)
	broken.unavailable.Store(false)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Added", "added", 3, newCatalogUpstream(t, 1).url)

	status := callGatewayTool(t, ti, slug, connection.token, "connection_status", map[string]any{})
	require.Contains(t, status, "Source: verified at consent")
	require.Contains(t, status, "Broken: new consent required")
	require.Contains(t, status, "Added: new consent required")
	listed := callGatewayTool(t, ti, slug, connection.token, "list_servers", map[string]any{})
	require.Contains(t, listed, `\"slug\":\"source\"`)
	require.NotContains(t, listed, `\"slug\":\"broken\"`)
	require.NotContains(t, listed, `\"slug\":\"added\"`)
	require.Contains(t, callGatewayTool(t, ti, slug, connection.token, "describe_server", map[string]any{"server": "added"}), "unknown server")
}

func TestGatewayConsentNoAccessRetainsRequestAccessLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-no-access-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	denied := authztest.WithExactGrants(t, ctx)
	page := consentGetPageWithContext(t, denied, ti, slug, stateID)
	require.Contains(t, page, "You do not have access to this gateway")
	require.Contains(t, page, ">Request access</a>")
	require.NotContains(t, page, `id="consent-tools-root"`)
	require.Zero(t, upstream.requests.Load())
}

func TestGatewayConsentLockdownPreventsCaptureAndApproval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-lockdown-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{OrganizationID: auth.ActiveOrganizationID, Domain: "consent.example.test", ProvisionerKind: "ingress", IpAllowlist: []string{"203.0.113.0/24"}})
	require.NoError(t, err)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	page := consentGetPageWithContext(t, ctx, ti, slug, stateID)
	require.Contains(t, page, "custom domain is not ready")
	require.NotContains(t, page, `href="https://consent.example.test`)
	_, err = customdomainsrepo.New(ti.conn).SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	page = consentGetPageWithContext(t, ctx, ti, slug, stateID)
	require.Contains(t, page, "custom domain is not ready", "verification alone does not activate a domain")
	_, err = customdomainsrepo.New(ti.conn).ActivateVerifiedCustomDomain(ctx, customdomainsrepo.ActivateVerifiedCustomDomainParams{ID: domain.ID, IngressName: pgtype.Text{String: "fixture", Valid: true}, CertSecretName: pgtype.Text{String: "fixture-cert", Valid: true}, ProvisionerKind: "ingress"})
	require.NoError(t, err)
	page = consentGetPageWithContext(t, ctx, ti, slug, stateID)
	require.Contains(t, page, "Reconnect your AI client")
	require.Contains(t, page, `href="https://consent.example.test/mcp/`+slug+`"`)
	require.NotContains(t, page, `id="consent-tools-root"`)
	require.Zero(t, upstream.requests.Load(), "lockdown prohibits discovery on this origin")
	for _, action := range []string{"identity_chaining_check", "validate"} {
		form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {action}}
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect/remote-session", strings.NewReader(form.Encode())).WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		err := ti.service.ServeConsentAction(httptest.NewRecorder(), req, endpoint)
		var denied *oops.ShareableError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, oops.CodeForbidden, denied.Code)
	}
	require.Zero(t, upstream.requests.Load())

	form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {"approve"}, "tool_inventory_id": {uuid.NewString()}, "tool_catalog_mode": {"cached"}, "tool_filtering": {"off"}}
	request := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode())).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	err = ti.service.ServeConsent(response, request, endpoint)
	var denied *oops.ShareableError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, oops.CodeForbidden, denied.Code)
	require.Zero(t, upstream.requests.Load())
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: *auth.ProjectID, CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true}, MetaMcpServerID: uuid.NullUUID{UUID: gateway.ID, Valid: true}, Slug: slug})
	require.NoError(t, err)
	admitted := customdomains.WithContext(ctx, &customdomains.Context{OrganizationID: auth.ActiveOrganizationID, Domain: domain.Domain, DomainID: domain.ID})
	admitted = requestorigin.WithContext(admitted, requestorigin.Origin{Surface: requestorigin.SurfaceCustomDomain, BaseURL: "https://" + domain.Domain, OrganizationID: auth.ActiveOrganizationID})
	domainEndpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(admitted, ti.logger, slug, "mcp")
	require.NoError(t, err)
	admittedState, _ := seedGatewayConsentChallenge(t, admitted, ti, issuer.ID, client, gateway.ID, slug)
	state, err := ti.authnChallengeCache.Get(admitted, "authnChallenge:"+admittedState)
	require.NoError(t, err)
	state.Endpoint, err = domainEndpoint.EndpointRef(admitted, ti.conn, "https://"+domain.Domain)
	require.NoError(t, err)
	require.NoError(t, ti.authnChallengeCache.Store(admitted, state))
	inventory := gatewayInventoryFromPage(t, consentGetPageWithContext(t, admitted, ti, slug, admittedState))
	require.Len(t, inventory.Tools, 1, "admission on the owning custom domain permits capture")
	require.Positive(t, upstream.requests.Load())
}

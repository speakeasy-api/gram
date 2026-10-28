package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestCodeConnectionsWorkWithoutToolPermission(t *testing.T) {
	t.Parallel()
	mock := memberMock()
	mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	auth, _ := contextvalues.GetAuthContext(ctx)
	mock.upsertResult = auth.UserID
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "code-connect-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	seedMetaMember(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Tracker", "tracker", 0, "public")
	token := mintGatewayTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode)
	var action *codemode.ConnectionAction
	executor := &recordingCodeExecutor{run: func(runCtx context.Context, _ string, host codemode.Host) (*codemode.ExecutionResult, error) {
		status, err := host.Servers(runCtx, codemode.PageArgs{})
		require.NoError(t, err)
		require.Len(t, status.Items, 1)
		action, err = host.Connect(runCtx, "tracker")
		require.NoError(t, err)
		again, err := host.Connect(runCtx, "tracker")
		require.NoError(t, err)
		require.Equal(t, action.AuthorizationURL, again.AuthorizationURL)
		_, err = host.Connect(runCtx, "hidden-or-absent")
		require.Error(t, err)
		_, err = host.Call(runCtx, "tracker--ping", json.RawMessage(`{}`))
		require.Error(t, err)
		return &codemode.ExecutionResult{}, nil
	}}
	mcp.SetTestCodeExecutor(ti.service.Service, executor)
	enableCodeMode(t, ctx, ti)
	listing, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
	require.NoError(t, err)
	require.Contains(t, listing.Body.String(), "tools.servers")
	require.Contains(t, listing.Body.String(), "tools.connect")
	response, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "recovery", "tools": []string{}}}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NotNil(t, action)
	require.NotEmpty(t, action.AuthorizationURL)
	require.NotContains(t, action.AuthorizationURL, token)
	link, err := url.Parse(action.AuthorizationURL)
	require.NoError(t, err)
	ticketID := link.Query().Get("code_reconnect")
	ticket, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+ticketID)
	require.NoError(t, err)
	require.Equal(t, auth.UserID, ticket.CodeReconnect.UserID)
	require.True(t, ticket.CodeReconnect.Ticket)
	// A forged focus parameter cannot redirect this ticket to another member.
	query := link.Query()
	query.Set("server", "forged")
	link.RawQuery = query.Encode()
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", slug)
	request := httptest.NewRequest(http.MethodGet, link.String(), nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	started := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleFirstPartyConnect(started, request))
	require.Equal(t, http.StatusFound, started.Code)
	require.Equal(t, mock.buildAuthURLResult.String(), started.Header().Get("Location"))
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+mock.buildAuthURLParams.State)
	require.NoError(t, err)
	require.Equal(t, "tracker", state.FocusServer)
	require.False(t, state.CodeReconnect.Ticket)
	require.Error(t, ti.service.HandleFirstPartyConnect(httptest.NewRecorder(), request), "entry ticket must be single-use")
	// The wrong browser user fails before any sign-in or credential handoff.
	mock.upsertResult = "different-user"
	callback := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?state="+state.ID+"&code=idp-auth-code", nil).WithContext(ctx)
	rejected := httptest.NewRecorder()
	require.ErrorContains(t, ti.service.HandleIDPCallback(rejected, callback), "Only the user who requested this connection")
	require.Empty(t, rejected.Header().Get("Location"))
	require.Empty(t, mock.memberChecks)
}

func TestCodeConnectionsRecoverExpiredCatalogue(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "code-expired-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Tracker", "tracker", 1, upstream.url)
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
	enableCodeMode(t, ctx, ti)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)
	require.NoError(t, ti.cacheAdapter.Delete(ctx, "mcpConnectionCatalog:v1:"+auth.ActiveOrganizationID+":"+connection.policy.Catalog.ID.String()))
	before := upstream.requests.Load()
	ran := false
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{run: func(runCtx context.Context, _ string, host codemode.Host) (*codemode.ExecutionResult, error) {
		ran = true
		status, err := host.Servers(runCtx, codemode.PageArgs{})
		require.NoError(t, err)
		require.Equal(t, "required", status.Items[0].Connection.Consent)
		action, err := host.Connect(runCtx, "tracker")
		require.NoError(t, err)
		require.True(t, action.ClientReconnectRequired)
		require.NotEmpty(t, action.AuthorizationURL)
		require.Contains(t, action.Instructions, "reconnect this gateway")
		_, err = host.Call(runCtx, "tracker--tool_000", json.RawMessage(`{}`))
		require.Error(t, err)
		return &codemode.ExecutionResult{}, nil
	}})
	response, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "recovery"}}), connection.token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.True(t, ran, response.Body.String())
	require.Equal(t, before, upstream.requests.Load(), "recovery must not contact member tools")
}

func TestCodeReconnectExpiry(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	_, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	slug := "expired-link-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	ref, err := endpoint.EndpointRef(ctx, ti.conn, ti.serverURL.String())
	require.NoError(t, err)
	state := mcp.AuthnChallengeState{ID: uuid.NewString(), FirstParty: true, CreatedAt: time.Now(), Endpoint: ref, UserSessionIssuerID: issuer.ID, CodeReconnect: &mcp.CodeReconnectBinding{UserID: auth.UserID, Server: "tracker", ServerID: uuid.New(), Ticket: true, ExpiresAt: time.Now().Add(-time.Minute)}}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	request := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect/first-party?code_reconnect="+state.ID, nil).WithContext(ctx)
	response := httptest.NewRecorder()
	require.ErrorContains(t, ti.service.ServeFirstPartyConnect(response, request, endpoint), "invalid or expired connection request")
	require.Empty(t, response.Header().Get("Location"))
}

func TestCodeServersExposeConsentWithoutGrantingTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "code-inventory-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Approved", "approved", 1, upstream.url)
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
	enableCodeMode(t, ctx, ti)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Unapproved", "unapproved", 2, upstream.url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Hidden", "hidden", 3, upstream.url)
	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, auth.ActiveOrganizationID, auth.UserID, mcpServerBySlug(t, ctx, ti, *auth.ProjectID, "hidden")))
	before := upstream.requests.Load()
	ran := false
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{run: func(runCtx context.Context, _ string, host codemode.Host) (*codemode.ExecutionResult, error) {
		ran = true
		page, err := host.Servers(runCtx, codemode.PageArgs{})
		require.NoError(t, err)
		require.Len(t, page.Items, 2)
		require.Equal(t, "approved", page.Items[0].Slug)
		require.Equal(t, "approved", page.Items[0].Connection.Consent)
		require.Equal(t, "verified_at_consent", page.Items[0].Connection.Authentication)
		require.Equal(t, "unapproved", page.Items[1].Slug)
		require.Equal(t, "required", page.Items[1].Connection.Consent)
		require.Equal(t, before, upstream.requests.Load(), "inventory must not probe upstream")
		_, err = host.Call(runCtx, "unapproved--tool_000", json.RawMessage(`{}`))
		require.Error(t, err)
		_, err = host.Connect(runCtx, "hidden")
		require.Error(t, err)
		return &codemode.ExecutionResult{}, nil
	}})
	response, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "inventory", "tools": []string{}}}), connection.token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.True(t, ran, response.Body.String())
	require.Equal(t, before, upstream.requests.Load())
}

func TestCodeReconnectSetupFailurePreservesTicket(t *testing.T) {
	t.Parallel()
	mock := memberMock()
	mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "setup-retry-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	ref, err := endpoint.EndpointRef(ctx, ti.conn, ti.serverURL.String())
	require.NoError(t, err)
	ticket := mcp.AuthnChallengeState{ID: uuid.NewString(), FirstParty: true, CreatedAt: time.Now(), Endpoint: ref, UserSessionIssuerID: issuer, CodeReconnect: &mcp.CodeReconnectBinding{UserID: auth.UserID, Server: "tracker", ServerID: uuid.New(), Ticket: true, ExpiresAt: time.Now().Add(time.Minute)}}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, ticket))
	target := "/mcp/" + slug + "/connect/first-party?code_reconnect=" + ticket.ID
	for _, conflict := range []string{"workload_reconnect", "binding_refresh_attempt", "reauthenticate_client_id"} {
		req := httptest.NewRequest(http.MethodGet, target+"&"+conflict+"=conflict", nil).WithContext(ctx)
		require.ErrorContains(t, ti.service.ServeFirstPartyConnect(httptest.NewRecorder(), req, endpoint), "conflicting connection requests")
		_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+ticket.ID)
		require.NoError(t, err, "conflict must not consume ticket")
	}
	mock.buildAuthURLErr = errors.New("synthetic provider unavailable")
	req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
	require.ErrorContains(t, ti.service.ServeFirstPartyConnect(httptest.NewRecorder(), req, endpoint), "build IDP authorization URL")
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+ticket.ID)
	require.NoError(t, err, "setup failure must not consume ticket")
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+mock.buildAuthURLParams.State)
	require.Error(t, err, "failed setup must discard its prepared challenge")
	mock.buildAuthURLErr = nil
	response := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeFirstPartyConnect(response, req, endpoint))
	require.Equal(t, http.StatusFound, response.Code)
	require.ErrorContains(t, ti.service.ServeFirstPartyConnect(httptest.NewRecorder(), req, endpoint), "connection request expired")
}

func TestCodeReconnectConsentGETKeepsBoundFocus(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "focus-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	seedMetaMember(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Bound tracker", "tracker", 0, "public")
	seedMetaMember(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Other server", "other", 1, "public")
	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	subject := urn.NewUserSubject(auth.UserID)
	state.Subject = &subject
	state.AuthorizerUserID = auth.UserID
	state.FirstParty = true
	state.FocusServer = "tracker"
	state.AutoConnectDone = true
	state.CodeReconnect = &mcp.CodeReconnectBinding{UserID: auth.UserID, Server: "tracker", ServerID: mcpServerBySlug(t, ctx, ti, *auth.ProjectID, "tracker"), ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect?state="+stateID+"&server=other", nil).WithContext(ctx)
	require.NoError(t, ti.service.ServeConsent(response, req, endpoint))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "Bound tracker")
	require.NotContains(t, response.Body.String(), "Other server")
}

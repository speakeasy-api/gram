package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func runCodeHost(t *testing.T, ctx context.Context, ti *testInstance, slug, bearer string, fn func(context.Context, codemode.Host)) {
	t.Helper()
	ran := false
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{run: func(runCtx context.Context, _ string, host codemode.Host) (*codemode.ExecutionResult, error) {
		ran = true
		fn(runCtx, host)
		return &codemode.ExecutionResult{}, nil
	}})
	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "check"}}), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, ran, w.Body.String())
}

func startCodeReconnectBrowserFlow(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	mock *mockIdentityResolver,
	slug string,
	authorizationURL string,
) (stateID string, browserCookie *http.Cookie, landing *url.URL, idpErr error) {
	t.Helper()
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", slug)
	entry := httptest.NewRequest(http.MethodGet, authorizationURL, nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	started := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleFirstPartyConnect(started, entry))
	require.Equal(t, http.StatusFound, started.Code)
	require.Equal(t, mock.buildAuthURLResult.String(), started.Header().Get("Location"))
	// The entry ticket is single-use.
	require.Error(t, ti.service.HandleFirstPartyConnect(httptest.NewRecorder(), entry))

	cookies := started.Result().Cookies()
	require.Len(t, cookies, 1)
	callback := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?state="+url.QueryEscape(mock.buildAuthURLParams.State)+"&code=idp-auth-code", nil).WithContext(ctx)
	callback.AddCookie(cookies[0])
	identified := httptest.NewRecorder()
	if err := ti.service.HandleIDPCallback(identified, callback); err != nil {
		return "", cookies[0], nil, fmt.Errorf("handle idp callback: %w", err)
	}
	require.Equal(t, http.StatusFound, identified.Code)
	parsed, err := url.Parse(identified.Header().Get("Location"))
	require.NoError(t, err)
	return parsed.Query().Get("state"), cookies[0], parsed, nil
}

func TestCodeReconnectNoUpstreamSignInMemberReviewsAndUpdatesCatalog(t *testing.T) {
	t.Parallel()

	t.Run("tunneled member approves all cached tools", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-noauth-all-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 2)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)

		// Attach a new no-upstream-sign-in tunneled member after consent.
		tunnelGateway := &fakeTunnelGateway{t: t, toolNames: []string{"ping"}, agentSessionID: "agent-noauth", backendSessionID: "backend-noauth"}
		tunnelServer := httptest.NewServer(tunnelGateway)
		t.Cleanup(tunnelServer.Close)
		tunnelID, _ := seedTunneledMetaMember(t, ctx, ti, *auth.ProjectID, gateway.ID, "Database Tunnel", "db-tunnel", 2, "")
		require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), tunnelServer.URL, time.Hour))
		tunnelServerID := tunneledFixtureMember(t, ctx, ti, *auth.ProjectID, gateway.ID, "db-tunnel")

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			page, err := host.Servers(runCtx, codemode.PageArgs{})
			require.NoError(t, err)
			bySlug := map[string]*codemode.ConnectionState{}
			for _, item := range page.Items {
				bySlug[item.Slug] = item.Connection
			}
			require.Equal(t, "approved", bySlug["existing"].Consent)
			require.Equal(t, "verified_at_consent", bySlug["existing"].Authentication)
			require.Equal(t, "required", bySlug["db-tunnel"].Consent)

			verified, err := host.Servers(runCtx, codemode.PageArgs{Server: "db-tunnel", Verify: true})
			require.NoError(t, err)
			require.Len(t, verified.Items, 1)
			require.Equal(t, "connected", verified.Items[0].Connection.Authentication)
			require.Equal(t, "required", verified.Items[0].Connection.Consent)

			_, err = host.Call(runCtx, "db-tunnel--ping", json.RawMessage(`{}`))
			require.Error(t, err)

			action, err = host.Connect(runCtx, "db-tunnel")
			require.NoError(t, err)
			require.False(t, action.ClientReconnectRequired)
			require.Equal(t, "consent_required", action.Reason)
			require.Contains(t, action.Instructions, "review and approve the refreshed gateway tools")
			require.NotEmpty(t, action.AuthorizationURL)
		})

		claims, err := usersessions.NewSigner("test-jwt-secret").Validate(connection.token, urn.NewUserSessionIssuer(issuer.ID).String())
		require.NoError(t, err)
		beforePolicyRow, err := usersessionsrepo.New(ti.conn).GetUserSessionPolicyByJTI(ctx, usersessionsrepo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
		require.NoError(t, err)
		beforeCatalogKey := "mcpConnectionCatalog:v1:" + auth.ActiveOrganizationID + ":" + connection.policy.Catalog.ID.String()
		var originalCatalog []byte
		require.NoError(t, ti.cacheAdapter.Get(ctx, beforeCatalogKey, &originalCatalog))

		rec := recordConnections(t, ti)
		stateID, browserCookie, _, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.NoError(t, idpErr)

		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
		require.NoError(t, err)
		require.NotNil(t, state.CodeReconnect)
		require.True(t, state.CodeReconnect.ReviewReady, "no-sign-in member opens catalog review right after IdP sign-in")
		require.False(t, state.CodeReconnect.CatalogComplete)

		// Opening the link alone must not alter the approved catalog.
		var beforeApprovalCatalog []byte
		require.NoError(t, ti.cacheAdapter.Get(ctx, beforeCatalogKey, &beforeApprovalCatalog))
		require.Equal(t, originalCatalog, beforeApprovalCatalog)

		reviewPage := consentGetPageWithContext(t, ctx, ti, slug, stateID, browserCookie)
		require.Contains(t, reviewPage, "Review updated tools")
		require.Contains(t, reviewPage, "data-catalog-changes")
		require.Contains(t, reviewPage, `data-gateway="true"`)
		require.Contains(t, reviewPage, `id="consent-approve-button"`)
		require.Contains(t, reviewPage, "db-tunnel--ping")
		require.NotContains(t, reviewPage, "needs no sign-in, so it works through this gateway with nothing to connect")
		require.NotContains(t, reviewPage, "data-gateway-back")
		rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultReviewShown)
		rec.RequireNotEnded()

		// Another browser cannot view or approve the review.
		endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
		require.NoError(t, err)
		for _, badCookie := range []*http.Cookie{nil, {Name: browserCookie.Name, Value: "wrong-browser"}} {
			getReq := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect?state="+stateID, nil).WithContext(ctx)
			postReq := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(url.Values{"state": {stateID}, "csrf_token": {state.CSRFToken}, "action": {"approve"}, "tool_filtering": {"off"}, "tool_catalog_mode": {"cached"}}.Encode())).WithContext(ctx)
			postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for _, req := range []*http.Request{getReq, postReq} {
				if badCookie != nil {
					req.AddCookie(badCookie)
				}
				require.ErrorContains(t, ti.service.ServeConsent(httptest.NewRecorder(), req, endpoint), "invalid consent browser binding")
			}
		}

		// Approve the refreshed catalog.
		inventory := gatewayInventoryFromPage(t, reviewPage)
		approveForm := url.Values{
			"state":             {stateID},
			"csrf_token":        {state.CSRFToken},
			"action":            {"approve"},
			"tool_inventory_id": {inventory.Attempt},
			"tool_filtering":    {"off"},
			"tool_catalog_mode": {"cached"},
		}
		approveReq := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(approveForm.Encode())).WithContext(ctx)
		approveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		approveReq.AddCookie(browserCookie)
		approveResp := httptest.NewRecorder()
		require.NoError(t, ti.service.ServeConsent(approveResp, approveReq, endpoint))
		require.Equal(t, http.StatusSeeOther, approveResp.Code, approveResp.Body.String())

		// Post-approval landing auto-closes without showing the old dead-end copy.
		donePage := consentGetPageWithContext(t, ctx, ti, slug, stateID, browserCookie)
		require.Contains(t, donePage, "data-auto-close")
		require.Contains(t, donePage, "Connection complete. This tab will close automatically.")
		require.NotContains(t, donePage, "data-gateway-back")

		require.NoError(t, ti.service.DrainConnectionEndings(ctx))
		row := rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.ResultComplete)
		require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)

		// Original catalog expires_at is preserved verbatim and no_auth is recorded.
		afterPolicyRow, err := usersessionsrepo.New(ti.conn).GetUserSessionPolicyByJTI(ctx, usersessionsrepo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
		require.NoError(t, err)
		afterPolicy, err := toolfilter.ParseSessionPolicy(afterPolicyRow.ToolSelection)
		require.NoError(t, err)
		var updatedCatalog []byte
		require.NoError(t, ti.cacheAdapter.Get(ctx, "mcpConnectionCatalog:v1:"+auth.ActiveOrganizationID+":"+afterPolicy.Catalog.ID.String(), &updatedCatalog))
		var beforeJSON, afterJSON map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(originalCatalog, &beforeJSON))
		require.NoError(t, json.Unmarshal(updatedCatalog, &afterJSON))
		require.Equal(t, beforeJSON["expires_at"], afterJSON["expires_at"])
		require.NotEqual(t, beforeJSON["id"], afterJSON["id"])

		choices, err := upstreamclientsrepo.New(ti.conn).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{
			OrganizationID: auth.ActiveOrganizationID,
			CredentialKind: upstreamclients.CredentialKindUserSession,
			CredentialID:   beforePolicyRow.CredentialID,
			McpServerIds:   []uuid.UUID{tunnelServerID},
		})
		require.NoError(t, err)
		require.Len(t, choices, 1)
		require.True(t, choices[0].NoAuth)

		// Fresh execution on the existing token discovers, describes, and calls both existing and newly approved tools.
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			servers, err := host.Servers(runCtx, codemode.PageArgs{})
			require.NoError(t, err)
			for _, item := range servers.Items {
				require.Equal(t, "approved", item.Connection.Consent)
				require.Equal(t, "verified_at_consent", item.Connection.Authentication)
			}

			search, err := host.Search(runCtx, codemode.SearchArgs{Query: "", Limit: 50})
			require.NoError(t, err)
			require.False(t, search.Incomplete)
			paths := make([]string, 0, len(search.Items))
			for _, item := range search.Items {
				paths = append(paths, item.Path)
			}
			require.ElementsMatch(t, []string{"existing--tool_000", "existing--tool_001", "db-tunnel--ping"}, paths)

			scoped, err := host.Search(runCtx, codemode.SearchArgs{Query: "", Server: "db-tunnel", Limit: 10})
			require.NoError(t, err)
			require.Len(t, scoped.Items, 1)
			require.Equal(t, "db-tunnel--ping", scoped.Items[0].Path)

			described, err := host.Describe(runCtx, "db-tunnel--ping")
			require.NoError(t, err)
			require.Equal(t, "db-tunnel--ping", described.Path)

			tunnelRes, err := host.Call(runCtx, "db-tunnel--ping", json.RawMessage(`{}`))
			require.NoError(t, err)
			require.True(t, tunnelRes.OK)
			require.Contains(t, string(tunnelRes.Content), "pong through the tunnel")

			existingRes, err := host.Call(runCtx, "existing--tool_000", json.RawMessage(`{}`))
			require.NoError(t, err)
			require.True(t, existingRes.OK)
			require.Contains(t, string(existingRes.Content), "executed live")
		})
	})

	t.Run("tunneled member preserves specific tool selection", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-noauth-sel-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 2)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, []string{"existing--tool_000"})

		tunnelGateway := &fakeTunnelGateway{t: t, toolNames: []string{"ping"}, agentSessionID: "agent-sel", backendSessionID: "backend-sel"}
		tunnelServer := httptest.NewServer(tunnelGateway)
		t.Cleanup(tunnelServer.Close)
		tunnelID, _ := seedTunneledMetaMember(t, ctx, ti, *auth.ProjectID, gateway.ID, "Database Tunnel", "db-tunnel", 2, "")
		require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), tunnelServer.URL, time.Hour))

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			var err error
			action, err = host.Connect(runCtx, "db-tunnel")
			require.NoError(t, err)
			require.False(t, action.ClientReconnectRequired)
		})

		stateID, browserCookie, _, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.NoError(t, idpErr)
		reviewPage := consentGetPageWithContext(t, ctx, ti, slug, stateID, browserCookie)
		require.Contains(t, reviewPage, "existing--tool_000")
		require.Contains(t, reviewPage, "db-tunnel--ping")

		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
		require.NoError(t, err)
		inventory := gatewayInventoryFromPage(t, reviewPage)
		endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
		require.NoError(t, err)
		approveForm := url.Values{
			"state":             {stateID},
			"csrf_token":        {state.CSRFToken},
			"action":            {"approve"},
			"tool_inventory_id": {inventory.Attempt},
			"tool_filtering":    {"on"},
			"tool_catalog_mode": {"cached"},
			"tools":             {"existing--tool_000", "db-tunnel--ping"},
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(approveForm.Encode())).WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(browserCookie)
		resp := httptest.NewRecorder()
		require.NoError(t, ti.service.ServeConsent(resp, req, endpoint))
		require.Equal(t, http.StatusSeeOther, resp.Code, resp.Body.String())

		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			search, err := host.Search(runCtx, codemode.SearchArgs{Query: "", Limit: 50})
			require.NoError(t, err)
			paths := make([]string, 0, len(search.Items))
			for _, item := range search.Items {
				paths = append(paths, item.Path)
			}
			require.ElementsMatch(t, []string{"existing--tool_000", "db-tunnel--ping"}, paths)

			_, err = host.Call(runCtx, "existing--tool_001", json.RawMessage(`{}`))
			require.Error(t, err, "unselected tool must remain blocked")

			res, err := host.Call(runCtx, "db-tunnel--ping", json.RawMessage(`{}`))
			require.NoError(t, err)
			require.True(t, res.OK)
			require.Contains(t, string(res.Content), "pong through the tunnel")
		})
	})

	t.Run("hosted no-sign-in member approves refreshed catalog", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-hosted-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)

		hosted := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted Tools", 2, "public", "healthcheck")

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			var err error
			action, err = host.Connect(runCtx, hosted.slug)
			require.NoError(t, err)
			require.False(t, action.ClientReconnectRequired)
		})

		stateID, browserCookie, _, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.NoError(t, idpErr)
		reviewPage := consentGetPageWithContext(t, ctx, ti, slug, stateID, browserCookie)
		require.Contains(t, reviewPage, "Review updated tools")
		require.Contains(t, reviewPage, hosted.slug+"--healthcheck")

		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
		require.NoError(t, err)
		inventory := gatewayInventoryFromPage(t, reviewPage)
		endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
		require.NoError(t, err)
		approveForm := url.Values{
			"state":             {stateID},
			"csrf_token":        {state.CSRFToken},
			"action":            {"approve"},
			"tool_inventory_id": {inventory.Attempt},
			"tool_filtering":    {"off"},
			"tool_catalog_mode": {"cached"},
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(approveForm.Encode())).WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(browserCookie)
		resp := httptest.NewRecorder()
		require.NoError(t, ti.service.ServeConsent(resp, req, endpoint))
		require.Equal(t, http.StatusSeeOther, resp.Code, resp.Body.String())

		donePage := consentGetPageWithContext(t, ctx, ti, slug, stateID, browserCookie)
		require.Contains(t, donePage, "data-auto-close")

		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			search, err := host.Search(runCtx, codemode.SearchArgs{Query: "", Server: hosted.slug, Limit: 10})
			require.NoError(t, err)
			require.Len(t, search.Items, 1)
			require.Equal(t, hosted.slug+"--healthcheck", search.Items[0].Path)

			described, err := host.Describe(runCtx, hosted.slug+"--healthcheck")
			require.NoError(t, err)
			require.Equal(t, hosted.slug+"--healthcheck", described.Path)
		})
	})

	t.Run("cancel preserves catalog and leaves new member unapproved", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-noauth-cancel-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)

		added := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Readonly DB", "readonly-db", 2, added.url)

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			var err error
			action, err = host.Connect(runCtx, "readonly-db")
			require.NoError(t, err)
			require.False(t, action.ClientReconnectRequired)
		})

		catalogKey := "mcpConnectionCatalog:v1:" + auth.ActiveOrganizationID + ":" + connection.policy.Catalog.ID.String()
		var beforeCatalog []byte
		require.NoError(t, ti.cacheAdapter.Get(ctx, catalogKey, &beforeCatalog))

		rec := recordConnections(t, ti)
		stateID, browserCookie, _, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.NoError(t, idpErr)
		_ = consentGetPageWithContext(t, ctx, ti, slug, stateID, browserCookie)

		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
		require.NoError(t, err)
		endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
		require.NoError(t, err)
		denyForm := url.Values{"state": {stateID}, "csrf_token": {state.CSRFToken}, "action": {"deny"}}
		denyReq := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(denyForm.Encode())).WithContext(ctx)
		denyReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		denyReq.AddCookie(browserCookie)
		denyResp := httptest.NewRecorder()
		require.NoError(t, ti.service.ServeConsent(denyResp, denyReq, endpoint))
		require.Contains(t, denyResp.Body.String(), "No tool changes were approved")

		var afterCatalog []byte
		require.NoError(t, ti.cacheAdapter.Get(ctx, catalogKey, &afterCatalog))
		require.Equal(t, beforeCatalog, afterCatalog)

		require.NoError(t, ti.service.DrainConnectionEndings(ctx))
		row := rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeReviewCanceled)
		require.Equal(t, string(mcpconnectionattempts.OutcomeDeclined), row.Outcome)

		reloaded := consentGetPageWithContext(t, ctx, ti, slug, stateID, browserCookie)
		require.NotContains(t, reloaded, "data-auto-close")
		require.NotContains(t, reloaded, "data-gateway-back")
		require.Contains(t, reloaded, "No tool changes were approved")
		require.NoError(t, ti.service.DrainConnectionEndings(ctx))
		for _, recorded := range rec.Rows() {
			require.NotEqual(t, mcpconnectionattempts.ResultComplete.String(), recorded.StepResult)
		}

		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			servers, err := host.Servers(runCtx, codemode.PageArgs{Server: "readonly-db"})
			require.NoError(t, err)
			require.Len(t, servers.Items, 1)
			require.Equal(t, "required", servers.Items[0].Connection.Consent)
			_, err = host.Call(runCtx, "readonly-db--tool_000", json.RawMessage(`{}`))
			require.Error(t, err)
		})
	})

	t.Run("wrong user rejected at IdP callback", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-noauth-wronguser-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)
		added := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Readonly DB", "readonly-db", 2, added.url)

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			var err error
			action, err = host.Connect(runCtx, "readonly-db")
			require.NoError(t, err)
		})

		mock.upsertResult = "different-user-" + uuid.NewString()[:8]
		_, _, _, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.ErrorContains(t, idpErr, "Only the user who requested this connection")
	})

	t.Run("evicted catalog falls back cleanly at IdP callback", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-noauth-evict-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)
		added := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Readonly DB", "readonly-db", 2, added.url)

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			var err error
			action, err = host.Connect(runCtx, "readonly-db")
			require.NoError(t, err)
			require.False(t, action.ClientReconnectRequired)
		})

		require.NoError(t, ti.cacheAdapter.Delete(ctx, "mcpConnectionCatalog:v1:"+auth.ActiveOrganizationID+":"+connection.policy.Catalog.ID.String()))

		rec := recordConnections(t, ti)
		_, browserCookie, landing, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.NoError(t, idpErr)
		require.Equal(t, "required", landing.Query().Get("catalog_reconnect"))

		endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, landing.String(), nil).WithContext(ctx)
		req.AddCookie(browserCookie)
		page := httptest.NewRecorder()
		require.NoError(t, ti.service.ServeConsent(page, req, endpoint))
		require.Contains(t, page.Body.String(), "Session expired. Please reconnect.")
		require.NotContains(t, page.Body.String(), "data-auto-close")
		require.NotContains(t, page.Body.String(), "data-gateway-back")
		require.NoError(t, ti.service.DrainConnectionEndings(ctx))
		rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeConnectionCatalogExpired)
	})

	t.Run("revoked session rejected at IdP callback", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-noauth-revoke-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)
		added := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Readonly DB", "readonly-db", 2, added.url)

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			var err error
			action, err = host.Connect(runCtx, "readonly-db")
			require.NoError(t, err)
		})

		claims, err := usersessions.NewSigner("test-jwt-secret").Validate(connection.token, urn.NewUserSessionIssuer(issuer.ID).String())
		require.NoError(t, err)
		session, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
		require.NoError(t, err)
		_, err = usersessionsrepo.New(ti.conn).RevokeUserSession(ctx, usersessionsrepo.RevokeUserSessionParams{ID: session.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
		require.NoError(t, err)

		rec := recordConnections(t, ti)
		_, _, _, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.ErrorContains(t, idpErr, "no longer active")
		require.NoError(t, ti.service.DrainConnectionEndings(ctx))
		rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeRefreshTokenRevoked)
	})

	t.Run("live catalog keeps client reconnect fallback and hides broken back link", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-noauth-live-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeLive, metamcp.DiscoveryModeCode, nil)
		added := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Readonly DB", "readonly-db", 2, added.url)

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			var err error
			action, err = host.Connect(runCtx, "readonly-db")
			require.NoError(t, err)
			require.True(t, action.ClientReconnectRequired)
			require.Contains(t, action.Instructions, "reconnect this gateway")
		})

		stateID, browserCookie, _, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.NoError(t, idpErr)
		page := consentGetPageWithContext(t, ctx, ti, slug, stateID, browserCookie)
		require.NotContains(t, page, "data-gateway-back")
		require.Contains(t, page, "Reconnect this gateway in your AI client to approve its tools.")
	})

	t.Run("proxied OAuth member still requires upstream sign-in before review", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, downstream := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-oauth-keep-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		oauthUpstream := newRecordingUpstream(t, "ping")
		provider := newConsentExchangeAS(t, nil, "token-1")
		remoteClient := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "keep-oauth", provider.URL)
		remoteIssuer := clientRemoteIssuerID(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, remoteClient)
		oauthMember := createMetaMember(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "tracker", oauthUpstream.url, conv.ToNullUUID(remoteIssuer), 2)
		selection := selectionFixture{ti: ti, projectID: *auth.ProjectID, orgID: auth.ActiveOrganizationID}
		selection.associate(t, ctx, oauthMember.mcpServerID, remoteClient)
		seedTypedSessionClient(t, ctx, ti, auth.ActiveOrganizationID, *auth.ProjectID, oauthMember.mcpServerID, remoteClient, "remote_session", urn.NewUserSubject(auth.UserID))

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, downstream, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			var err error
			action, err = host.Connect(runCtx, "tracker")
			require.NoError(t, err)
			require.False(t, action.ClientReconnectRequired)
			require.Contains(t, action.Instructions, "connect this service, then review and approve")
		})

		stateID, _, _, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.NoError(t, idpErr)
		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
		require.NoError(t, err)
		require.NotNil(t, state.CodeReconnect)
		require.NotEqual(t, uuid.Nil, state.CodeReconnect.CatalogID)
		require.False(t, state.CodeReconnect.ReviewReady, "OAuth member must wait for upstream callback before opening review")
	})

	t.Run("setup-incomplete member keeps client reconnect fallback", func(t *testing.T) {
		t.Parallel()
		mock := memberMock()
		mock.buildAuthURLResult, _ = url.Parse("https://login.example.test/authorize")
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		auth, _ := contextvalues.GetAuthContext(ctx)
		mock.upsertResult = auth.UserID
		ti.service.manual = true
		_, issuer, downstream := seedPrivateToolsetWithIssuer(t, ctx, ti)
		slug := "code-setup-incomplete-" + uuid.NewString()[:8]
		gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
		existing := newCatalogUpstream(t, 1)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Existing", "existing", 1, existing.url)

		mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
		enableCodeMode(t, ctx, ti)
		connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, downstream, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, nil)

		// Attach a member with a legacy OAuth association but no typed auth client offer yet.
		brokenUpstream := newRecordingUpstream(t, "ping")
		provider := newConsentExchangeAS(t, nil, "unused")
		remoteClient := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "incomplete-oauth", provider.URL)
		remoteIssuer := clientRemoteIssuerID(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, remoteClient)
		brokenMember := createMetaMember(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "incomplete", brokenUpstream.url, conv.ToNullUUID(remoteIssuer), 2)
		selection := selectionFixture{ti: ti, projectID: *auth.ProjectID, orgID: auth.ActiveOrganizationID}
		selection.associate(t, ctx, brokenMember.mcpServerID, remoteClient)

		var action *codemode.ConnectionAction
		runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
			var err error
			action, err = host.Connect(runCtx, "incomplete")
			require.NoError(t, err)
			require.True(t, action.ClientReconnectRequired)
			require.Equal(t, "consent_required", action.Reason)
			require.Contains(t, action.Instructions, "reconnect this gateway")
		})

		stateID, browserCookie, _, idpErr := startCodeReconnectBrowserFlow(t, ctx, ti, mock, slug, action.AuthorizationURL)
		require.NoError(t, idpErr)
		state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
		require.NoError(t, err)
		require.Equal(t, uuid.Nil, state.CodeReconnect.CatalogID)
		require.False(t, state.CodeReconnect.ReviewReady)
		page := consentGetPageWithContext(t, ctx, ti, slug, stateID, browserCookie)
		require.Contains(t, page, "Reconnect this gateway in your AI client to approve its tools.")
		require.NotContains(t, page, "data-gateway-back")
		require.NotContains(t, page, "data-auto-close")
	})
}

func TestCodeModeCachedCatalogToolSelectionVisibility(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		selectedTools  []string
		wantPaths      []string
		wantCallable   []string
		wantRefused    []string
		wantNoCallable bool
		betaSearchErr  bool
	}{
		{
			name:          "all cached tools expose existing and newly attached members",
			selectedTools: nil,
			wantPaths:     []string{"alpha--tool_000", "alpha--tool_001", "beta--ping"},
			wantCallable:  []string{"alpha--tool_000", "beta--ping"},
		},
		{
			name:          "specific tools expose only selected subset across members",
			selectedTools: []string{"alpha--tool_000", "beta--ping"},
			wantPaths:     []string{"alpha--tool_000", "beta--ping"},
			wantCallable:  []string{"alpha--tool_000", "beta--ping"},
			wantRefused:   []string{"alpha--tool_001"},
		},
		{
			name:          "specific tools omitting a verified member keep that member uncallable",
			selectedTools: []string{"alpha--tool_000"},
			wantPaths:     []string{"alpha--tool_000"},
			wantCallable:  []string{"alpha--tool_000"},
			wantRefused:   []string{"alpha--tool_001", "beta--ping"},
			betaSearchErr: true,
		},
		{
			name:           "specific tools with empty selection leave no callable tools while members stay approved",
			selectedTools:  []string{},
			wantPaths:      []string{},
			wantRefused:    []string{"alpha--tool_000", "beta--ping"},
			wantNoCallable: true,
			betaSearchErr:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			auth, _ := contextvalues.GetAuthContext(ctx)
			ti.service.manual = true
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "code-sel-vis-" + uuid.NewString()[:8]
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)

			alpha := newCatalogUpstream(t, 2)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Alpha", "alpha", 1, alpha.url)
			tunnelGateway := &fakeTunnelGateway{t: t, toolNames: []string{"ping"}, agentSessionID: "agent-vis", backendSessionID: "backend-vis"}
			tunnelServer := httptest.NewServer(tunnelGateway)
			t.Cleanup(tunnelServer.Close)
			tunnelID, _ := seedTunneledMetaMember(t, ctx, ti, *auth.ProjectID, gateway.ID, "Beta Tunnel", "beta", 2, "")
			require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), tunnelServer.URL, time.Hour))

			mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
			enableCodeMode(t, ctx, ti)
			connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, tc.selectedTools)

			listing, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, listing.Code, listing.Body.String())
			if tc.wantNoCallable {
				require.Contains(t, listing.Body.String(), "No member tools are currently callable.")
			} else {
				require.NotContains(t, listing.Body.String(), "No member tools are currently callable.")
			}

			runCodeHost(t, ctx, ti, slug, connection.token, func(runCtx context.Context, host codemode.Host) {
				servers, err := host.Servers(runCtx, codemode.PageArgs{})
				require.NoError(t, err)
				require.Len(t, servers.Items, 2)
				for _, item := range servers.Items {
					require.Equal(t, "approved", item.Connection.Consent)
					require.Equal(t, "verified_at_consent", item.Connection.Authentication)
				}

				verified, err := host.Servers(runCtx, codemode.PageArgs{Verify: true})
				require.NoError(t, err)
				require.Len(t, verified.Items, 2)
				for _, item := range verified.Items {
					require.Equal(t, "approved", item.Connection.Consent)
					require.Equal(t, "connected", item.Connection.Authentication)
				}

				search, err := host.Search(runCtx, codemode.SearchArgs{Query: "", Limit: 50})
				require.NoError(t, err)
				require.False(t, search.Incomplete)
				gotPaths := make([]string, 0, len(search.Items))
				for _, item := range search.Items {
					gotPaths = append(gotPaths, item.Path)
				}
				require.ElementsMatch(t, tc.wantPaths, gotPaths)

				betaSearch, err := host.Search(runCtx, codemode.SearchArgs{Query: "", Server: "beta", Limit: 10})
				if tc.betaSearchErr {
					require.ErrorIs(t, err, codemode.ErrToolUnavailable)
				} else {
					require.NoError(t, err)
					require.Len(t, betaSearch.Items, 1)
					require.Equal(t, "beta--ping", betaSearch.Items[0].Path)
				}

				for _, path := range tc.wantCallable {
					described, err := host.Describe(runCtx, path)
					require.NoError(t, err)
					require.Equal(t, path, described.Path)
					res, err := host.Call(runCtx, path, json.RawMessage(`{}`))
					require.NoError(t, err)
					require.True(t, res.OK)
					require.NotEmpty(t, res.Content)
				}
				for _, path := range tc.wantRefused {
					_, err := host.Call(runCtx, path, json.RawMessage(`{}`))
					require.Error(t, err, "unselected tool %s must be refused", path)
				}
			})
		})
	}
}

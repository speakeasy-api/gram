package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func mintFrozenTestSession(t *testing.T, ctx context.Context, ti *testInstance, gatewayID, issuerID uuid.UUID, slug string, mode metamcp.DiscoveryMode, snapshot *toolfilter.FrozenToolset) string {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	policy, err := json.Marshal(&toolfilter.SessionPolicy{Resource: "meta_mcp_server:" + gatewayID.String(), Selection: nil, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &mode, Frozen: snapshot}})
	require.NoError(t, err)
	subject := urn.NewUserSubject(authCtx.UserID)
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: ti.serverURL.JoinPath("mcp", slug).String(), Lifetime: time.Hour})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuerID, SubjectUrn: subject, Jti: jti, RefreshTokenHash: conv.ToPGText(uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ToolSelection: policy})
	require.NoError(t, err)
	return token
}
func TestFrozenGatewayDirectRejectsDefinitionChanges(t *testing.T) {
	t.Parallel()
	testFrozenGatewayDefinitions(t, metamcp.DiscoveryModeDirect)
}
func TestFrozenGatewayProgressiveRejectsDefinitionChanges(t *testing.T) {
	t.Parallel()
	testFrozenGatewayDefinitions(t, metamcp.DiscoveryModeProgressive)
}
func testFrozenGatewayDefinitions(t *testing.T, mode metamcp.DiscoveryMode) {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "frozen-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)
	var changed atomic.Bool
	var calls atomic.Int64
	var onList atomic.Pointer[func()]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": req.Params["protocolVersion"], "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "frozen-fixture", "version": "1"}}
		case "tools/list":
			if callback := onList.Swap(nil); callback != nil {
				(*callback)()
			}
			description := "approved"
			if changed.Load() {
				description = "changed"
			}
			result = map[string]any{"tools": []any{map[string]any{"name": "ping", "description": description, "inputSchema": map[string]any{"type": "object"}, "outputSchema": map[string]any{"type": "string"}, "extension": map[string]any{"version": 1}}}}
		case "tools/call":
			calls.Add(1)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "pong"}}}
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(upstream.Close)
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Frozen member", "frozen-member", 1, upstream.URL)
	inventory, err := ti.service.GatewayInventory(ctx, gateway.ID, *authCtx.ProjectID, urn.NewUserSubject(authCtx.UserID))
	require.NoError(t, err)
	require.Len(t, inventory.Tools, 1)
	require.Contains(t, string(inventory.Tools[0].Definition), `"extension"`)
	token := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuerID, slug, mode, inventory)
	callName := "frozen-member--ping"
	args := map[string]any{}
	if mode == metamcp.DiscoveryModeProgressive {
		callName = "execute_tool"
		args = map[string]any{"name": "frozen-member--ping", "arguments": map[string]any{}}
	}
	call := func() string {
		w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": callName, "arguments": args}), token, nil)
		require.NoError(t, err)
		return w.Body.String()
	}
	require.Contains(t, call(), "pong")
	require.EqualValues(t, 1, calls.Load())
	changed.Store(true)
	require.Contains(t, call(), "approved frozen toolset")
	require.EqualValues(t, 1, calls.Load(), "changed tools never dispatch")
	var listing string
	if mode == metamcp.DiscoveryModeDirect {
		w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
		require.NoError(t, err)
		listing = w.Body.String()
	} else {
		w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "describe_server", "arguments": map[string]any{"server": "frozen-member"}}), token, nil)
		require.NoError(t, err)
		listing = w.Body.String()
	}
	require.NotContains(t, listing, `"name":"frozen-member--ping"`)
	fresh, err := ti.service.GatewayInventory(ctx, gateway.ID, *authCtx.ProjectID, urn.NewUserSubject(authCtx.UserID))
	require.NoError(t, err)
	oldHash, err := inventory.Fingerprint()
	require.NoError(t, err)
	_, err = fresh.Select(oldHash, []string{"frozen-member--ping"})
	require.ErrorContains(t, err, "changed during review")
	// Edit the remote URL while tools/list is in flight. This call must use the
	// checked destination; the next call must reject its changed identity.
	changed.Store(false)
	replacement := newRecordingUpstream(t, "ping")
	memberRow, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: memberID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	remoteQueries := remoterepo.New(ti.conn)
	remote, err := remoteQueries.GetServerByID(ctx, remoterepo.GetServerByIDParams{ID: memberRow.RemoteMcpServerID.UUID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	routingErr := make(chan error, 1)
	redirect := func() {
		_, err := remoteQueries.UpdateServer(ctx, remoterepo.UpdateServerParams{ID: remote.ID, ProjectID: remote.ProjectID, Name: remote.Name, Slug: remote.Slug, TransportType: remote.TransportType, Url: replacement.url})
		routingErr <- err
	}
	onList.Store(&redirect)
	require.Contains(t, call(), "pong")
	require.NoError(t, <-routingErr)
	require.EqualValues(t, 2, calls.Load(), "dispatch stays on the validated destination")
	require.Contains(t, call(), "approved frozen toolset")
	require.EqualValues(t, 2, calls.Load())
}

func TestFrozenGatewayRefreshPreservesSnapshot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "frozen-refresh-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
	policy, err := json.Marshal(&toolfilter.SessionPolicy{Resource: "meta_mcp_server:" + gateway.ID.String(), Selection: nil, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: nil, Frozen: &toolfilter.FrozenToolset{Tools: []toolfilter.FrozenTool{}}}})
	require.NoError(t, err)
	refresh := "frozen-refresh-" + uuid.NewString()
	hash := sha256.Sum256([]byte(refresh))
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuer.ID, UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true}, SubjectUrn: urn.NewUserSubject(authCtx.UserID), Jti: uuid.NewString(), RefreshTokenHash: conv.ToPGText(base64.RawURLEncoding.EncodeToString(hash[:])), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}, ToolSelection: policy})
	require.NoError(t, err)
	response := performRefreshRequest(ctx, ti, slug, client.ClientID, refresh)
	require.NoError(t, response.err)
	require.Equal(t, http.StatusOK, response.code, response.body)
	var result struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(response.body), &result))
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(result.AccessToken, urn.NewUserSessionIssuer(issuer.ID).String())
	require.NoError(t, err)
	row, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
	require.NoError(t, err)
	require.JSONEq(t, string(policy), string(row.ToolSelection))
	replay := performRefreshRequest(ctx, ti, slug, client.ClientID, refresh)
	require.NoError(t, replay.err)
	require.Equal(t, http.StatusOK, replay.code, replay.body)
}

func TestFrozenGatewayHostedDefaultAndRevocation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "frozen-hosted-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted frozen", 1, mcpservers.VisibilityPublic, "alpha", "beta")
	inventory, err := ti.service.GatewayInventory(ctx, gateway.ID, *authCtx.ProjectID, urn.NewUserSubject(authCtx.UserID))
	require.NoError(t, err)
	require.Len(t, inventory.Tools, 2)
	token := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeDirect, inventory)
	list := func(token string) string {
		w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
		require.NoError(t, err)
		return w.Body.String()
	}
	require.Contains(t, list(token), member.slug+"--alpha")
	empty := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeDirect, &toolfilter.FrozenToolset{Tools: []toolfilter.FrozenTool{}})
	require.NotContains(t, list(empty), member.slug+"--alpha")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": member.slug + "--alpha", "arguments": map[string]any{}}), empty, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "error")
	queries := toolsetsrepo.New(ti.conn)
	row, err := queries.GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: member.toolsetID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	_, err = queries.UpdateToolset(ctx, toolsetsrepo.UpdateToolsetParams{Name: row.Name, Slug: row.Slug, Description: row.Description, McpEnabled: row.McpEnabled, McpIsPublic: row.McpIsPublic, McpSlug: row.McpSlug, DefaultEnvironmentSlug: conv.ToPGText("changed-environment"), ProjectID: row.ProjectID, CustomDomainID: row.CustomDomainID, ToolSelectionMode: row.ToolSelectionMode})
	require.NoError(t, err)
	require.NotContains(t, list(token), member.slug+"--alpha", "changed default environment requires review")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": member.slug + "--alpha", "arguments": map[string]any{}}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "approved frozen toolset")
	fresh, err := ti.service.GatewayInventory(ctx, gateway.ID, *authCtx.ProjectID, urn.NewUserSubject(authCtx.UserID))
	require.NoError(t, err)
	refreshed := mintFrozenTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeDirect, fresh)
	require.Contains(t, list(refreshed), member.slug+"--alpha")
	setToolsetMcpDisabled(t, ctx, ti, member.toolsetID, *authCtx.ProjectID)
	require.NotContains(t, list(refreshed), member.slug+"--alpha", "current revocation wins")
}

func TestFrozenGatewayTunnelAndIncompleteInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "frozen-tunnel-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer)
	gateway := &fakeTunnelGateway{toolNames: []string{"ping"}, t: t, agentSessionID: "frozen-agent", backendSessionID: "frozen-backend", legacy: false, dead: false, challenge: ""}
	tunnelID, _ := seedTunneledMetaMember(t, ctx, ti, *authCtx.ProjectID, meta.ID, "Frozen tunnel", "frozen-tunnel", 0, "")
	upstream := httptest.NewServer(gateway)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))
	inventory, err := ti.service.GatewayInventory(ctx, meta.ID, *authCtx.ProjectID, urn.NewUserSubject(authCtx.UserID))
	require.NoError(t, err)
	require.Len(t, inventory.Tools, 1)
	token := mintFrozenTestSession(t, ctx, ti, meta.ID, issuer, slug, metamcp.DiscoveryModeDirect, inventory)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "frozen-tunnel--ping", "arguments": map[string]any{}}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "pong through the tunnel")
	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(unavailable.Close)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "Unavailable member", "unavailable", 1, unavailable.URL)
	_, err = ti.service.GatewayInventory(ctx, meta.ID, *authCtx.ProjectID, urn.NewUserSubject(authCtx.UserID))
	require.ErrorContains(t, err, "incomplete")
	// An unapproved new member cannot break the existing frozen inventory.
	w, err = servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "frozen-tunnel--ping")
	require.NotContains(t, w.Body.String(), "error")
}

func TestFrozenConsentKeepsApprovalWhenInventoryUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "freeze-consent-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(unavailable.Close)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Unavailable", "unavailable", 1, unavailable.URL)
	subject := urn.NewUserSubject(authCtx.UserID)
	policy, err := json.Marshal(&toolfilter.SessionPolicy{Resource: "meta_mcp_server:" + gateway.ID.String(), Gateway: &toolfilter.GatewayOptions{Frozen: &toolfilter.FrozenToolset{Tools: []toolfilter.FrozenTool{}}}})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuer.ID, UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true}, SubjectUrn: subject, Jti: uuid.NewString(), RefreshTokenHash: conv.ToPGText(uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ToolSelection: policy})
	require.NoError(t, err)
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{ID: stateID, UserSessionIssuerID: issuer.ID, Endpoint: mcp.EndpointRef{McpSlug: slug, RouteBase: "mcp"}, ClientID: client.ClientID, RedirectURI: client.RedirectUris[0], State: "client-state", CodeChallenge: "challenge", CodeChallengeMethod: "S256", CSRFToken: "csrf", Subject: &subject, AuthorizerUserID: authCtx.UserID, AuthorizerImpersonated: new(bool), CreatedAt: time.Now()}))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("mcpSlug", slug)
	requestContext := context.WithValue(ctx, chi.RouteCtxKey, routeContext)
	render := func() string {
		req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect?state="+stateID, nil).WithContext(requestContext)
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleConsent(w, req))
		require.Equal(t, http.StatusOK, w.Code)
		return w.Body.String()
	}
	ti.features.SetFlag(feature.FlagGatewayFrozenToolsets, authCtx.ActiveOrganizationID, true)
	body := render()
	require.Contains(t, body, "The full tool list is unavailable")
	require.Regexp(t, `name="gateway_freeze"[^>]*checked[^>]*>`, body)
	require.NotRegexp(t, `name="gateway_freeze"[^>]*disabled`, body)
	ti.features.SetFlag(feature.FlagGatewayFrozenToolsets, authCtx.ActiveOrganizationID, false)
	body = render()
	require.Contains(t, body, "Your existing connection stays frozen")
	require.Regexp(t, `name="gateway_freeze"[^>]*checked[^>]*>`, body)
}

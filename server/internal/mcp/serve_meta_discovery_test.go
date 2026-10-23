package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func setGatewayMode(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID, mode string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := metamcprepo.New(ti.conn)
	server, err := queries.GetMetaMCPServer(ctx, metamcprepo.GetMetaMCPServerParams{ID: id, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	_, err = queries.UpdateMetaMCPServer(ctx, metamcprepo.UpdateMetaMCPServerParams{ID: id, ProjectID: server.ProjectID, OrganizationID: server.OrganizationID, Name: server.Name, UserSessionIssuerID: server.UserSessionIssuerID, DiscoveryMode: conv.ToPGText(mode)})
	require.NoError(t, err)
}

func TestGatewayDiscoveryDefaultChangesLive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	denied := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Private tools", 2, mcpservers.VisibilityPublic, "hidden")
	setToolsetMcpPrivate(t, ctx, ti, denied.toolsetID, *authCtx.ProjectID)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
	request := makeMetaRPCBody(t, "tools/list", map[string]any{})
	w, err := servePublicHTTP(t, t.Context(), ti, slug, request, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"name":"execute_tool"`)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, request, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), member.slug+"--alpha")
	require.NotContains(t, w.Body.String(), denied.slug+"--hidden")
	require.NotContains(t, w.Body.String(), `"name":"execute_tool"`)
	require.Contains(t, w.Body.String(), `"inputSchema"`)
	setGatewayMode(t, ctx, ti, gateway.ID, "progressive")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, request, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"name":"execute_tool"`)
}

func TestGatewayDirectRoutesRemoteTool(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newRecordingUpstream(t, "ping")
	memberSlug := "remote-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Remote tools", memberSlug, 1, upstream.url)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), memberSlug+"--ping")
	result := callMetaTool(t, t.Context(), ti, slug, memberSlug+"--ping", map[string]any{})
	require.NotContains(t, result, "error")
	require.Contains(t, result, "result")
	rejected := callMetaTool(t, t.Context(), ti, slug, "execute_tool", map[string]any{"name": memberSlug + "--ping"})
	require.Contains(t, rejected, "error")
}

func TestGatewayModeOnlySessionIsBoundAndUnrestricted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha", "beta")
	otherSlug := "gateway-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, otherSlug, issuerID)
	mode := metamcp.DiscoveryModeDirect
	policy, err := json.Marshal(&toolfilter.SessionPolicy{Resource: "meta_mcp_server:" + gateway.ID.String(), Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &mode}})
	require.NoError(t, err)
	subject := urn.NewUserSubject(authCtx.UserID)
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: ti.serverURL.String() + "/mcp/" + slug, Lifetime: time.Hour})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuerID, SubjectUrn: subject, Jti: jti, RefreshTokenHash: conv.ToPGText("gateway-mode-test-" + uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ToolSelection: policy})
	require.NoError(t, err)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), member.slug+"--alpha")
	require.Contains(t, w.Body.String(), member.slug+"--beta")
	require.NotContains(t, w.Body.String(), `"name":"execute_tool"`)
	_, err = servePublicHTTP(t, t.Context(), ti, otherSlug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
	require.ErrorContains(t, err, "invalid access token")
}

type directListing struct {
	Result *struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		NextCursor *string `json:"nextCursor"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func listDirectGateway(t *testing.T, ti *testInstance, slug string, params map[string]any) directListing {
	t.Helper()
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", params), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	var listing directListing
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listing), w.Body.String())
	return listing
}

func (l directListing) names() []string {
	if l.Result == nil {
		return nil
	}
	names := make([]string, 0, len(l.Result.Tools))
	for _, tool := range l.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestGatewayDirectListsWholeInventoryInOneResponse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	names := make([]string, 101)
	for i := range names {
		names[i] = fmt.Sprintf("tool_%03d", i)
	}
	seedHostedMetaMember(t, ctx, ti, gateway.ID, "Many tools", 1, mcpservers.VisibilityPublic, names...)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	listing := listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Error)
	require.Len(t, listing.Result.Tools, 101)
	require.Nil(t, listing.Result.NextCursor)
}

func TestGatewayDirectRejectsCursorFromAPaginatedListing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	listing := listDirectGateway(t, ti, slug, map[string]any{"cursor": "eyJvZmZzZXQiOjEwMH0"})
	require.Nil(t, listing.Result)
	require.NotNil(t, listing.Error)
	require.Contains(t, listing.Error.Message, "restart tools/list")
}

// scriptedUpstream is a streamable-HTTP MCP server whose tools/list pages its
// current tools and can be made to fail every call with a raw status and body.
type scriptedUpstream struct {
	url string

	mu         sync.Mutex
	tools      []any
	pageSize   int
	failStatus int
	failBody   string

	initializes atomic.Int32
	listings    atomic.Int32
}

func newScriptedUpstream(t *testing.T, tools []any, pageSize int) *scriptedUpstream {
	t.Helper()
	u := &scriptedUpstream{tools: tools, pageSize: pageSize}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Cursor string `json:"cursor"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		u.mu.Lock()
		tools, pageSize, failStatus, failBody := u.tools, u.pageSize, u.failStatus, u.failBody
		u.mu.Unlock()
		if failStatus != 0 {
			w.WriteHeader(failStatus)
			_, _ = w.Write([]byte(failBody))
			return
		}
		var response any
		switch request.Method {
		case "initialize":
			u.initializes.Add(1)
			response = map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "scripted-test", "version": "1"}}}
		case "tools/list":
			u.listings.Add(1)
			offset := 0
			if request.Params.Cursor != "" {
				if _, err := fmt.Sscanf(request.Params.Cursor, "page-%d", &offset); err != nil {
					t.Errorf("parse upstream cursor: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
			}
			end := min(offset+pageSize, len(tools))
			page := map[string]any{"tools": tools[offset:end]}
			if end < len(tools) {
				page["nextCursor"] = fmt.Sprintf("page-%d", end)
			}
			response = map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": page}
		default:
			response = map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{}}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode upstream response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	u.url = server.URL
	return u
}

func (u *scriptedUpstream) fail(status int, body string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.failStatus, u.failBody = status, body
}

func (u *scriptedUpstream) setTools(tools []any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.tools = tools
}

func scriptedTools(prefix string, n int) []any {
	tools := make([]any, n)
	for i := range tools {
		tools[i] = map[string]any{"name": fmt.Sprintf("%s_%03d", prefix, i), "inputSchema": map[string]any{"type": "object"}}
	}
	return tools
}

func TestGatewayDirectListsPagedUpstreamInOneDiscovery(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newScriptedUpstream(t, scriptedTools("tool", 150), 60)
	memberSlug := "paged-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Paged tools", memberSlug, 1, upstream.url)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	listing := listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Error)
	require.Len(t, listing.Result.Tools, 150)
	require.Nil(t, listing.Result.NextCursor)
	require.Contains(t, listing.names(), memberSlug+"--tool_149")
	require.Equal(t, int32(1), upstream.initializes.Load())
	require.Equal(t, int32(3), upstream.listings.Load())
}

func TestGatewayDirectFailsWhenAMemberDoesNotAnswer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "rate limited with a non-JSON body", status: http.StatusTooManyRequests, body: "rate limited"},
		{name: "rate limited with a JSON-RPC error", status: http.StatusTooManyRequests, body: `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"rate limited"}}`},
		{name: "server error", status: http.StatusInternalServerError, body: ""},
		{name: "unavailable", status: http.StatusServiceUnavailable, body: ""},
		{name: "JSON-RPC error", status: http.StatusOK, body: `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"internal"}}`},
		{name: "unreadable success", status: http.StatusOK, body: "not json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			authCtx, _ := contextvalues.GetAuthContext(ctx)
			slug := "gateway-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
			healthy := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
			upstream := newScriptedUpstream(t, scriptedTools("tool", 3), 100)
			upstream.fail(tc.status, tc.body)
			memberSlug := "failing-" + uuid.NewString()[:8]
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Failing tools", memberSlug, 2, upstream.url)
			setGatewayMode(t, ctx, ti, gateway.ID, "direct")
			listing := listDirectGateway(t, ti, slug, map[string]any{})
			require.Nil(t, listing.Result, "a member that did not answer must not yield a partial listing")
			require.NotNil(t, listing.Error)
			require.Contains(t, listing.Error.Message, "gateway tool inventory is incomplete")
			require.Contains(t, listing.Error.Message, memberSlug)
			require.NotContains(t, listing.Error.Message, healthy.slug)
		})
	}
}

func TestGatewayDirectRecoversAndSeesLiveToolsAfterAFailedListing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newScriptedUpstream(t, scriptedTools("tool", 120), 50)
	memberSlug := "flaky-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Flaky tools", memberSlug, 1, upstream.url)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	upstream.fail(http.StatusTooManyRequests, "rate limited")
	listing := listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Result)
	require.NotNil(t, listing.Error)
	upstream.fail(0, "")
	listing = listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Error)
	require.Len(t, listing.Result.Tools, 120)
	upstream.setTools(scriptedTools("tool", 121))
	listing = listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Error)
	require.Len(t, listing.Result.Tools, 121)
	require.Contains(t, listing.names(), memberSlug+"--tool_120")
}

func TestGatewayDirectOmitsMembersThatRejectTheCredential(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			authCtx, _ := contextvalues.GetAuthContext(ctx)
			slug := "gateway-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
			member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
			upstream := newScriptedUpstream(t, scriptedTools("tool", 3), 100)
			upstream.fail(status, "")
			rejectingSlug := "rejecting-" + uuid.NewString()[:8]
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Rejecting tools", rejectingSlug, 2, upstream.url)
			setGatewayMode(t, ctx, ti, gateway.ID, "direct")
			listing := listDirectGateway(t, ti, slug, map[string]any{})
			require.Nil(t, listing.Error)
			require.Equal(t, []string{member.slug + "--alpha"}, listing.names())
		})
	}
}
func TestDirectRejectsMalformedCatalogWhileProgressiveRemainsUsable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newListingUpstream(t, []any{map[string]any{"name": "valid", "inputSchema": map[string]any{"type": "object"}}, map[string]any{"description": "missing name", "inputSchema": map[string]any{"type": "object"}}}, nil)
	memberSlug := "catalog-member"
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Catalog member", memberSlug, 1, upstream)
	result := callMetaTool(t, t.Context(), ti, slug, "describe_tools", map[string]any{"tools": []string{memberSlug + "--valid"}})
	require.NotContains(t, result, "error")
	require.Contains(t, string(result["result"]), memberSlug+"--valid")
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "invalid definitions")
	require.NotContains(t, w.Body.String(), memberSlug+"--valid")
}

// newListingUpstream serves a minimal streamable-HTTP MCP server whose
// tools/list returns tools. beforeList, when set, runs first and can fail the
// listing by returning false.
func newListingUpstream(t *testing.T, tools []any, beforeList func() bool) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "catalog-test", "version": "1"}}
		case "tools/list":
			if beforeList != nil && !beforeList() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			result = map[string]any{"tools": tools}
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			t.Errorf("encode upstream response: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)
	return upstream.URL
}

func TestGatewayDirectFailsWhenAMemberIsDown(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()
	downSlug := "down-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Down tools", downSlug, 2, downURL)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	listing := listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Result)
	require.NotNil(t, listing.Error)
	require.Contains(t, listing.Error.Message, downSlug)
	require.NotContains(t, listing.Error.Message, member.slug+"--alpha")
}

func TestGatewayDirectDescribesMembersConcurrently(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// Each listing waits until both are in flight, so a sequential describe
	// times out the first member.
	var inFlight atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	barrier := func() bool {
		if inFlight.Add(1) >= 2 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
			return true
		case <-time.After(5 * time.Second):
			return false
		}
	}
	slugs := []string{"first-" + uuid.NewString()[:8], "second-" + uuid.NewString()[:8]}
	for i, memberSlug := range slugs {
		upstream := newListingUpstream(t, []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}, barrier)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Member "+memberSlug, memberSlug, int32(i+1), upstream)
	}
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), slugs[0]+"--ping")
	require.Contains(t, w.Body.String(), slugs[1]+"--ping")
}

func TestGatewayDirectBoundsTheWholeInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithMetaRuntime(t, testenv.NewMeterProvider(t), mcp.MetaRuntimeConfig{MemberCallTimeout: 500 * time.Millisecond, ValidationTimeout: 0, AutoVerifyWait: 0, RecheckInterval: 0})
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// Five batches of members that never answer would take five call timeouts
	// one batch at a time; the inventory stops after its overall budget.
	hang := func() bool {
		<-time.After(3 * time.Second)
		return true
	}
	for i := range 25 {
		memberSlug := fmt.Sprintf("member%d-%s", i, uuid.NewString()[:8])
		upstream := newListingUpstream(t, []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}, hang)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Member "+memberSlug, memberSlug, int32(i+1), upstream)
	}
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	started := time.Now()
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Less(t, time.Since(started), 2*time.Second)
	// Members past the budget leave the inventory unknown, so the listing fails.
	require.Contains(t, w.Body.String(), "gateway tool inventory is incomplete")
	require.NotContains(t, w.Body.String(), `"tools"`)
}

func TestGatewayDirectGivesQueuedMembersTheirOwnDeadline(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithMetaRuntime(t, testenv.NewMeterProvider(t), mcp.MetaRuntimeConfig{MemberCallTimeout: 1500 * time.Millisecond, ValidationTimeout: 0, AutoVerifyWait: 0, RecheckInterval: 0})
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// Seven members against a concurrency limit of six: the seventh starts
	// only after a slow member finishes, past half of one shared deadline.
	slow := func() bool {
		<-time.After(time.Second)
		return true
	}
	slugs := make([]string, 7)
	for i := range slugs {
		slugs[i] = fmt.Sprintf("member%d-%s", i, uuid.NewString()[:8])
		upstream := newListingUpstream(t, []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}, slow)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Member "+slugs[i], slugs[i], int32(i+1), upstream)
	}
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	for _, memberSlug := range slugs {
		require.Contains(t, w.Body.String(), memberSlug+"--ping")
	}
}

func TestGatewayDirectCanceledListingFailsWithoutListing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithMetaRuntime(t, testenv.NewMeterProvider(t), mcp.MetaRuntimeConfig{MemberCallTimeout: 5 * time.Second, ValidationTimeout: 0, AutoVerifyWait: 0, RecheckInterval: 0})
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// The seventh member is still queued behind the concurrency limit when
	// the request is canceled, so it is never described.
	slow := func() bool {
		<-time.After(2 * time.Second)
		return true
	}
	for i := range 7 {
		memberSlug := fmt.Sprintf("member%d-%s", i, uuid.NewString()[:8])
		upstream := newListingUpstream(t, []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}, slow)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Member "+memberSlug, memberSlug, int32(i+1), upstream)
	}
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	requestCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	w, err := servePublicHTTP(t, requestCtx, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	if err == nil {
		require.NotContains(t, w.Body.String(), "--ping")
	}
}

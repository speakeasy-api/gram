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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
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
	token := mintGatewayTestSession(t, ctx, ti, gateway.ID, issuerID, slug, metamcp.DiscoveryModeDirect)
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
// current tools and can be made to fail its next calls with a raw status,
// Retry-After, and body.
type scriptedUpstream struct {
	url string

	mu             sync.Mutex
	tools          []any
	pageSize       int
	failStatus     int
	failBody       string
	failRetryAfter string
	failRemaining  int

	initializes atomic.Int32
	listings    atomic.Int32
	failures    atomic.Int32
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
		// Discovery falls back to initialize, so failures start at initialize
		// and count one per attempt.
		if request.Method == "server/discover" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		u.mu.Lock()
		tools, pageSize, failStatus, failBody, failRetryAfter := u.tools, u.pageSize, u.failStatus, u.failBody, u.failRetryAfter
		if u.failRemaining > 0 {
			u.failRemaining--
		} else if u.failRemaining == 0 {
			failStatus = 0
		}
		u.mu.Unlock()
		if failStatus != 0 {
			u.failures.Add(1)
			if failRetryAfter != "" {
				w.Header().Set("Retry-After", failRetryAfter)
			}
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

// fail answers the next times requests, or every request when times is
// negative, with status, retryAfter, and body.
func (u *scriptedUpstream) fail(status int, retryAfter, body string, times int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.failStatus, u.failRetryAfter, u.failBody, u.failRemaining = status, retryAfter, body, times
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

// omittedMemberLogs reads a gateway's omitted-member discovery rows the way
// the dashboard does: by event source and the gateway's tools/list URN.
func omittedMemberLogs(t *testing.T, projectID, gatewayID uuid.UUID, want int) []telemetryrepo.TelemetryLog {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var logs []telemetryrepo.TelemetryLog
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		logs, err = telemetryrepo.New(chConn).ListTelemetryLogs(t.Context(), telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: projectID.String(),
			TimeStart:     time.Now().Add(-time.Hour).UnixNano(),
			TimeEnd:       time.Now().Add(time.Hour).UnixNano(),
			GramURNs:      []string{"metamcp:" + gatewayID.String() + ":tools/list"},
			EventSource:   "meta_discovery",
			SortOrder:     "desc",
			Limit:         100,
		})
		assert.NoError(c, err)
		assert.Len(c, logs, want)
	}, 5*time.Second, 50*time.Millisecond)
	return logs
}

func requireOmittedMemberLog(t *testing.T, log telemetryrepo.TelemetryLog, status int, message string) {
	t.Helper()
	var attributes struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		HTTP struct {
			Response struct {
				StatusCode int `json:"status_code"`
			} `json:"response"`
		} `json:"http"`
	}
	require.NoError(t, json.Unmarshal([]byte(log.Attributes), &attributes), log.Attributes)
	require.Equal(t, status, attributes.HTTP.Response.StatusCode)
	require.Contains(t, attributes.Error.Message, message)
}

func TestGatewayDirectOmitsAndRecordsAMemberThatFails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		status   int
		body     string
		retried  bool
		recorded int
	}{
		{name: "rate limited with a non-JSON body", status: http.StatusTooManyRequests, body: "rate limited", retried: true, recorded: http.StatusTooManyRequests},
		{name: "rate limited with a JSON-RPC error", status: http.StatusTooManyRequests, body: `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"rate limited"}}`, retried: true, recorded: http.StatusTooManyRequests},
		{name: "server error", status: http.StatusInternalServerError, body: "", retried: true, recorded: http.StatusInternalServerError},
		{name: "unavailable", status: http.StatusServiceUnavailable, body: "", retried: true, recorded: http.StatusServiceUnavailable},
		{name: "client error", status: http.StatusBadRequest, body: "", retried: false, recorded: http.StatusBadRequest},
		{name: "JSON-RPC error", status: http.StatusOK, body: `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"internal"}}`, retried: false, recorded: http.StatusBadGateway},
		{name: "unreadable success", status: http.StatusOK, body: "not json", retried: false, recorded: http.StatusBadGateway},
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
			upstream.fail(tc.status, "", tc.body, -1)
			memberSlug := "failing-" + uuid.NewString()[:8]
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Failing tools", memberSlug, 2, upstream.url)
			setGatewayMode(t, ctx, ti, gateway.ID, "direct")
			started := time.Now()
			listing := listDirectGateway(t, ti, slug, map[string]any{})
			require.Nil(t, listing.Error)
			require.Equal(t, []string{healthy.slug + "--alpha"}, listing.names())
			require.Less(t, time.Since(started), 4*time.Second)
			// A persistent retryable failure is tried again while the listing's
			// budget allows, at most maxMemberDescribeAttempts times in all.
			if tc.retried {
				require.GreaterOrEqual(t, upstream.failures.Load(), int32(2))
				require.LessOrEqual(t, upstream.failures.Load(), int32(3))
			} else {
				require.Equal(t, int32(1), upstream.failures.Load())
			}
			logs := omittedMemberLogs(t, *authCtx.ProjectID, gateway.ID, 1)
			requireOmittedMemberLog(t, logs[0], tc.recorded, memberSlug)
		})
	}
}

func TestGatewayDirectRetriesARateLimitedMemberAfterRetryAfter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newScriptedUpstream(t, scriptedTools("tool", 120), 50)
	memberSlug := "limited-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Limited tools", memberSlug, 1, upstream.url)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	upstream.fail(http.StatusTooManyRequests, "1", "rate limited", 1)
	started := time.Now()
	listing := listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Error)
	require.Len(t, listing.Result.Tools, 120)
	require.GreaterOrEqual(t, time.Since(started), time.Second)
	require.Equal(t, int32(1), upstream.failures.Load())
	upstream.setTools(scriptedTools("tool", 121))
	listing = listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Error)
	require.Contains(t, listing.names(), memberSlug+"--tool_120")
}

func TestGatewayDirectListsHealthyMembersQueuedBehindRateLimitedOnes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// More rate-limited members than the concurrency limit come first, and
	// wait out their Retry-After. The healthy member queued behind them takes
	// a second to answer, so it is listed only if it can start while they wait.
	for i := range 6 {
		limited := newScriptedUpstream(t, scriptedTools("tool", 1), 100)
		limited.fail(http.StatusTooManyRequests, "2", "rate limited", -1)
		limitedSlug := fmt.Sprintf("limited%d-%s", i, uuid.NewString()[:8])
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Limited "+limitedSlug, limitedSlug, int32(i+1), limited.url)
	}
	slow := func() bool {
		<-time.After(time.Second)
		return true
	}
	healthy := newListingUpstream(t, []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}, slow)
	healthySlug := "healthy-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Healthy tools", healthySlug, 7, healthy)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	listing := listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Error)
	require.Equal(t, []string{healthySlug + "--ping"}, listing.names())
}

func TestGatewayDirectOmitsAMemberWhoseRetryAfterOutlastsTheListing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	healthy := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
	upstream := newScriptedUpstream(t, scriptedTools("tool", 3), 100)
	upstream.fail(http.StatusTooManyRequests, "99999999999999999999", "rate limited", -1)
	memberSlug := "limited-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Limited tools", memberSlug, 2, upstream.url)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	started := time.Now()
	listing := listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Error)
	require.Equal(t, []string{healthy.slug + "--alpha"}, listing.names())
	require.Less(t, time.Since(started), time.Second)
	require.Equal(t, int32(1), upstream.failures.Load())
	requireOmittedMemberLog(t, omittedMemberLogs(t, *authCtx.ProjectID, gateway.ID, 1)[0], http.StatusTooManyRequests, memberSlug)
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
			upstream.fail(status, "", "", -1)
			rejectingSlug := "rejecting-" + uuid.NewString()[:8]
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Rejecting tools", rejectingSlug, 2, upstream.url)
			// A failing member in the same listing writes the one row, in the
			// same batch any row for the rejecting member would join.
			failing := newScriptedUpstream(t, scriptedTools("tool", 1), 100)
			failing.fail(http.StatusBadRequest, "", "", -1)
			failingSlug := "failing-" + uuid.NewString()[:8]
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Failing tools", failingSlug, 3, failing.url)
			setGatewayMode(t, ctx, ti, gateway.ID, "direct")
			listing := listDirectGateway(t, ti, slug, map[string]any{})
			require.Nil(t, listing.Error)
			require.Equal(t, []string{member.slug + "--alpha"}, listing.names())
			// A credential exclusion is not a failure, so only the failing
			// member is recorded.
			requireOmittedMemberLog(t, omittedMemberLogs(t, *authCtx.ProjectID, gateway.ID, 1)[0], http.StatusBadRequest, failingSlug)
		})
	}
}

func TestDirectOmitsMalformedCatalogWhileProgressiveRemainsUsable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	healthy := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 2, mcpservers.VisibilityPublic, "alpha")
	upstream := newListingUpstream(t, []any{map[string]any{"name": "valid", "inputSchema": map[string]any{"type": "object"}}, map[string]any{"description": "missing name", "inputSchema": map[string]any{"type": "object"}}}, nil)
	memberSlug := "catalog-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Catalog member", memberSlug, 1, upstream)
	result := callMetaTool(t, t.Context(), ti, slug, "describe_tools", map[string]any{"tools": []string{memberSlug + "--valid"}})
	require.NotContains(t, result, "error")
	require.Contains(t, string(result["result"]), memberSlug+"--valid")
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	listing := listDirectGateway(t, ti, slug, map[string]any{})
	require.Nil(t, listing.Error)
	require.Equal(t, []string{healthy.slug + "--alpha"}, listing.names())
	requireOmittedMemberLog(t, omittedMemberLogs(t, *authCtx.ProjectID, gateway.ID, 1)[0], http.StatusBadGateway, memberSlug)
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

func TestGatewayDirectListsReachableMembersWhenOneIsDown(t *testing.T) {
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
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), member.slug+"--alpha")
	require.NotContains(t, w.Body.String(), downSlug+"--")
	require.NotContains(t, w.Body.String(), `"error"`)
	requireOmittedMemberLog(t, omittedMemberLogs(t, *authCtx.ProjectID, gateway.ID, 1)[0], http.StatusBadGateway, downSlug)
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
	// Members past the budget are skipped, not a failed listing.
	require.NotContains(t, w.Body.String(), `"error"`)
	require.Contains(t, w.Body.String(), `"tools":[]`)
	// The listing's own deadline has ended, but each skipped member is recorded.
	omittedMemberLogs(t, *authCtx.ProjectID, gateway.ID, 25)
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

package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	mcpfeedbackrepo "github.com/speakeasy-api/gram/server/internal/mcpfeedback/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// serveFeedback has the test service answer feedback tools, as the server does.
func serveFeedback(t *testing.T, ti *testInstance) {
	t.Helper()

	ti.service.SetFeedback(mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, chatredact.New(), mcpfeedback.Limits{Caller: nil, Server: nil}, "gram-server", nil, mcpfeedback.WithSettingsFreshFor(0)))
}

// setFeedbackTool stores a server's feedback tool settings. An empty name uses the default.
func setFeedbackTool(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID, enabled bool, toolName string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := mcpfeedbackrepo.New(ti.conn).UpsertFeedbackConfig(ctx, mcpfeedbackrepo.UpsertFeedbackConfigParams{
		McpServerID:     serverID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		Enabled:         enabled,
		ToolName:        pgtype.Text{String: toolName, Valid: toolName != ""},
		ToolDescription: pgtype.Text{String: "", Valid: false},
		UpdatedByUserID: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
}

func storedFeedback(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID) []mcpfeedbackrepo.ListFeedbackRow {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	rows, err := mcpfeedbackrepo.New(ti.conn).ListFeedback(ctx, mcpfeedbackrepo.ListFeedbackParams{
		ProjectID:       *authCtx.ProjectID,
		McpServerID:     serverID,
		Severity:        pgtype.Text{String: "", Valid: false},
		ToolName:        pgtype.Text{String: "", Valid: false},
		CursorCreatedAt: pgtype.Timestamptz{Valid: false},
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageLimit:       100,
	})
	require.NoError(t, err)
	return rows
}

func feedbackCallBody(t *testing.T, toolName string, arguments map[string]any) []byte {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params":  map[string]any{"name": toolName, "arguments": arguments},
	})
	require.NoError(t, err)
	return body
}

// toolCallOutcome is the part of a tools/call response a caller acts on.
type toolCallOutcome struct {
	Result *struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent map[string]any `json:"structuredContent"`
		IsError           bool           `json:"isError"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func decodeToolCall(t *testing.T, body []byte) toolCallOutcome {
	t.Helper()

	var outcome toolCallOutcome
	require.NoError(t, json.Unmarshal(body, &outcome), string(body))
	return outcome
}

func TestServePublic_FeedbackTool_HostedServerListsAndRecordsIt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "feedback-hosted-"+uuid.NewString()[:8])
	addHTTPTools(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_alpha")
	slug := toolset.McpSlug.String

	// A server offers the tool only once it is switched on.
	w, err := servePublicHTTP(t, ctx, ti, slug, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"tool_alpha"}, toolNames(parseToolsListResponse(t, w.Body.Bytes())))

	setFeedbackTool(t, ctx, ti, toolset.ID, true, "")

	w, err = servePublicHTTP(t, ctx, ti, slug, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	listed := parseToolsListResponse(t, w.Body.Bytes())
	require.Equal(t, []string{"tool_alpha", mcpfeedback.DefaultToolName}, toolNames(listed))
	require.Equal(t, mcpfeedback.DefaultToolDescription, listed.Result.Tools[1].Description)
	require.Contains(t, w.Body.String(), `"openWorldHint":true`)
	require.Contains(t, w.Body.String(), `"required":["content"]`)

	// An anonymous caller of the public server: the request carries no identity.
	w, err = servePublicHTTP(t, t.Context(), ti, slug, feedbackCallBody(t, mcpfeedback.DefaultToolName, map[string]any{
		"content":  "tool_alpha returned an empty list although there is data.",
		"severity": "blocked",
		"tool":     "tool_alpha",
	}), "", map[string]string{"Mcp-Session-Id": "session-abc"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	rows := storedFeedback(t, ctx, ti, toolset.ID)
	require.Len(t, rows, 1)
	require.Equal(t, "tool_alpha returned an empty list although there is data.", rows[0].Content)
	require.Equal(t, "blocked", rows[0].Severity.String)
	require.Equal(t, "tool_alpha", rows[0].ToolName.String)
	require.False(t, rows[0].UserID.Valid, "nobody was signed in")

	outcome := decodeToolCall(t, w.Body.Bytes())
	require.NotNil(t, outcome.Result)
	require.False(t, outcome.Result.IsError)
	require.Contains(t, outcome.Result.Content[0].Text, "Feedback received (reference "+rows[0].ID.String()+")")
	require.Equal(t, rows[0].ID.String(), outcome.Result.StructuredContent["id"])

	// A member of the server's organization is named on their report.
	w, err = servePublicHTTP(t, ctx, ti, slug, feedbackCallBody(t, mcpfeedback.DefaultToolName, map[string]any{"content": "tool_alpha is well described."}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	rows = storedFeedback(t, ctx, ti, toolset.ID)
	require.Len(t, rows, 2)
	require.Equal(t, authCtx.UserID, rows[0].UserID.String)

	// Sending feedback is not one of the server's tool calls.
	require.Empty(t, ti.toolCallRecords.all())
}

// A report an agent can correct comes back as a tool result it can read.
func TestServePublic_FeedbackTool_HostedServerAnswersAnInvalidReportAsAToolError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "feedback-invalid-"+uuid.NewString()[:8])
	setFeedbackTool(t, ctx, ti, toolset.ID, true, "")

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, feedbackCallBody(t, mcpfeedback.DefaultToolName, map[string]any{"severity": "urgent"}), "", nil)
	require.NoError(t, err)

	outcome := decodeToolCall(t, w.Body.Bytes())
	require.NotNil(t, outcome.Result)
	require.True(t, outcome.Result.IsError)
	require.Equal(t, "Feedback was not recorded: content is required.", outcome.Result.Content[0].Text)
	require.Empty(t, storedFeedback(t, ctx, ti, toolset.ID))
}

// The tool's name is taken on the server: a tool of the server's own with the
// same name is neither listed nor called.
func TestServePublic_FeedbackTool_TakesItsNameOnAHostedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "feedback-taken-"+uuid.NewString()[:8])
	addHTTPTools(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_alpha", "report_issue")
	setFeedbackTool(t, ctx, ti, toolset.ID, true, "report_issue")
	slug := toolset.McpSlug.String

	w, err := servePublicHTTP(t, ctx, ti, slug, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	listed := parseToolsListResponse(t, w.Body.Bytes())
	require.Equal(t, []string{"tool_alpha", "report_issue"}, toolNames(listed))
	require.Equal(t, mcpfeedback.DefaultToolDescription, listed.Result.Tools[1].Description)

	w, err = servePublicHTTP(t, ctx, ti, slug, feedbackCallBody(t, "report_issue", map[string]any{"content": "tool_alpha is slow."}), "", nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "Feedback received")
	require.Len(t, storedFeedback(t, ctx, ti, toolset.ID), 1)
}

func TestServePublic_FeedbackTool_SwitchedOffIsNotATool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "feedback-off-"+uuid.NewString()[:8])
	addHTTPTools(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_alpha")
	setFeedbackTool(t, ctx, ti, toolset.ID, false, "")
	slug := toolset.McpSlug.String

	w, err := servePublicHTTP(t, ctx, ti, slug, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"tool_alpha"}, toolNames(parseToolsListResponse(t, w.Body.Bytes())))

	w, err = servePublicHTTP(t, ctx, ti, slug, feedbackCallBody(t, mcpfeedback.DefaultToolName, map[string]any{"content": "Is anyone there?"}), "", nil)
	require.NoError(t, err)
	outcome := decodeToolCall(t, w.Body.Bytes())
	require.Nil(t, outcome.Result)
	require.NotNil(t, outcome.Error, w.Body.String())
	require.Contains(t, outcome.Error.Message, "tool not found")
	require.Empty(t, storedFeedback(t, ctx, ti, toolset.ID))
}

// A gateway that was never given a recorder serves every server without the tool.
func TestServePublic_FeedbackTool_IsNotServedWithoutARecorder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "feedback-none-"+uuid.NewString()[:8])
	addHTTPTools(t, ctx, ti, toolset.ID, toolset.ProjectID, authCtx.ActiveOrganizationID, "tool_alpha")
	setFeedbackTool(t, ctx, ti, toolset.ID, true, "")

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"tool_alpha"}, toolNames(parseToolsListResponse(t, w.Body.Bytes())))
}

// methodRecordingUpstream is a remote MCP server that answers every request
// and remembers the methods it was asked for.
type methodRecordingUpstream struct {
	server *httptest.Server

	mu      sync.Mutex
	methods []string
}

func newMethodRecordingUpstream(t *testing.T) *methodRecordingUpstream {
	t.Helper()

	upstream := &methodRecordingUpstream{server: nil, mu: sync.Mutex{}, methods: nil}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The proxy's background metadata probe is not MCP traffic.
		if r.URL.Path == wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}

		body, _ := io.ReadAll(r.Body)
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &request)
		upstream.mu.Lock()
		upstream.methods = append(upstream.methods, request.Method)
		upstream.mu.Unlock()

		var result any
		switch request.Method {
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "ping",
				"description": "Returns pong",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "pong"}}, "isError": false}
		default:
			result = map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (u *methodRecordingUpstream) count(method string) int {
	u.mu.Lock()
	defer u.mu.Unlock()

	n := 0
	for _, seen := range u.methods {
		if seen == method {
			n++
		}
	}
	return n
}

// A remote server belongs to a third party, so the gateway never adds the
// feedback tool to it, even if settings stored for it say otherwise: its
// tools are listed and called exactly as the server serves them.
func TestServePublic_FeedbackTool_RemoteServerIsNeverOfferedIt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	upstream := newMethodRecordingUpstream(t)
	slug := "feedback-remote-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.server.URL, slug, "public", issuerID)
	token := mintIssuerBearerForEndpoint(t, ctx, ti, slug, mcpServer, authCtx.ActiveOrganizationID)
	setFeedbackTool(t, ctx, ti, mcpServer.ID, true, "")

	w, err := servePublicHTTP(t, ctx, ti, slug, makeToolsListBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"ping"}, toolNames(parseToolsListResponse(t, w.Body.Bytes())))

	_, err = servePublicHTTP(t, ctx, ti, slug, feedbackCallBody(t, mcpfeedback.DefaultToolName, map[string]any{"content": "x"}), token, nil)
	require.NoError(t, err)
	require.Equal(t, 1, upstream.count("tools/call"), "the call goes to the remote server as any other would")
	require.Empty(t, storedFeedback(t, ctx, ti, mcpServer.ID))
}

// seedFeedbackGateway puts a remote member and a hosted member, both with
// feedback on, behind a new gateway. The remote member's upstream records the
// calls it receives.
func seedFeedbackGateway(t *testing.T, ctx context.Context, ti *testInstance) (gatewayID uuid.UUID, slug string, remoteSlug string, remoteMember uuid.UUID, hosted hostedMemberFixture, upstream *recordingUpstream) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug = "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	gatewayID = gateway.ID
	upstream = newRecordingUpstream(t, "ping")
	remoteSlug = "remote-" + uuid.NewString()[:8]
	remoteMember = seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Remote tools", remoteSlug, 1, upstream.url)
	hosted = seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 2, mcpservers.VisibilityPublic, "alpha")

	setFeedbackTool(t, ctx, ti, remoteMember, true, "")
	setFeedbackTool(t, ctx, ti, hosted.serverID, true, "")

	return gatewayID, slug, remoteSlug, remoteMember, hosted, upstream
}

// toolCalls counts the tools/call requests the upstream received.
func (u *recordingUpstream) toolCalls() int {
	calls := 0
	for _, request := range u.journal() {
		if request.rpcMethod == "tools/call" {
			calls++
		}
	}
	return calls
}

// A gateway lists each member's feedback tool under the member's name, next to
// the member's own tools, and files a report against the member it names.
func TestGatewayOffersEachMembersFeedbackTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	gatewayID, slug, remoteSlug, remoteMember, hosted, upstream := seedFeedbackGateway(t, ctx, ti)
	setGatewayMode(t, ctx, ti, gatewayID, "direct")

	names := listDirectGateway(t, ti, slug, map[string]any{}).names()
	require.Contains(t, names, remoteSlug+"--ping")
	require.Contains(t, names, remoteSlug+"--"+mcpfeedback.DefaultToolName)
	require.Contains(t, names, hosted.slug+"--alpha")
	require.Contains(t, names, hosted.slug+"--"+mcpfeedback.DefaultToolName)

	remote := decodeMetaToolResult(t, callMetaTool(t, t.Context(), ti, slug, remoteSlug+"--"+mcpfeedback.DefaultToolName, map[string]any{"content": "ping never answers."}))
	require.False(t, remote.IsError, string(remote.StructuredContent))
	require.Len(t, storedFeedback(t, ctx, ti, remoteMember), 1)
	require.Zero(t, upstream.toolCalls(), "the member's upstream never sees a feedback call")

	local := decodeMetaToolResult(t, callMetaTool(t, t.Context(), ti, slug, hosted.slug+"--"+mcpfeedback.DefaultToolName, map[string]any{"content": "alpha returned nothing."}))
	require.False(t, local.IsError, string(local.StructuredContent))
	reports := storedFeedback(t, ctx, ti, hosted.serverID)
	require.Len(t, reports, 1)
	require.Equal(t, "alpha returned nothing.", reports[0].Content)
	require.Len(t, storedFeedback(t, ctx, ti, remoteMember), 1, "each report goes to the member it names")
}

// Drill-down gateways reach a member's feedback tool through describe and execute.
func TestGatewayDrillDownExecutesAMembersFeedbackTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	_, slug, remoteSlug, remoteMember, _, upstream := seedFeedbackGateway(t, ctx, ti)

	described := callMetaTool(t, t.Context(), ti, slug, "describe_tools", map[string]any{"tools": []string{remoteSlug + "--" + mcpfeedback.DefaultToolName}})
	require.NotContains(t, described, "error")
	require.Contains(t, string(described["result"]), mcpfeedback.DefaultToolName)

	executed := decodeMetaToolResult(t, callMetaTool(t, t.Context(), ti, slug, "execute_tool", map[string]any{
		"name":      remoteSlug + "--" + mcpfeedback.DefaultToolName,
		"arguments": map[string]any{"content": "ping never answers."},
	}))
	require.False(t, executed.IsError)
	require.Len(t, storedFeedback(t, ctx, ti, remoteMember), 1)
	require.Zero(t, upstream.toolCalls())
}

// A member with feedback off offers no feedback tool through a gateway.
func TestGatewayLeavesOutAFeedbackToolThatIsOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	gatewayID, slug, remoteSlug, remoteMember, hosted, _ := seedFeedbackGateway(t, ctx, ti)
	setGatewayMode(t, ctx, ti, gatewayID, "direct")
	setFeedbackTool(t, ctx, ti, remoteMember, false, "")
	setFeedbackTool(t, ctx, ti, hosted.serverID, false, "")

	names := listDirectGateway(t, ti, slug, map[string]any{}).names()
	require.Contains(t, names, remoteSlug+"--ping")
	for _, name := range names {
		require.NotContains(t, name, mcpfeedback.DefaultToolName)
	}
}

// A stopped server refuses its feedback tool like any of its own tools: the
// kill switch is checked before a local tool is answered.
func TestServePublic_FeedbackTool_StoppedHostedServerRefusesIt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "stopped-"+uuid.NewString()[:8])
	endpointSlug := "stopped-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, issuerID)
	setFeedbackTool(t, ctx, ti, mcpServer.ID, true, "")
	userToken := mintIssuerBearerForEndpointSubject(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID, urn.NewUserSubject(authCtx.UserID))
	insertHostedKillswitchPrescription(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, mcpServer.ID, "Tool calls paused for maintenance.")

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, feedbackCallBody(t, mcpfeedback.DefaultToolName, map[string]any{"content": "It broke."}), userToken, map[string]string{"Mcp-Session-Id": "stopped-session"})
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "mcp_tool_calls_paused")
	require.Empty(t, storedFeedback(t, ctx, ti, mcpServer.ID))
}

// A connection that approved only some of a member's tools still reaches the
// member's feedback tool: access to it follows access to the member, not to
// any one of its tools, so it is not part of what was approved.
func TestGatewayCachedCatalogOffersAMembersFeedbackTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	serveFeedback(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newRecordingUpstream(t, "ping")
	memberSlug := "source"
	member := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", memberSlug, 1, upstream.url)
	setFeedbackTool(t, ctx, ti, member, true, "")
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, []string{memberSlug + "--ping"})

	listed, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.NoError(t, err)
	require.Contains(t, listed.Body.String(), memberSlug+"--ping")
	require.Contains(t, listed.Body.String(), memberSlug+"--"+mcpfeedback.DefaultToolName)

	called, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      memberSlug + "--" + mcpfeedback.DefaultToolName,
		"arguments": map[string]any{"content": "ping never answers."},
	}), connection.token, nil)
	require.NoError(t, err)
	require.NotContains(t, called.Body.String(), "not approved", called.Body.String())
	require.Len(t, storedFeedback(t, ctx, ti, member), 1)
	require.Zero(t, upstream.toolCalls())
}

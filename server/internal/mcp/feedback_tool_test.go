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
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	mcpfeedbackrepo "github.com/speakeasy-api/gram/server/internal/mcpfeedback/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// serveFeedback has the test service answer feedback tools, as the server does.
func serveFeedback(t *testing.T, ti *testInstance) {
	t.Helper()

	ti.service.SetFeedback(mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, chatredact.New(), mcpfeedback.Limits{Caller: nil, Server: nil}, mcpfeedback.WithSettingsFreshFor(0)))
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

func TestServePublic_FeedbackTool_ProxiedServerListsAndRecordsIt(t *testing.T) {
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

	w, err := servePublicHTTP(t, ctx, ti, slug, makeToolsListBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"ping"}, toolNames(parseToolsListResponse(t, w.Body.Bytes())))

	setFeedbackTool(t, ctx, ti, mcpServer.ID, true, "acme_submit_feedback")

	w, err = servePublicHTTP(t, ctx, ti, slug, makeToolsListBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"ping", "acme_submit_feedback"}, toolNames(parseToolsListResponse(t, w.Body.Bytes())))

	w, err = servePublicHTTP(t, ctx, ti, slug, feedbackCallBody(t, "acme_submit_feedback", map[string]any{
		"content":  "ping answered pong but the docs promise a timestamp.",
		"severity": "cosmetic",
		"tool":     "ping",
	}), token, map[string]string{"Mcp-Session-Id": "session-remote"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	outcome := decodeToolCall(t, w.Body.Bytes())
	require.NotNil(t, outcome.Result, w.Body.String())
	require.False(t, outcome.Result.IsError)
	require.Contains(t, outcome.Result.Content[0].Text, "Feedback received")
	require.Zero(t, upstream.count("tools/call"), "the remote server never sees the feedback call")

	rows := storedFeedback(t, ctx, ti, mcpServer.ID)
	require.Len(t, rows, 1)
	require.Equal(t, "cosmetic", rows[0].Severity.String)
	require.Equal(t, "ping", rows[0].ToolName.String)

	// The server's own tools still reach it, and the default name is not taken here.
	w, err = servePublicHTTP(t, ctx, ti, slug, feedbackCallBody(t, "ping", map[string]any{}), token, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "pong")
	require.Equal(t, 1, upstream.count("tools/call"))

	_, err = servePublicHTTP(t, ctx, ti, slug, feedbackCallBody(t, mcpfeedback.DefaultToolName, map[string]any{"content": "x"}), token, nil)
	require.NoError(t, err)
	require.Equal(t, 2, upstream.count("tools/call"), "a name the server does not use for feedback is forwarded")
	require.Len(t, storedFeedback(t, ctx, ti, mcpServer.ID), 1)
}

// A gateway in front of several servers lists their tools, not one feedback
// tool per server.
func TestGatewayDoesNotListItsMembersFeedbackTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	serveFeedback(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newRecordingUpstream(t, "ping")
	remoteSlug := "remote-" + uuid.NewString()[:8]
	remoteMember := seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Remote tools", remoteSlug, 1, upstream.url)
	hostedMember := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 2, mcpservers.VisibilityPublic, "alpha")
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")

	setFeedbackTool(t, ctx, ti, remoteMember, true, "")
	setFeedbackTool(t, ctx, ti, hostedMember.serverID, true, "")
	setFeedbackTool(t, ctx, ti, hostedMember.toolsetID, true, "")

	names := listDirectGateway(t, ti, slug, map[string]any{}).names()
	require.Contains(t, names, remoteSlug+"--ping")
	require.Contains(t, names, hostedMember.slug+"--alpha")
	for _, name := range names {
		require.NotContains(t, name, mcpfeedback.DefaultToolName)
	}
}

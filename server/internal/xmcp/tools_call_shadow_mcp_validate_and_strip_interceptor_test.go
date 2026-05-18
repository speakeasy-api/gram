package xmcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// newToolsCallRequestWithArguments builds a ToolsCallRequest for the
// no-op gating tests. The underlying JSONRPCMessages slice is left
// empty because the interceptor short-circuits before reaching
// SetArguments in these scenarios.
func newToolsCallRequestWithArguments(toolName string, args json.RawMessage) *proxy.ToolsCallRequest {
	return &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{},
		Params: &mcp.CallToolParamsRaw{
			Name:      toolName,
			Arguments: args,
			Meta:      nil,
		},
	}
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))
	require.Equal(t, "tools-call-shadow-mcp-validate-and-strip", interceptor.Name())
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_NilParamsPassesThrough(t *testing.T) {
	t.Parallel()

	// Defensive: a nil Params (only reachable through direct
	// construction) must not panic.
	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))

	call := &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{},
		Params:      nil,
	}
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_InvalidProjectIDPassesThrough(t *testing.T) {
	t.Parallel()

	// Non-UUID project id short-circuits with a warning log — the call
	// flows through to upstream unchanged, since shadow-MCP cannot be
	// validated against an unknown project scope.
	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(newShadowMCPClientForTest(t), testServerID, "not-a-uuid", testenv.NewLogger(t))

	call := newToolsCallRequestWithArguments("tool_a", json.RawMessage(`{"x-gram-toolset-id":"abc"}`))
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	// Arguments must be unchanged (no validation, no strip).
	require.JSONEq(t, `{"x-gram-toolset-id":"abc"}`, string(call.Params.Arguments))
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_PolicyDisabledPassesThrough(t *testing.T) {
	t.Parallel()

	// With a fresh project (no enabled risk policies), the gate skips
	// validation and the arguments are forwarded verbatim — including
	// any x-gram-toolset-id property the caller happened to echo (no
	// validation, no strip).
	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))

	call := newToolsCallRequestWithArguments("tool_a", json.RawMessage(`{"x-gram-toolset-id":"abc","location":"sf"}`))
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, `{"x-gram-toolset-id":"abc","location":"sf"}`, string(call.Params.Arguments))
}

// enabledShadowMCPFixture seeds the test database with the minimum
// state needed to drive the validate+strip interceptor through its
// enabled-policy branch: an enabled shadow_mcp risk policy on the
// active project plus two remote_mcp_server rows the test can pick
// between for route-matching scenarios.
type enabledShadowMCPFixture struct {
	ti        *testInstance
	projectID uuid.UUID
	serverA   uuid.UUID
	serverB   uuid.UUID
	client    *shadowmcp.Client
}

func newEnabledShadowMCPFixture(t *testing.T, ctx context.Context, ti *testInstance) *enabledShadowMCPFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context must be initialized for the test instance")
	require.NotNil(t, authCtx.ProjectID, "auth context must carry a project id")

	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:               policyID,
		ProjectID:        *authCtx.ProjectID,
		OrganizationID:   authCtx.ActiveOrganizationID,
		Name:             "shadow-mcp-test-" + uuid.NewString()[:8],
		Sources:          []string{shadowmcp.SourceShadowMCP},
		PresidioEntities: nil,
		Enabled:          true,
		Action:           "block",
		AutoName:         false,
		UserMessage:      pgtype.Text{},
	})
	require.NoError(t, err)

	serverA := seedShadowMCPRemoteServer(t, ctx, ti, *authCtx.ProjectID, "srv-a")
	serverB := seedShadowMCPRemoteServer(t, ctx, ti, *authCtx.ProjectID, "srv-b")

	return &enabledShadowMCPFixture{
		ti:        ti,
		projectID: *authCtx.ProjectID,
		serverA:   serverA,
		serverB:   serverB,
		client:    ti.shadowMCPClient,
	}
}

func seedShadowMCPRemoteServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	require.NoError(t, err)
	server, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            id,
		ProjectID:     projectID,
		Name:          pgtype.Text{String: slug, Valid: true},
		Slug:          pgtype.Text{String: slug + "-" + uuid.NewString()[:8], Valid: true},
		TransportType: "streamable_http",
		Url:           "https://example.test/" + slug,
	})
	require.NoError(t, err)
	return server.ID
}

// newToolsCallRequestForSetArguments builds a ToolsCallRequest whose
// underlying JSONRPCMessages[0] is a real *jsonrpc.Request — required
// for SetArguments to commit the scrubbed payload back onto the wire
// shape after the validator passes.
func newToolsCallRequestForSetArguments(toolName string, args json.RawMessage) *proxy.ToolsCallRequest {
	rpcReq := &jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/call",
		Params: json.RawMessage(`{}`),
		Extra:  nil,
	}
	return &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{
			UserHTTPRequest: nil,
			JSONRPCMessages: []jsonrpc.Message{rpcReq},
		},
		Params: &mcp.CallToolParamsRaw{
			Name:      toolName,
			Arguments: args,
			Meta:      nil,
		},
	}
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_RouteMismatchRejects(t *testing.T) {
	t.Parallel()

	// Both serverA and serverB are real remote_mcp_servers in the
	// active project, so ValidateRemoteMCPServerCall on its own would
	// accept either UUID. The route-hardening check after validation
	// must catch the cross-server echo and reject with a clear
	// envelope so a caller cannot satisfy validation by echoing a
	// sibling server's UUID.
	ctx, ti := newTestService(t)
	f := newEnabledShadowMCPFixture(t, ctx, ti)

	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(f.client, f.serverA.String(), f.projectID.String(), testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`{"%s":"%s"}`, shadowmcp.XGramToolsetIDField, f.serverB))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	err := interceptor.InterceptToolsCallRequest(ctx, call)
	var rejErr *proxy.RejectError
	require.ErrorAs(t, err, &rejErr, "route mismatch must surface as a JSON-RPC rejection")
	require.Contains(t, rejErr.Message, "does not match the routed server")
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_MatchingEchoStripsProperty(t *testing.T) {
	t.Parallel()

	// The happy path: echo matches the routed serverID, validation
	// passes, the property is stripped from the forwarded arguments
	// so the upstream tool sees its declared shape.
	ctx, ti := newTestService(t)
	f := newEnabledShadowMCPFixture(t, ctx, ti)

	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(f.client, f.serverA.String(), f.projectID.String(), testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`{"%s":"%s","location":"sf"}`, shadowmcp.XGramToolsetIDField, f.serverA))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, call))
	require.NotContains(t, string(call.Params.Arguments), shadowmcp.XGramToolsetIDField, "echoed property must be stripped before forwarding")
	require.Contains(t, string(call.Params.Arguments), `"sf"`, "real tool arguments must survive the strip")
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_MalformedJSONArgumentsRejects(t *testing.T) {
	t.Parallel()

	// Arguments that don't decode as a JSON object surface a distinct
	// rejection message rather than the misleading "missing required
	// property" detail the validator would emit if we passed nil
	// through. The reject is gated on shadow-MCP being enabled — when
	// the policy is off, malformed bodies pass through to the upstream
	// tool's own validation.
	ctx, ti := newTestService(t)
	f := newEnabledShadowMCPFixture(t, ctx, ti)

	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(f.client, f.serverA.String(), f.projectID.String(), testenv.NewLogger(t))

	call := newToolsCallRequestForSetArguments("tool_a", json.RawMessage(`[1,2,3]`))

	err := interceptor.InterceptToolsCallRequest(ctx, call)
	var rejErr *proxy.RejectError
	require.ErrorAs(t, err, &rejErr, "malformed arguments must surface as a JSON-RPC rejection")
	require.Contains(t, rejErr.Message, "JSON object")
}

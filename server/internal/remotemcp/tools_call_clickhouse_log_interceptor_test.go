package remotemcp_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestToolsCallClickHouseLogInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsCallClickHouseLogInterceptor(telemetry.NewStub(testenv.NewLogger(t)), proxy.ServerIdentity{
		RemoteMCPServerID:   "server-id",
		TunneledMCPServerID: "",
		McpServerID:         "mcp-server-id",
	}, testenv.NewLogger(t))
	require.Equal(t, "tools-call-clickhouse-log", interceptor.Name())
}

func TestToolsCallClickHouseLogInterceptor_EmitsRow(t *testing.T) {
	t.Parallel()
	for _, caller := range []string{"legacy", "managed agent", "anonymous", "other organization"} {
		t.Run(caller, func(t *testing.T) { t.Parallel(); testToolsCallClickHouseEmission(t, caller) })
	}
}

func testToolsCallClickHouseEmission(t *testing.T, caller string) {
	t.Helper()

	logger := testenv.NewLogger(t)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	telemLogger := telemetry.NewLogger(t.Context(), logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), chConn, logsEnabled, toolIOLogsEnabled, nil, telemetry.NewNoopLogPublisher(testenv.NewLogger(t)))

	projectID := uuid.New()
	serverID := uuid.New().String()
	mcpServerID := uuid.New().String()
	ctx := t.Context()
	if caller != "anonymous" {
		ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
			ActiveOrganizationID: "org-test",
			UserID:               "user-123",
			APIKeyID:             "key-456",
			ProjectID:            &projectID,
		})
	}
	if caller == "anonymous" || caller == "other organization" {
		ctx = remotemcp.WithServerContext(ctx, remotemcp.ServerContext{
			OrganizationID: "org-owner", ProjectID: projectID,
		})
	}

	agentID := uuid.NewString()
	if caller == "managed agent" {
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		ctx = contextvalues.WithPrincipalAPIKeyAuthorization(ctx, authCtx,
			urn.NewPrincipal(urn.PrincipalTypeAgent, agentID),
			contextvalues.PrincipalCredential{AuthorizerUserID: "approver-id"})
		ctx = contextvalues.WithPrincipalCredentialOwner(ctx, "owner-id")
	}

	interceptor := remotemcp.NewToolsCallClickHouseLogInterceptor(telemLogger, proxy.ServerIdentity{
		RemoteMCPServerID:   serverID,
		TunneledMCPServerID: "",
		McpServerID:         mcpServerID,
	}, logger)

	req := &proxy.ToolsCallRequest{
		UserRequest: nil,
		Params: &mcp.CallToolParamsRaw{
			Arguments: []byte(`{"q":"hi"}`),
			Meta:      nil,
			Name:      "list_things",
		},
	}

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, req))

	rpcResp, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	require.NoError(t, err)

	resp := &proxy.ToolsCallResponse{
		Error: nil,
		RemoteMessage: &proxy.RemoteMessage{
			UserHTTPRequest:    nil,
			RemoteHTTPRequest:  nil,
			RemoteHTTPResponse: &http.Response{StatusCode: http.StatusOK},
			Message:            rpcResp,
		},
		Request: req,
		Result: &mcp.CallToolResult{
			Content:           nil,
			IsError:           false,
			Meta:              nil,
			StructuredContent: nil,
		},
	}

	require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, resp))

	// The two correlation ids are distinct, so a row matching both columns
	// proves the interceptor records the fronting mcp_servers id alongside
	// the remote_mcp_servers id rather than reusing one for both. A single
	// shared id is what masked the PR #3405 RBAC bug.
	require.NotEqual(t, serverID, mcpServerID)

	// Emission is fire-and-forget; poll the read side until the row appears.
	expectedURN := "tools:externalmcp:" + serverID + ":list_things"
	var gotArgs string
	var gotResult string
	require.Eventually(t, func() bool {
		row := chConn.QueryRow(t.Context(),
			`SELECT
			       toString(attributes.gen_ai.tool.call.arguments),
			       toString(attributes.gen_ai.tool.call.result)
			 FROM telemetry_logs
			 WHERE gram_project_id = ?
			   AND gram_urn = ?
			   AND trace_id IS NOT NULL
			   AND span_id IS NOT NULL
			   AND remote_mcp_server_id = ?
			   AND mcp_server_id = ?
			   AND tool_name = ?
			   AND event_source = ?
			   AND toInt32OrZero(toString(attributes.http.response.status_code)) = 200
			 LIMIT 1`,
			projectID.String(), expectedURN, serverID, mcpServerID, "list_things", "tool_call")
		var args string
		var result string
		if err := row.Scan(&args, &result); err != nil {
			return false
		}
		gotArgs = args
		gotResult = result
		return gotArgs != "" && gotResult != ""
	}, 5*time.Second, 50*time.Millisecond, "telemetry_logs row did not appear")
	require.JSONEq(t, `{"q":"hi"}`, gotArgs)
	require.JSONEq(t, `{"ok":true}`, gotResult)
	if caller == "anonymous" || caller == "other organization" {
		var organizationID, userID, apiKeyID, actorID string
		err := chConn.QueryRow(t.Context(), `SELECT
    toString(attributes.gram.org.id), user_id,
    toString(attributes.gram.api_key.id), toString(attributes.gram.authorization.actor.id)
   FROM telemetry_logs WHERE gram_project_id = ? AND gram_urn = ? LIMIT 1`,
			projectID.String(), expectedURN).Scan(&organizationID, &userID, &apiKeyID, &actorID)
		require.NoError(t, err)
		require.Equal(t, "org-owner", organizationID)
		require.Empty(t, userID)
		require.Empty(t, apiKeyID)
		require.Empty(t, actorID)
	}
	if caller == "managed agent" {
		var actorType, actorID, authorizerID string
		err := chConn.QueryRow(t.Context(), `SELECT
    toString(attributes.gram.authorization.actor.type),
    toString(attributes.gram.authorization.actor.id),
    toString(attributes.gram.authorization.authorizer_user_id)
   FROM telemetry_logs WHERE gram_project_id = ? AND gram_urn = ? LIMIT 1`,
			projectID.String(), expectedURN).Scan(&actorType, &actorID, &authorizerID)
		require.NoError(t, err)
		require.Equal(t, "agent", actorType)
		require.Equal(t, agentID, actorID)
		require.Equal(t, "approver-id", authorizerID)
		// Raw rows can become visible before their materialized-view projection.
		require.Eventually(t, func() bool {
			var summarizedAgentID string
			err := chConn.QueryRow(t.Context(),
				"SELECT max(agent_id) FROM trace_summaries WHERE gram_project_id = ?", projectID.String()).Scan(&summarizedAgentID)
			return err == nil && summarizedAgentID == agentID
		}, 5*time.Second, 50*time.Millisecond, "trusted agent attribution did not reach trace_summaries")
	}

}

func TestToolsCallClickHouseLogInterceptor_DurationMissingSentinel(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return false, nil }
	telemLogger := telemetry.NewLogger(t.Context(), logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), chConn, logsEnabled, toolIOLogsEnabled, nil, telemetry.NewNoopLogPublisher(testenv.NewLogger(t)))

	projectID := uuid.New()
	serverID := uuid.New().String()
	mcpServerID := uuid.New().String()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		ProjectID:            &projectID,
	})

	interceptor := remotemcp.NewToolsCallClickHouseLogInterceptor(telemLogger, proxy.ServerIdentity{
		RemoteMCPServerID:   serverID,
		TunneledMCPServerID: "",
		McpServerID:         mcpServerID,
	}, logger)

	// Skip the request side so the response has no stashed start time.
	req := &proxy.ToolsCallRequest{
		UserRequest: nil,
		Params: &mcp.CallToolParamsRaw{
			Arguments: []byte(`{}`),
			Meta:      nil,
			Name:      "orphan",
		},
	}
	rpcResp, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`))
	require.NoError(t, err)

	resp := &proxy.ToolsCallResponse{
		Error: nil,
		RemoteMessage: &proxy.RemoteMessage{
			UserHTTPRequest:    nil,
			RemoteHTTPRequest:  nil,
			RemoteHTTPResponse: &http.Response{StatusCode: http.StatusOK},
			Message:            rpcResp,
		},
		Request: req,
		Result: &mcp.CallToolResult{
			Content:           nil,
			IsError:           false,
			Meta:              nil,
			StructuredContent: nil,
		},
	}

	require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, resp))

	expectedURN := "tools:externalmcp:" + serverID + ":orphan"
	require.Eventually(t, func() bool {
		var count uint64
		row := chConn.QueryRow(t.Context(),
			`SELECT count() FROM telemetry_logs
			 WHERE gram_project_id = ?
			   AND gram_urn = ?
			   AND toString(attributes.gram.telemetry.duration_missing) = 'true'`,
			projectID.String(), expectedURN)
		if err := row.Scan(&count); err != nil {
			return false
		}
		return count == 1
	}, 5*time.Second, 50*time.Millisecond, "duration-missing row did not appear")
}

func TestToolsCallClickHouseLogInterceptor_NoAuthContextSkips(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	telemLogger := telemetry.NewLogger(t.Context(), logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), chConn, logsEnabled, toolIOLogsEnabled, nil, telemetry.NewNoopLogPublisher(testenv.NewLogger(t)))

	serverID := uuid.New().String()
	mcpServerID := uuid.New().String()
	interceptor := remotemcp.NewToolsCallClickHouseLogInterceptor(telemLogger, proxy.ServerIdentity{
		RemoteMCPServerID:   serverID,
		TunneledMCPServerID: "",
		McpServerID:         mcpServerID,
	}, logger)

	// Deliberately call without an auth context on ctx — the response side
	// must short-circuit and emit nothing.
	req := &proxy.ToolsCallRequest{
		UserRequest: nil,
		Params: &mcp.CallToolParamsRaw{
			Arguments: []byte(`{}`),
			Meta:      nil,
			Name:      "no_auth",
		},
	}
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), req))
	require.NoError(t, interceptor.InterceptToolsCallResponse(t.Context(), &proxy.ToolsCallResponse{
		Error: nil,
		RemoteMessage: &proxy.RemoteMessage{
			UserHTTPRequest:    nil,
			RemoteHTTPRequest:  nil,
			RemoteHTTPResponse: &http.Response{StatusCode: http.StatusOK},
			Message:            nil,
		},
		Request: req,
		Result:  nil,
	}))

	// Poll for the full fire-and-forget window the other tests use and
	// require that no row carrying this server id ever appears.
	expectedURN := "tools:externalmcp:" + serverID + ":no_auth"
	require.Never(t, func() bool {
		var count uint64
		row := chConn.QueryRow(t.Context(),
			`SELECT count() FROM telemetry_logs
			 WHERE gram_urn = ?
			    OR remote_mcp_server_id = ?`,
			expectedURN, serverID)
		if err := row.Scan(&count); err != nil {
			return false
		}
		return count > 0
	}, 1*time.Second, 50*time.Millisecond, "telemetry_logs row was emitted despite missing auth context")
}

// upstreamStatusCode mapping edge cases — exercised through the public
// interceptor surface and verified via the materialized status_code column.

func TestToolsCallClickHouseLogInterceptor_JSONRPCErrorWith2xxMapsTo500(t *testing.T) {
	t.Parallel()

	statusCode := runStatusCodeMappingCase(t, statusCodeCase{
		toolName: "rpc_err_2xx",
		response: func(req *proxy.ToolsCallRequest) *proxy.ToolsCallResponse {
			return &proxy.ToolsCallResponse{
				Error: &jsonrpc.Error{Code: -32000, Message: "upstream rpc failure"},
				RemoteMessage: &proxy.RemoteMessage{
					UserHTTPRequest:    nil,
					RemoteHTTPRequest:  nil,
					RemoteHTTPResponse: &http.Response{StatusCode: http.StatusOK},
					Message:            nil,
				},
				Request: req,
				Result:  nil,
			}
		},
	})
	require.Equal(t, int32(500), statusCode, "JSON-RPC Error with upstream 2xx should map to 500")
}

func TestToolsCallClickHouseLogInterceptor_JSONRPCErrorWithoutHTTPResponseMapsTo500(t *testing.T) {
	t.Parallel()

	statusCode := runStatusCodeMappingCase(t, statusCodeCase{
		toolName: "rpc_err_no_http",
		response: func(req *proxy.ToolsCallRequest) *proxy.ToolsCallResponse {
			return &proxy.ToolsCallResponse{
				Error: &jsonrpc.Error{Code: -32603, Message: "internal"},
				RemoteMessage: &proxy.RemoteMessage{
					UserHTTPRequest:    nil,
					RemoteHTTPRequest:  nil,
					RemoteHTTPResponse: nil,
					Message:            nil,
				},
				Request: req,
				Result:  nil,
			}
		},
	})
	require.Equal(t, int32(500), statusCode, "JSON-RPC Error with no upstream HTTP response should map to 500")
}

func TestToolsCallClickHouseLogInterceptor_JSONRPCErrorWith4xxPreservesCode(t *testing.T) {
	t.Parallel()

	statusCode := runStatusCodeMappingCase(t, statusCodeCase{
		toolName: "rpc_err_401",
		response: func(req *proxy.ToolsCallRequest) *proxy.ToolsCallResponse {
			return &proxy.ToolsCallResponse{
				Error: &jsonrpc.Error{Code: -32001, Message: "unauthorized"},
				RemoteMessage: &proxy.RemoteMessage{
					UserHTTPRequest:    nil,
					RemoteHTTPRequest:  nil,
					RemoteHTTPResponse: &http.Response{StatusCode: http.StatusUnauthorized},
					Message:            nil,
				},
				Request: req,
				Result:  nil,
			}
		},
	})
	require.Equal(t, int32(401), statusCode, "JSON-RPC Error with upstream 4xx should preserve the upstream code")
}

func TestToolsCallClickHouseLogInterceptor_ToolErrorResultIsFailed(t *testing.T) {
	t.Parallel()

	toolError := func(httpStatus int) func(*proxy.ToolsCallRequest) *proxy.ToolsCallResponse {
		return func(req *proxy.ToolsCallRequest) *proxy.ToolsCallResponse {
			return &proxy.ToolsCallResponse{
				Error: nil,
				RemoteMessage: &proxy.RemoteMessage{
					UserHTTPRequest:    nil,
					RemoteHTTPRequest:  nil,
					RemoteHTTPResponse: &http.Response{StatusCode: httpStatus},
					Message:            nil,
				},
				Request: req,
				Result:  &mcp.CallToolResult{Content: nil, IsError: true, Meta: nil, StructuredContent: nil},
			}
		}
	}

	tests := []struct {
		name       string
		httpStatus int
		want       int32
	}{
		// Tool Logs counts a call as failed from a status of 400 or above, so
		// a tool error on a successful exchange must not read as a success.
		{name: "2xx maps to 500", httpStatus: http.StatusOK, want: 500},
		{name: "4xx is kept", httpStatus: http.StatusNotFound, want: 404},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			statusCode := runStatusCodeMappingCase(t, statusCodeCase{
				toolName: "tool_err_" + strconv.Itoa(tc.httpStatus),
				response: toolError(tc.httpStatus),
				failure:  nil,
			})
			require.Equal(t, tc.want, statusCode)
		})
	}
}

// The failure chain must reuse the request chain's instance: the row's
// duration comes from the start time the request side stashed.
func TestProxyManager_LogsToolCallsWithoutReply(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	manager := remotemcp.NewProxyManager(logger, tracerProvider, meterProvider,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		mcpriskscan.NewNoop(tracerProvider, meterProvider, logger), nil)
	p := manager.BuildTarget(logger,
		proxy.ServerIdentity{TunneledMCPServerID: uuid.NewString(), McpServerID: uuid.NewString()},
		"https://tunnel.example.com/mcp", nil, mcpservers.VisibilityPublic, "org-test", uuid.NewString(), "", "", nil)

	require.Len(t, p.ToolsCallFailureInterceptors, 1, "only the log row is written for a call without a reply")
	failureLog, ok := p.ToolsCallFailureInterceptors[0].(*remotemcp.ToolsCallClickHouseLogInterceptor)
	require.True(t, ok)
	var requestLog proxy.ToolsCallRequestInterceptor
	for _, interceptor := range p.ToolsCallRequestInterceptors {
		if interceptor.Name() == failureLog.Name() {
			requestLog = interceptor
		}
	}
	require.Same(t, failureLog, requestLog)
}

// A forwarded call whose reply never arrived still writes a failed row.
func TestToolsCallClickHouseLogInterceptor_FailedCallWithoutReply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		upstream   int
		proxyError error
		want       int32
	}{
		{name: "upstream error status is kept", upstream: http.StatusBadGateway, proxyError: nil, want: 502},
		{name: "unreachable upstream", upstream: 0, proxyError: oops.E(oops.CodeGatewayError, nil, "remote mcp server unreachable"), want: 502},
		{name: "proxy error status", upstream: 0, proxyError: oops.E(oops.CodeUnauthorized, nil, "could not establish tunnel caller identity"), want: 401},
		{name: "2xx without a tools/call reply", upstream: http.StatusOK, proxyError: nil, want: 502},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			statusCode := runStatusCodeMappingCase(t, statusCodeCase{
				toolName: "no_reply_" + strconv.Itoa(i),
				response: nil,
				failure: func(req *proxy.ToolsCallRequest) *proxy.ToolsCallFailure {
					return &proxy.ToolsCallFailure{Request: req, StatusCode: tc.upstream, Err: tc.proxyError}
				},
			})
			require.Equal(t, tc.want, statusCode)
		})
	}
}

type statusCodeCase struct {
	toolName string
	response func(*proxy.ToolsCallRequest) *proxy.ToolsCallResponse
	// failure, when set, reports the call to the failure interceptor
	// instead of passing a response.
	failure func(*proxy.ToolsCallRequest) *proxy.ToolsCallFailure
}

// runStatusCodeMappingCase drives one tools/call through a fresh interceptor
// and returns the materialized http.response.status_code recorded for the row,
// so each upstreamStatusCode branch can be asserted against a real telemetry
// row.
func runStatusCodeMappingCase(t *testing.T, tc statusCodeCase) int32 {
	t.Helper()

	logger := testenv.NewLogger(t)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	telemLogger := telemetry.NewLogger(t.Context(), logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, logsEnabled, toolIOLogsEnabled, nil, telemetry.NewNoopLogPublisher(testenv.NewLogger(t)))

	projectID := uuid.New()
	serverID := uuid.New().String()
	mcpServerID := uuid.New().String()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		ProjectID:            &projectID,
	})

	interceptor := remotemcp.NewToolsCallClickHouseLogInterceptor(telemLogger, proxy.ServerIdentity{
		RemoteMCPServerID:   serverID,
		TunneledMCPServerID: "",
		McpServerID:         mcpServerID,
	}, logger)

	req := &proxy.ToolsCallRequest{
		UserRequest: nil,
		Params: &mcp.CallToolParamsRaw{
			Arguments: []byte(`{}`),
			Meta:      nil,
			Name:      tc.toolName,
		},
	}

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, req))
	if tc.failure != nil {
		interceptor.InterceptToolsCallFailure(ctx, tc.failure(req))
	} else {
		require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, tc.response(req)))
	}

	expectedURN := "tools:externalmcp:" + serverID + ":" + tc.toolName
	var statusCode int32
	var result string
	require.Eventually(t, func() bool {
		row := conn.QueryRow(t.Context(),
			`SELECT toInt32OrZero(toString(attributes.http.response.status_code)),
			       toString(attributes.gen_ai.tool.call.result)
			 FROM telemetry_logs
			 WHERE gram_project_id = ? AND gram_urn = ?`,
			projectID.String(), expectedURN)
		var got int32
		if err := row.Scan(&got, &result); err != nil {
			return false
		}
		if got == 0 {
			return false
		}
		statusCode = got
		return true
	}, 5*time.Second, 50*time.Millisecond, "telemetry_logs row did not appear")
	if tc.failure != nil {
		require.Empty(t, result, "a call without a reply records no output")
	}

	return statusCode
}

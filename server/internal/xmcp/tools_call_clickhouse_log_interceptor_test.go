package xmcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

func TestToolsCallClickHouseLogInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsCallClickHouseLogInterceptor(telemetry.NewStub(testenv.NewLogger(t)), "server-id", testenv.NewLogger(t))
	require.Equal(t, "tools-call-clickhouse-log", interceptor.Name())
}

func TestToolsCallClickHouseLogInterceptor_EmitsRow(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	telemLogger := telemetry.NewLogger(t.Context(), logger, chConn, logsEnabled, toolIOLogsEnabled)

	projectID := uuid.New()
	serverID := uuid.New().String()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		UserID:               "user-123",
		APIKeyID:             "key-456",
		ProjectID:            &projectID,
	})

	interceptor := xmcp.NewToolsCallClickHouseLogInterceptor(telemLogger, serverID, logger)

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

	// Emission is fire-and-forget; poll the read side until the row appears.
	expectedURN := "tools:externalmcp:" + serverID + ":list_things"
	require.Eventually(t, func() bool {
		var count uint64
		row := chConn.QueryRow(t.Context(),
			`SELECT count() FROM telemetry_logs
			 WHERE gram_project_id = ?
			   AND gram_urn = ?
			   AND remote_mcp_server_id = ?
			   AND tool_name = ?
			   AND event_source = ?
			   AND toInt32OrZero(toString(attributes.http.response.status_code)) = 200`,
			projectID.String(), expectedURN, serverID, "list_things", "tool_call")
		if err := row.Scan(&count); err != nil {
			return false
		}
		return count == 1
	}, 5*time.Second, 50*time.Millisecond, "telemetry_logs row did not appear")
}

func TestToolsCallClickHouseLogInterceptor_DurationMissingSentinel(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	logsEnabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	toolIOLogsEnabled := func(_ context.Context, _ string) (bool, error) { return false, nil }
	telemLogger := telemetry.NewLogger(t.Context(), logger, chConn, logsEnabled, toolIOLogsEnabled)

	projectID := uuid.New()
	serverID := uuid.New().String()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		ProjectID:            &projectID,
	})

	interceptor := xmcp.NewToolsCallClickHouseLogInterceptor(telemLogger, serverID, logger)

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
	telemLogger := telemetry.NewLogger(t.Context(), logger, chConn, logsEnabled, toolIOLogsEnabled)

	serverID := uuid.New().String()
	interceptor := xmcp.NewToolsCallClickHouseLogInterceptor(telemLogger, serverID, logger)

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

type statusCodeCase struct {
	toolName string
	response func(*proxy.ToolsCallRequest) *proxy.ToolsCallResponse
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
	telemLogger := telemetry.NewLogger(t.Context(), logger, conn, logsEnabled, toolIOLogsEnabled)

	projectID := uuid.New()
	serverID := uuid.New().String()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		ProjectID:            &projectID,
	})

	interceptor := xmcp.NewToolsCallClickHouseLogInterceptor(telemLogger, serverID, logger)

	req := &proxy.ToolsCallRequest{
		UserRequest: nil,
		Params: &mcp.CallToolParamsRaw{
			Arguments: []byte(`{}`),
			Meta:      nil,
			Name:      tc.toolName,
		},
	}

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, req))
	require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, tc.response(req)))

	expectedURN := "tools:externalmcp:" + serverID + ":" + tc.toolName
	var statusCode int32
	require.Eventually(t, func() bool {
		row := conn.QueryRow(t.Context(),
			`SELECT toInt32OrZero(toString(attributes.http.response.status_code)) FROM telemetry_logs
			 WHERE gram_project_id = ? AND gram_urn = ?`,
			projectID.String(), expectedURN)
		var got int32
		if err := row.Scan(&got); err != nil {
			return false
		}
		if got == 0 {
			return false
		}
		statusCode = got
		return true
	}, 5*time.Second, 50*time.Millisecond, "telemetry_logs row did not appear")

	return statusCode
}

package remotemcp_test

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
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// An upstream that answers 200 but flags isError keeps its status code, so the
// row needs the separate signal or Tool Logs reads the call as a success.
func TestToolsCallClickHouseLogInterceptor_RecordsInBandToolError(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		it            string
		isError       bool
		wantToolError string
	}{
		{it: "records the reason when the upstream flags isError", isError: true, wantToolError: "upstream_result_error"},
		{it: "leaves the attribute unset on a clean result", isError: false, wantToolError: ""},
	} {
		t.Run(tt.it, func(t *testing.T) {
			t.Parallel()

			logger := testenv.NewLogger(t)
			chConn, err := infra.NewClickhouseClient(t)
			require.NoError(t, err)

			enabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
			telemLogger := telemetry.NewLogger(t.Context(), logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), chConn, enabled, enabled, nil, telemetry.NewNoopLogPublisher(testenv.NewLogger(t)))

			projectID := uuid.New()
			serverID := uuid.New().String()
			ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
				ActiveOrganizationID: "org-test",
				UserID:               "user-123",
				APIKeyID:             "key-456",
				ProjectID:            &projectID,
			})

			interceptor := remotemcp.NewToolsCallClickHouseLogInterceptor(telemLogger, proxy.ServerIdentity{
				RemoteMCPServerID:   serverID,
				TunneledMCPServerID: "",
				McpServerID:         uuid.New().String(),
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

			require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, &proxy.ToolsCallResponse{
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
					IsError:           tt.isError,
					Meta:              nil,
					StructuredContent: nil,
				},
			}))

			expectedURN := "tools:externalmcp:" + serverID + ":list_things"
			var gotToolError string
			var gotStatus int32
			require.Eventually(t, func() bool {
				row := chConn.QueryRow(t.Context(),
					`SELECT
					       toString(attributes.gram.tool_call.error),
					       toInt32OrZero(toString(attributes.http.response.status_code))
					 FROM telemetry_logs
					 WHERE gram_project_id = ? AND gram_urn = ?
					 LIMIT 1`,
					projectID.String(), expectedURN)
				return row.Scan(&gotToolError, &gotStatus) == nil
			}, 5*time.Second, 50*time.Millisecond, "telemetry_logs row did not appear")

			require.Equal(t, tt.wantToolError, gotToolError)
			// The status code is reported as the upstream sent it either way.
			require.Equal(t, int32(http.StatusOK), gotStatus)
		})
	}
}

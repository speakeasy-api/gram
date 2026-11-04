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
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// rowCredential is the auth client attribution read back off a tool-call row.
type rowCredential struct {
	authorizerID string
	clientType   string
	clientID     string
	ownerID      string
}

// emitAgentToolCallRow writes one agent tool-call row through the interceptor
// with credential on the context, and reads the row's attribution back.
func emitAgentToolCallRow(t *testing.T, credential *contextvalues.UpstreamCredential) rowCredential {
	t.Helper()

	logger := testenv.NewLogger(t)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	enabled := func(_ context.Context, _ string) (bool, error) { return true, nil }
	telemLogger := telemetry.NewLogger(t.Context(), logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), chConn, enabled, enabled, nil, telemetry.NewNoopLogPublisher(testenv.NewLogger(t)))

	projectID := uuid.New()
	serverID := uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		UserID:               "user-123",
		APIKeyID:             "key-456",
		ProjectID:            &projectID,
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = contextvalues.WithPrincipalAPIKeyAuthorization(ctx, authCtx,
		urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()),
		contextvalues.PrincipalCredential{AuthorizerUserID: "key-creator-id"})
	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, "key-owner-id")
	if credential != nil {
		ctx = contextvalues.SetUpstreamCredential(ctx, *credential)
	}

	interceptor := remotemcp.NewToolsCallClickHouseLogInterceptor(telemLogger, proxy.ServerIdentity{
		RemoteMCPServerID:   serverID,
		TunneledMCPServerID: "",
		McpServerID:         uuid.NewString(),
	}, logger)
	req := &proxy.ToolsCallRequest{
		UserRequest: nil,
		Params:      &mcp.CallToolParamsRaw{Arguments: []byte(`{}`), Meta: nil, Name: "list_things"},
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
		Result:  &mcp.CallToolResult{Content: nil, IsError: false, Meta: nil, StructuredContent: nil},
	}))

	var got rowCredential
	require.Eventually(t, func() bool {
		err := chConn.QueryRow(t.Context(), `SELECT
    toString(attributes.gram.authorization.authorizer_user_id),
    toString(attributes.gram.auth_client.type),
    toString(attributes.gram.auth_client.id),
    toString(attributes.gram.auth_client.connection_owner_user_id)
   FROM telemetry_logs WHERE gram_project_id = ? AND gram_urn = ? AND event_source = 'tool_call' LIMIT 1`,
			projectID.String(), "tools:externalmcp:"+serverID+":list_things").Scan(&got.authorizerID, &got.clientType, &got.clientID, &got.ownerID)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "telemetry_logs row did not appear")
	return got
}

func TestToolsCallClickHouseLogInterceptor_RecordsPersonalConnection(t *testing.T) {
	t.Parallel()
	clientID := uuid.NewString()

	got := emitAgentToolCallRow(t, &contextvalues.UpstreamCredential{
		AuthClientID:          clientID,
		AuthClientType:        "remote_session",
		ConnectionOwnerUserID: "connection-owner-id",
	})

	require.Equal(t, rowCredential{
		authorizerID: "key-creator-id",
		clientType:   "remote_session",
		clientID:     clientID,
		ownerID:      "connection-owner-id",
	}, got)
}

func TestToolsCallClickHouseLogInterceptor_RecordsSharedCredential(t *testing.T) {
	t.Parallel()
	clientID := uuid.NewString()

	got := emitAgentToolCallRow(t, &contextvalues.UpstreamCredential{
		AuthClientID:          clientID,
		AuthClientType:        "service_account",
		ConnectionOwnerUserID: "",
	})

	// The key creator stays on the row as credential provenance; no
	// connection owner is recorded for a shared credential.
	require.Equal(t, rowCredential{
		authorizerID: "key-creator-id",
		clientType:   "service_account",
		clientID:     clientID,
		ownerID:      "",
	}, got)
}

func TestToolsCallClickHouseLogInterceptor_NoCredentialRecordsNoAuthClient(t *testing.T) {
	t.Parallel()

	got := emitAgentToolCallRow(t, nil)

	require.Equal(t, rowCredential{authorizerID: "key-creator-id", clientType: "", clientID: "", ownerID: ""}, got)
}

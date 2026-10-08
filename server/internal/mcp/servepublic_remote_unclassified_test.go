package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A remote MCP server's tool with no stored metadata has no disposition. A
// grant narrowed only by disposition therefore does not reach it: tools/list
// withholds it and tools/call refuses it, while a grant naming the tool still
// admits it.
func TestServePublic_PrivateRemote_DispositionGrantExcludesUnclassifiedTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	const toolName = "ping"
	upstream := newStatelessRemoteMCPUpstream(t, toolName, nil)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "endpoint-" + uuid.NewString()
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, endpointSlug, "private", issuerID)

	seedMockUserMCPGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mcpServer.ID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})

	token := mintMetaIssuerBearer(t, ti, endpointSlug, issuerID, urn.NewUserSubject(mockidp.MockUserID))

	initResp, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, initResp.Code, "initialize: %s", initResp.Body.String())

	listResp, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeToolsListBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listResp.Code, "tools/list: %s", listResp.Body.String())
	tools, ok := decodeMCPResult(t, listResp.Body.Bytes())["tools"].([]any)
	require.True(t, ok, "tools/list result must carry a tools array: %s", listResp.Body.String())
	require.Empty(t, tools, "a disposition-only grant must not reach a tool with no stored metadata")

	callBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params":  map[string]any{"name": toolName, "arguments": map[string]any{}},
	})
	require.NoError(t, err)
	callResp, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, callBody, token, nil)
	require.NoError(t, err)
	require.Contains(t, callResp.Body.String(), `"error"`, "tools/call must be refused: %s", callResp.Body.String())
	require.NotContains(t, callResp.Body.String(), "pong")

	// Naming the tool reaches it whatever its classification.
	seedMockUserMCPGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mcpServer.ID, map[string]string{authz.SelectorKeyTool: toolName})

	callResp, err = servePublicHTTP(t, context.Background(), ti, endpointSlug, callBody, token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call: %s", callResp.Body.String())
	decodeMCPResult(t, callResp.Body.Bytes())
}

package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
)

// parameterHeaderSchema annotates owner and repo with x-mcp-header, so the
// SDK upstream rejects a tools/call that does not mirror them.
func parameterHeaderSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"owner": map[string]any{"type": "string", "x-mcp-header": "owner"},
			"repo":  map[string]any{"type": "string", "x-mcp-header": "repo"},
			"path":  map[string]any{"type": "string"},
		},
	}
}

// parameterHeaderArguments includes a non-ASCII value, which must travel
// Base64-encoded.
const parameterHeaderArguments = `{"owner":"octo-org","repo":"héllo","path":"README.md"}`

func TestServePublic_MetaEndpoint_ExecuteTool_MirrorsParameterHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-param-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream())
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Repo", "repo", 0, upstream.url)
	subject := createTestUser(t, ctx, ti, "meta-param-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)

	body := makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "execute_tool",
		"arguments": map[string]any{"name": "repo--get_file_contents", "arguments": json.RawMessage(parameterHeaderArguments)},
	})
	resp, err := servePublicHTTP(t, ctx, ti, metaSlug, body, bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "execute_tool response: %s", resp.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, resp))
	require.False(t, isError, "the member must accept the mirrored parameter headers: %s", text)
	require.Contains(t, text, "pong from get_file_contents")

	methods := map[string]int{}
	for _, req := range upstream.journal() {
		methods[req.rpcMethod]++
	}
	require.Equal(t, 2, methods["tools/call"], "the refused call is retried once")
	require.Equal(t, 1, methods["tools/list"], "the retry reads the tool's current definition")
}

func TestCodeBridgeMirrorsParameterHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream())
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Repo", "repo", 0, upstream.url)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service.Service, gateway.ID)

	for range 2 {
		result, err := host.Call(ctx, "repo--get_file_contents", json.RawMessage(parameterHeaderArguments))
		require.NoError(t, err)
		require.True(t, result.OK, "%+v", result)
		require.Contains(t, string(result.Content), "pong from get_file_contents")
	}
}

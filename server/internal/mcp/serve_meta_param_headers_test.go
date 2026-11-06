package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
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

// parameterHeaderGateway is a gateway with one member, "repo", whose upstream
// serves get_file_contents.
type parameterHeaderGateway struct {
	ti       *testInstance
	slug     string
	metaID   uuid.UUID
	bearer   string
	recorder *mcpconnectionattemptstest.Recording
}

func newParameterHeaderGateway(t *testing.T, upstreamURL string) parameterHeaderGateway {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "meta-param-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, slug, issuerID)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Repo", "repo", 0, upstreamURL)
	subject := createTestUser(t, ctx, ti, "meta-param-user-"+uuid.NewString())
	return parameterHeaderGateway{ti: ti, slug: slug, metaID: meta.ID, bearer: mintMetaIssuerBearer(t, ti, slug, issuerID, subject), recorder: rec}
}

func (g parameterHeaderGateway) execute(t *testing.T) {
	t.Helper()
	text, isError := metaToolResultText(t, decodeToolsCall(t, g.ti, g.slug, g.bearer, "execute_tool", map[string]any{
		"name": "repo--get_file_contents", "arguments": json.RawMessage(parameterHeaderArguments),
	}))
	require.False(t, isError, "the member must accept the mirrored parameter headers: %s", text)
	require.Contains(t, text, "pong from get_file_contents")
}

func decodeToolsCall(t *testing.T, ti *testInstance, slug, bearer, name string, arguments map[string]any) map[string]json.RawMessage {
	t.Helper()
	resp, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": name, "arguments": arguments}), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "%s response: %s", name, resp.Body.String())
	return decodeRPCResponse(t, resp)
}

// rpcMethodCounts counts the JSON-RPC methods an upstream received.
func rpcMethodCounts(u *recordingUpstream) map[string]int {
	methods := map[string]int{}
	for _, req := range u.journal() {
		methods[req.rpcMethod]++
	}
	return methods
}

// A gateway that holds no schema for the tool lists it before the first call,
// so the member never refuses it.
func TestServePublic_MetaEndpoint_ExecuteTool_MirrorsParameterHeaders(t *testing.T) {
	t.Parallel()

	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream())
	gateway := newParameterHeaderGateway(t, upstream.url)

	gateway.execute(t)
	methods := rpcMethodCounts(upstream)
	require.Equal(t, 1, methods["tools/call"], "the member must not refuse the call")
	require.Equal(t, 1, methods["tools/list"], "the gateway reads the tool's definition once")

	gateway.execute(t)
	methods = rpcMethodCounts(upstream)
	require.Equal(t, 2, methods["tools/call"])
	require.Equal(t, 1, methods["tools/list"], "the next call reuses the listed schema")
	gateway.recorder.RequireNothing()
}

// describe_tools records the schema, so the call that follows sends its
// headers without listing again.
func TestServePublic_MetaEndpoint_ExecuteTool_DescribedSchemaSendsParameterHeaders(t *testing.T) {
	t.Parallel()

	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream())
	gateway := newParameterHeaderGateway(t, upstream.url)

	described := decodeToolsCall(t, gateway.ti, gateway.slug, gateway.bearer, "describe_tools", map[string]any{"tools": []string{"repo--get_file_contents"}})
	require.Contains(t, string(described["result"]), "x-mcp-header")
	before := rpcMethodCounts(upstream)

	gateway.execute(t)
	after := rpcMethodCounts(upstream)
	require.Equal(t, 1, after["tools/call"]-before["tools/call"])
	require.Equal(t, 0, after["tools/list"]-before["tools/list"], "the described schema stands in for a listing")
	gateway.recorder.RequireNothing()
}

// A schema that gained annotations after the gateway recorded it makes the
// member refuse the first call; the gateway lists again and retries once.
func TestServePublic_MetaEndpoint_ExecuteTool_StaleSchemaRetriesWithParameterHeaders(t *testing.T) {
	t.Parallel()

	unannotated := newRecordingUpstream(t, "get_file_contents", withStatelessUpstream())
	annotated := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream())
	var target atomic.Pointer[httputil.ReverseProxy]
	forward := func(raw string) *httputil.ReverseProxy {
		parsed, err := url.Parse(raw)
		require.NoError(t, err)
		return httputil.NewSingleHostReverseProxy(parsed)
	}
	target.Store(forward(unannotated.url))
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { target.Load().ServeHTTP(w, r) }))
	t.Cleanup(front.Close)
	gateway := newParameterHeaderGateway(t, front.URL)

	decodeToolsCall(t, gateway.ti, gateway.slug, gateway.bearer, "describe_tools", map[string]any{"tools": []string{"repo--get_file_contents"}})
	target.Store(forward(annotated.url))

	gateway.execute(t)
	methods := rpcMethodCounts(annotated)
	require.Equal(t, 2, methods["tools/call"], "the refused call is retried once")
	require.Equal(t, 1, methods["tools/list"], "the retry reads the tool's current definition")
	gateway.recorder.RequireNothing()
	requireTelemetryRowCount(t, `meta_mcp_server_id = ? AND event_source = 'tool_call' AND tool_name = 'get_file_contents'`, 2, gateway.metaID.String())

	gateway.execute(t)
	methods = rpcMethodCounts(annotated)
	require.Equal(t, 3, methods["tools/call"], "the refreshed schema is sent on the first attempt")
	require.Equal(t, 1, methods["tools/list"])
}

// A cached-catalog connection sends headers from the schema it was approved with.
func TestConnectionCatalogCachedSchemaSendsParameterHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "catalog-param-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream())
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Repo", "repo", 1, upstream.url)
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive, nil)
	// Approval writes its own connected row; only the call's rows matter here.
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	before := rpcMethodCounts(upstream)

	text, isError := metaToolResultText(t, decodeToolsCall(t, ti, slug, connection.token, "execute_tool", map[string]any{
		"name": "repo--get_file_contents", "arguments": json.RawMessage(parameterHeaderArguments),
	}))
	require.False(t, isError, text)
	after := rpcMethodCounts(upstream)
	require.Equal(t, 1, after["tools/call"]-before["tools/call"])
	require.Equal(t, 0, after["tools/list"]-before["tools/list"])
	rec.RequireNothing()
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
	methods := rpcMethodCounts(upstream)
	require.Equal(t, 2, methods["tools/call"], "the member must not refuse a call")
	require.Equal(t, 1, methods["tools/list"])
}

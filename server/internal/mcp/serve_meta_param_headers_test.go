package mcp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
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

// newAnsweringFront forwards to upstreamURL, except the JSON-RPC methods
// answer handles itself, and counts each method it receives.
func newAnsweringFront(t *testing.T, upstreamURL string, answer func(w http.ResponseWriter, method string, id json.RawMessage) bool) (string, *methodCounter) {
	t.Helper()
	parsed, err := url.Parse(upstreamURL)
	require.NoError(t, err)
	forward := httputil.NewSingleHostReverseProxy(parsed)
	counts := &methodCounter{mu: sync.Mutex{}, counts: map[string]int{}}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var rpc struct {
				Method string          `json:"method"`
				ID     json.RawMessage `json:"id"`
			}
			_ = json.Unmarshal(body, &rpc)
			counts.add(rpc.Method)
			if answer(w, rpc.Method, rpc.ID) {
				return
			}
		}
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	return front.URL, counts
}

// methodCounter counts JSON-RPC methods across handler goroutines.
type methodCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *methodCounter) add(method string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[method]++
}

func frontCount(counts *methodCounter, method string) int {
	counts.mu.Lock()
	defer counts.mu.Unlock()
	return counts.counts[method]
}

// A schema with no annotations is never checked against the header budgets,
// however large it is.
func TestServePublic_MetaEndpoint_ExecuteTool_LargeSchemaWithoutAnnotationsCalls(t *testing.T) {
	t.Parallel()

	properties := map[string]any{}
	for i := range 5000 {
		properties[fmt.Sprintf("field_%04d", i)] = map[string]any{"type": "string"}
	}
	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(map[string]any{"type": "object", "properties": properties}), withStatelessUpstream())
	gateway := newParameterHeaderGateway(t, upstream.url)

	gateway.execute(t)
	require.Equal(t, 1, rpcMethodCounts(upstream)["tools/call"])
	for _, req := range upstream.journal() {
		if req.rpcMethod == "tools/call" {
			require.Empty(t, req.paramHeaders, "an unannotated tool gets no parameter headers")
		}
	}
	gateway.recorder.RequireNothing()
}

// A failed listing does not fail the call; it is sent without headers.
func TestServePublic_MetaEndpoint_ExecuteTool_FailedListingStillCalls(t *testing.T) {
	t.Parallel()

	upstream := newRecordingUpstream(t, "get_file_contents", withStatelessUpstream())
	front, counts := newAnsweringFront(t, upstream.url, func(w http.ResponseWriter, method string, _ json.RawMessage) bool {
		if method != "tools/list" {
			return false
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	})
	gateway := newParameterHeaderGateway(t, front)

	gateway.execute(t)
	require.Equal(t, 1, frontCount(counts, "tools/list"))
	require.Equal(t, 1, rpcMethodCounts(upstream)["tools/call"])
}

// A -32020 relayed in a successful HTTP response may follow a run, so it is
// never retried.
func TestServePublic_MetaEndpoint_ExecuteTool_HeaderMismatchAfterDispatchNotRetried(t *testing.T) {
	t.Parallel()

	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream())
	front, counts := newAnsweringFront(t, upstream.url, func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/call" {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32020, "message": "header mismatch"}}))
		return true
	})
	gateway := newParameterHeaderGateway(t, front)

	_, isError := metaToolResultText(t, decodeToolsCall(t, gateway.ti, gateway.slug, gateway.bearer, "execute_tool", map[string]any{
		"name": "repo--get_file_contents", "arguments": json.RawMessage(parameterHeaderArguments),
	}))
	require.True(t, isError)
	require.Equal(t, 1, frontCount(counts, "tools/call"), "the call must not run twice")
}

// A listing without a positive ttlMs is stale at once, so each call lists
// again rather than reuse it, and is never refused.
func TestServePublic_MetaEndpoint_ExecuteTool_UncacheableListingListsEachCall(t *testing.T) {
	t.Parallel()

	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream(), withListCaching(0, ""))
	gateway := newParameterHeaderGateway(t, upstream.url)

	gateway.execute(t)
	gateway.execute(t)
	methods := rpcMethodCounts(upstream)
	require.Equal(t, 2, methods["tools/call"], "the member must not refuse a call")
	require.Equal(t, 2, methods["tools/list"])
}

// A private listing is reused for the caller that listed it.
func TestServePublic_MetaEndpoint_ExecuteTool_PrivateListingReusedByItsCaller(t *testing.T) {
	t.Parallel()

	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream(), withListCaching(time.Hour, "private"))
	gateway := newParameterHeaderGateway(t, upstream.url)

	gateway.execute(t)
	gateway.execute(t)
	methods := rpcMethodCounts(upstream)
	require.Equal(t, 2, methods["tools/call"])
	require.Equal(t, 1, methods["tools/list"])
}

// An annotation outside a chain of properties keys makes the definition
// invalid: the gateway computes no headers and the member decides.
func TestServePublic_MetaEndpoint_ExecuteTool_InvalidAnnotationCallsWithoutHeaders(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string", "x-mcp-header": "path"}},
		},
	}
	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(schema), withStatelessUpstream())
	gateway := newParameterHeaderGateway(t, upstream.url)

	text, isError := metaToolResultText(t, decodeToolsCall(t, gateway.ti, gateway.slug, gateway.bearer, "execute_tool", map[string]any{
		"name": "repo--get_file_contents", "arguments": map[string]any{"paths": []string{"README.md"}},
	}))
	require.False(t, isError, text)
	for _, req := range upstream.journal() {
		if req.rpcMethod == "tools/call" {
			require.Empty(t, req.paramHeaders)
		}
	}
}

// A private remote member records its tool catalog from the same listing the
// headers read, so a cold call lists the member once.
func TestServePublic_MetaEndpoint_ExecuteTool_PrivateMemberColdCallListsOnce(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "meta-param-private-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, slug, issuerID)
	upstream := newRecordingUpstream(t, "get_file_contents", withInputSchema(parameterHeaderSchema()), withStatelessUpstream())
	member := createMetaMemberWithVisibility(t, ctx, ti.conn, projectID, meta.ID, "repo", upstream.url, uuid.NullUUID{}, 0, "private")
	seedMetaMemberConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, member.mcpServerID)
	subject := createTestUser(t, ctx, ti, "meta-param-private-"+uuid.NewString())
	gateway := parameterHeaderGateway{ti: ti, slug: slug, metaID: meta.ID, bearer: mintMetaIssuerBearer(t, ti, slug, issuerID, subject), recorder: nil}

	gateway.execute(t)
	methods := rpcMethodCounts(upstream)
	require.Equal(t, 1, methods["tools/call"])
	require.Equal(t, 1, methods["tools/list"], "the tool catalog and the parameter headers share one listing")

	// The member's own public ttlMs counts, not the proxy's private relabel.
	gateway.execute(t)
	methods = rpcMethodCounts(upstream)
	require.Equal(t, 2, methods["tools/call"])
	require.Equal(t, 1, methods["tools/list"], "the private-visibility member's public listing is reused")
}

// credentialedMemberCalls runs execute_tool for two callers who each hold
// their own upstream token for one member, and returns the member's journal.
func credentialedMemberCalls(t *testing.T, opts ...recordingUpstreamOption) *recordingUpstream {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "meta-param-cred-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, issuerID)
	upstream := newRecordingUpstream(t, "get_file_contents", append([]recordingUpstreamOption{withInputSchema(parameterHeaderSchema()), withStatelessUpstream()}, opts...)...)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Repo", "repo", 0, upstream.url)
	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-param-cred", "", []uuid.UUID{issuerID})
	for i := range 2 {
		subject := createTestUser(t, ctx, ti, fmt.Sprintf("meta-param-cred-%d-%s", i, uuid.NewString()))
		insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, client, subject, fmt.Sprintf("token-%d", i), upstream.url)
		gateway := parameterHeaderGateway{ti: ti, slug: slug, metaID: meta.ID, bearer: mintMetaIssuerBearer(t, ti, slug, issuerID, subject), recorder: nil}
		gateway.execute(t)
	}
	require.Equal(t, 2, rpcMethodCounts(upstream)["tools/call"])
	return upstream
}

// A member's public listing serves every caller, whatever credential each sends.
func TestServePublic_MetaEndpoint_ExecuteTool_PublicListingSharedAcrossCredentials(t *testing.T) {
	t.Parallel()

	upstream := credentialedMemberCalls(t)
	require.Equal(t, 1, rpcMethodCounts(upstream)["tools/list"])
}

// A member's private listing serves only the credential that listed it.
func TestServePublic_MetaEndpoint_ExecuteTool_PrivateListingPerCredential(t *testing.T) {
	t.Parallel()

	upstream := credentialedMemberCalls(t, withListCaching(time.Hour, "private"))
	require.Equal(t, 2, rpcMethodCounts(upstream)["tools/list"])
}

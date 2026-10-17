package mcp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	serverrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeBridgePreservesUnknownOutcomeAfterUpstreamFailure(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "write")
	target, err := url.Parse(upstream.url)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	var writes atomic.Int64
	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &request)
		if request.Method == "tools/call" {
			writes.Add(1)
			// A side effect occurred, but the proxy cannot recover its reply.
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(failure.Close)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, failure.URL)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, nil)
	result, err := host.Call(ctx, "remote--write", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Equal(t, "unknown", result.Outcome)
	require.Equal(t, "tool_call_outcome_unknown", result.Error)
	require.EqualValues(t, 1, writes.Load(), "a failed result must not replay a write")
}

func TestCodeBridgeDiscoversHostedRemoteAndTunnelMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	hosted := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted", 0, "public", "list")
	remote := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 1, remote.url)
	tunnel := &fakeTunnelGateway{t: t, agentSessionID: "code-agent", backendSessionID: "code-backend", toolNames: []string{"ping"}}
	tunnelID, _ := seedTunneledMetaMember(t, ctx, ti, *auth.ProjectID, gateway.ID, "Tunnel", "tunnel", 2, "")
	upstream := httptest.NewServer(tunnel)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, nil)
	servers, err := host.Servers(ctx, codemode.PageArgs{})
	require.NoError(t, err)
	require.Len(t, servers.Items, 3)
	page, err := host.Search(ctx, codemode.SearchArgs{})
	require.NoError(t, err)
	require.False(t, page.Incomplete, "%+v", page.FailedMembers)
	paths := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		paths = append(paths, item.Path)
	}
	require.Contains(t, paths, hosted.slug+"--list")
	require.Contains(t, paths, "remote--ping")
	require.Contains(t, paths, "tunnel--ping")
	for _, path := range []string{"remote--ping", "tunnel--ping"} {
		description, err := host.Describe(ctx, path)
		require.NoError(t, err)
		require.Contains(t, string(description.Definition), "inputSchema")
		result, err := host.Call(ctx, path, json.RawMessage(`{}`))
		require.NoError(t, err)
		require.True(t, result.OK, "%+v", result)
		require.Contains(t, string(result.Content), "pong")
	}
	// The fixture has no HTTP URL. The shared executor reports this after
	// accepting dispatch, so the bridge conservatively retains an unknown outcome.
	result, err := host.Call(ctx, hosted.slug+"--list", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Equal(t, "unknown", result.Outcome)
	require.Equal(t, "tool_call_outcome_unknown", result.Error)
	result, err = host.Call(ctx, hosted.slug+"--missing", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, "completed", result.Outcome)
	require.Equal(t, "tool_call_rejected", result.Error)
	require.Contains(t, string(result.Content), "tool not found")
}

func TestCodeBridgeObservesEndpointAndGrantRevocation(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"endpoint", "issuer", "grant"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
			upstream := newRecordingUpstream(t, "ping")
			memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, upstream.url)
			if change == "grant" {
				member, err := serverrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, serverrepo.GetMCPServerByIDAndProjectIDParams{ID: memberID, ProjectID: *auth.ProjectID})
				require.NoError(t, err)
				_, err = serverrepo.New(ti.conn).UpdateMCPServer(ctx, serverrepo.UpdateMCPServerParams{ID: member.ID, ProjectID: member.ProjectID, Name: member.Name, Slug: member.Slug, EnvironmentID: member.EnvironmentID, UserSessionIssuerID: member.UserSessionIssuerID, RemoteMcpServerID: member.RemoteMcpServerID, TunneledMcpServerID: member.TunneledMcpServerID, ToolsetID: member.ToolsetID, UnproxiedMcpServerID: member.UnproxiedMcpServerID, ToolVariationsGroupID: member.ToolVariationsGroupID, Visibility: "private"})
				require.NoError(t, err)
			}
			host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, nil)
			// The call reads no catalog, so authorization alone gates it.
			result, err := host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
			require.NoError(t, err)
			require.True(t, result.OK, "%+v", result)
			switch change {
			case "endpoint":
				_, err = endpointrepo.New(ti.conn).SoftDeleteMCPEndpointsByMetaMCPServerID(ctx, endpointrepo.SoftDeleteMCPEndpointsByMetaMCPServerIDParams{MetaMcpServerID: gateway.ID, ProjectID: gateway.ProjectID})
			case "issuer":
				replacement := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
				_, err = metamcprepo.New(ti.conn).UpdateMetaMCPServer(ctx, metamcprepo.UpdateMetaMCPServerParams{ID: gateway.ID, ProjectID: gateway.ProjectID, OrganizationID: gateway.OrganizationID, Name: gateway.Name, UserSessionIssuerID: uuid.NullUUID{UUID: replacement, Valid: true}})
			case "grant":
				_, err = accessrepo.New(ti.conn).DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{OrganizationID: auth.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID)})
			}
			require.NoError(t, err)
			before := upstream.requestCount()
			_, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
			require.Error(t, err)
			require.Equal(t, before, upstream.requestCount())
		})
	}
}

func TestCodeBridgeStopsWhenGatewayIsDisabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, upstream.url)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, nil)
	_, err := host.Describe(ctx, "remote--ping")
	require.NoError(t, err)
	_, err = metamcprepo.New(ti.conn).UpdateMetaMCPServer(ctx, metamcprepo.UpdateMetaMCPServerParams{ID: gateway.ID, ProjectID: gateway.ProjectID, OrganizationID: gateway.OrganizationID, Name: gateway.Name, UserSessionIssuerID: gateway.UserSessionIssuerID, Visibility: pgtype.Text{String: "disabled", Valid: true}})
	require.NoError(t, err)
	before := upstream.requestCount()
	_, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
	require.ErrorIs(t, err, codemode.ErrExecutionRevoked)
	require.Equal(t, before, upstream.requestCount())
	_, err = metamcprepo.New(ti.conn).UpdateMetaMCPServer(ctx, metamcprepo.UpdateMetaMCPServerParams{ID: gateway.ID, ProjectID: gateway.ProjectID, OrganizationID: gateway.OrganizationID, Name: gateway.Name, UserSessionIssuerID: gateway.UserSessionIssuerID, Visibility: pgtype.Text{String: "private", Valid: true}})
	require.NoError(t, err)
	_, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
	require.ErrorIs(t, err, codemode.ErrExecutionRevoked, "re-enabling the gateway cannot resume this execution")
}

func TestCodeBridgeHonorsToolSelection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Approved", "approved", 0, upstream.url)
	selection, err := toolfilter.NewSessionSelection("meta_mcp_server:"+gateway.ID.String(), uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: "approved--ping", Mode: nil, Tools: nil},
	})
	require.NoError(t, err)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, selection)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "New", "new", 1, upstream.url)
	page, err := host.Search(ctx, codemode.SearchArgs{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "approved--ping", page.Items[0].Path)
	_, err = host.Call(ctx, "new--ping", json.RawMessage(`{}`))
	require.Error(t, err, "a callback cannot reach a tool outside the selection")
}

func TestCodeBridgeOmitsMembersWithNoPermittedTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Approved", "approved", 0, upstream.url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Other", "other", 1, upstream.url)
	selection, err := toolfilter.NewSessionSelection("meta_mcp_server:"+gateway.ID.String(), uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: "approved--ping", Mode: nil, Tools: nil},
	})
	require.NoError(t, err)

	servers, _, err := mcp.NewTestMetaCodeBackend(t, ctx, ti.service, gateway.ID, selection).Members(ctx)
	require.NoError(t, err)
	require.Equal(t, []codemode.Server{{Slug: "approved", Name: "Approved"}}, servers)

	servers, _, err = mcp.NewTestMetaCodeBackend(t, ctx, ti.service, gateway.ID, nil).Members(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 2, "an unrestricted session keeps every member")
}

// TestCodeBridgeCallsWithoutListingAndDropsSwappedDestination pins the call
// path: a call dispatches without reading the member's catalog, the member's
// own refusal reaches the program as a completed failure, and a member now
// served by a different backend is dropped before any call.
func TestCodeBridgeCallsWithoutListingAndDropsSwappedDestination(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	original := newRecordingUpstream(t, "ping")
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, original.url)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, nil)
	count := func(u *recordingUpstream, method string) int {
		n := 0
		for _, req := range u.journal() {
			if req.rpcMethod == method {
				n++
			}
		}
		return n
	}

	for range 2 {
		result, err := host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
		require.NoError(t, err)
		require.True(t, result.OK, "%+v", result)
	}
	require.Equal(t, 2, count(original, "tools/call"))
	require.Zero(t, count(original, "tools/list"), "a call never reads the member catalog")

	result, err := host.Call(ctx, "remote--missing", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, "completed", result.Outcome)
	require.False(t, result.OK)
	require.Equal(t, "tool_call_rejected", result.Error)
	require.Contains(t, string(result.Content), "missing")

	replacement := newRecordingUpstream(t, "ping")
	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{ProjectID: *auth.ProjectID, TransportType: "streamable-http", Url: replacement.url})
	row, err := serverrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, serverrepo.GetMCPServerByIDAndProjectIDParams{ID: memberID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	_, err = serverrepo.New(ti.conn).UpdateMCPServer(ctx, serverrepo.UpdateMCPServerParams{Name: row.Name, Slug: row.Slug, EnvironmentID: row.EnvironmentID, UserSessionIssuerID: row.UserSessionIssuerID, RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, TunneledMcpServerID: row.TunneledMcpServerID, ToolsetID: row.ToolsetID, UnproxiedMcpServerID: row.UnproxiedMcpServerID, ToolVariationsGroupID: row.ToolVariationsGroupID, Visibility: row.Visibility, ID: row.ID, ProjectID: row.ProjectID})
	require.NoError(t, err)

	calls := count(original, "tools/call")
	_, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
	require.Error(t, err)
	require.Equal(t, calls, count(original, "tools/call"))
	require.Empty(t, replacement.journal(), "a swapped backend must not be dialed")
}

// TestCodeBridgeLiveAnnotationGrantWithoutDescribe: a call that skipped
// discovery is still held to a live annotation grant, which lists on the
// call's own session; the refused tool never runs.
func TestCodeBridgeLiveAnnotationGrantWithoutDescribe(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, []testmcp.Tool{
		{Name: "read_docs", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}, InputSchema: map[string]any{"type": "object"}, Response: testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "read ok"}}}},
		{Name: "delete_docs", InputSchema: map[string]any{"type": "object"}, Response: testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "delete ok"}}}},
	})
	t.Cleanup(upstream.Close)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Docs", "docs", 0, upstream.URL)
	live := toolfilter.AnnotationModeLive
	selection, err := toolfilter.NewSessionSelection("meta_mcp_server:"+gateway.ID.String(), uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeAnnotation, Name: toolfilter.AnnotationReadOnly, Mode: &live, Tools: nil},
	})
	require.NoError(t, err)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, selection)

	result, err := host.Call(ctx, "docs--read_docs", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result)
	require.Contains(t, string(result.Content), "read ok")

	_, err = host.Call(ctx, "docs--delete_docs", json.RawMessage(`{}`))
	var refusal *codemode.DispatchError
	require.ErrorAs(t, err, &refusal)
	require.True(t, refusal.BeforeInvoke, "the gateway refused the call before the member saw it")
}

// TestCodeBridgeSharesMemberSessionWithinExecution: one execution pays a
// member's handshake once across describe and calls, including concurrent
// ones, and closes the session when it ends.
func TestCodeBridgeSharesMemberSessionWithinExecution(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, upstream.url)
	backend := mcp.NewTestMetaCodeBackend(t, ctx, ti.service, gateway.ID, nil)
	host, err := codemode.NewHost(ctx, backend, uuid.NewString(), nil)
	require.NoError(t, err)
	count := func(method string) int {
		n := 0
		for _, req := range upstream.journal() {
			if req.rpcMethod == method || req.httpMethod == method {
				n++
			}
		}
		return n
	}

	_, err = host.Describe(ctx, "remote--ping")
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make([]*codemode.ToolResult, 6)
	for i := range results {
		wg.Go(func() {
			result, err := host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
			assert.NoError(t, err)
			results[i] = result
		})
	}
	wg.Wait()
	for _, result := range results {
		require.NotNil(t, result)
		require.True(t, result.OK, "%+v", result)
	}
	require.Equal(t, 6, count("tools/call"))
	require.Equal(t, 1, count("tools/list"))
	require.Equal(t, 1, count("initialize"), "describe and every call share one handshake")

	// The member's own refusal leaves the shared session usable.
	result, err := host.Call(ctx, "remote--missing", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Equal(t, "tool_call_rejected", result.Error)
	result, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result)
	require.Equal(t, 1, count("initialize"))

	mcp.CloseTestMetaCodeSessions(ctx, backend)
	require.Eventually(t, func() bool { return count(http.MethodDelete) == 1 }, 5*time.Second, 10*time.Millisecond, "the execution's end closes its session")

	// A straggler after the execution ended uses, and closes, a session of its own.
	result, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result)
	require.Equal(t, 2, count("initialize"))
	require.Equal(t, 2, count(http.MethodDelete))
}

// TestCodeBridgeRetiresSharedSessionAfterFailedExchange: a failed status
// retires the shared session, even when it carries a JSON-RPC error body, and
// a sibling call already in flight on it still completes. Nothing is replayed.
func TestCodeBridgeRetiresSharedSessionAfterFailedExchange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "write")
	target, err := url.Parse(upstream.url)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	var calls, initializes atomic.Int64
	slowStarted, failed := make(chan struct{}), make(chan struct{})
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Arguments struct {
					Mode string `json:"mode"`
				} `json:"arguments"`
			} `json:"params"`
		}
		_ = json.Unmarshal(body, &request)
		switch request.Method {
		case "initialize":
			initializes.Add(1)
		case "tools/call":
			calls.Add(1)
			switch request.Params.Arguments.Mode {
			case "slow":
				close(slowStarted)
				<-failed
			case "fail":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"boom"}}`, request.ID)
				close(failed)
				return
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(flaky.Close)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, flaky.URL)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, nil)

	result, err := host.Call(ctx, "remote--write", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result)

	slow := make(chan *codemode.ToolResult, 1)
	go func() {
		result, err := host.Call(ctx, "remote--write", json.RawMessage(`{"mode":"slow"}`))
		assert.NoError(t, err)
		slow <- result
	}()
	<-slowStarted
	result, err = host.Call(ctx, "remote--write", json.RawMessage(`{"mode":"fail"}`))
	require.NoError(t, err)
	require.False(t, result.OK)
	sibling := <-slow
	require.NotNil(t, sibling)
	require.True(t, sibling.OK, "a sibling in flight outlives the retired session: %+v", sibling)
	require.EqualValues(t, 1, initializes.Load())

	result, err = host.Call(ctx, "remote--write", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result)
	require.EqualValues(t, 4, calls.Load(), "nothing is replayed")
	require.EqualValues(t, 2, initializes.Load(), "the failed session is not reused")
}

// TestCodeBridgeSharedSessionFollowsEditedUpstreamURL: editing a member's
// upstream URL in place never sends the old session to the new upstream.
func TestCodeBridgeSharedSessionFollowsEditedUpstreamURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	before, after := newRecordingUpstream(t, "ping"), newRecordingUpstream(t, "ping")
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, before.url)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, nil)

	result, err := host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result)

	row, err := serverrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, serverrepo.GetMCPServerByIDAndProjectIDParams{ID: memberID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	_, err = remotemcprepo.New(ti.conn).UpdateServer(ctx, remotemcprepo.UpdateServerParams{Name: pgtype.Text{String: "", Valid: false}, Slug: pgtype.Text{String: "", Valid: false}, TransportType: "streamable-http", Url: after.url, ID: row.RemoteMcpServerID.UUID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)

	result, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result)
	methods := []string{}
	for _, req := range after.journal() {
		if req.rpcMethod != "" {
			methods = append(methods, req.rpcMethod)
		}
	}
	require.Contains(t, methods, "initialize", "the edited upstream gets a session of its own")
	require.Contains(t, methods, "tools/call")
}

// TestCodeBridgeLiveGrantListingsAndCallsOverlap: describes and calls that a
// live grant authorizes share one session concurrently without refusing an
// allowed call.
func TestCodeBridgeLiveGrantListingsAndCallsOverlap(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, []testmcp.Tool{
		{Name: "read_docs", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}, InputSchema: map[string]any{"type": "object"}, Response: testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "read ok"}}}},
	})
	t.Cleanup(upstream.Close)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Docs", "docs", 0, upstream.URL)
	live := toolfilter.AnnotationModeLive
	selection, err := toolfilter.NewSessionSelection("meta_mcp_server:"+gateway.ID.String(), uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeAnnotation, Name: toolfilter.AnnotationReadOnly, Mode: &live, Tools: nil},
	})
	require.NoError(t, err)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, selection)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if i%2 == 0 {
				_, err := host.Describe(ctx, "docs--read_docs")
				assert.NoError(t, err)
				return
			}
			result, err := host.Call(ctx, "docs--read_docs", json.RawMessage(`{}`))
			if assert.NoError(t, err) {
				assert.True(t, result.OK, "%+v", result)
			}
		})
	}
	wg.Wait()
}

// TestCodeBridgeSharesMemberSessionAcrossExecutions: a verified caller's
// successive executions reuse a member session; another credential never
// does, and an idle session expires and closes.
func TestCodeBridgeSharesMemberSessionAcrossExecutions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	mcp.SetTestCodeSessionPool(ti.service, 3*time.Second, time.Minute, 16)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, upstream.url)
	count := func(method string) int {
		n := 0
		for _, req := range upstream.journal() {
			if req.rpcMethod == method || req.httpMethod == method {
				n++
			}
		}
		return n
	}
	execute := func(credential string) {
		t.Helper()
		backend := mcp.NewTestSharedMetaCodeBackend(t, ctx, ti.service, gateway.ID, credential)
		host, err := codemode.NewHost(ctx, backend, uuid.NewString(), nil)
		require.NoError(t, err)
		result, err := host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
		require.NoError(t, err)
		require.True(t, result.OK, "%+v", result)
		mcp.CloseTestMetaCodeSessions(ctx, backend)
	}

	execute("caller-a")
	execute("caller-a")
	require.Equal(t, 2, count("tools/call"))
	require.Equal(t, 1, count("initialize"), "the caller's next execution reuses its session")
	require.Zero(t, count(http.MethodDelete), "an execution's end leaves a shared session open")

	execute("caller-b")
	require.Equal(t, 2, count("initialize"), "another credential never shares a session")

	require.Eventually(t, func() bool { return count(http.MethodDelete) == 2 }, 15*time.Second, 50*time.Millisecond, "idle sessions expire and close")
	execute("caller-a")
	require.Equal(t, 3, count("initialize"))
}

// TestCodeBridgePingsIdleSessionBeforeReuse: a session idle past the ping
// threshold is pinged before reuse, so one the member expired is replaced
// before any call; a 404 on tools/call itself is never replayed.
func TestCodeBridgePingsIdleSessionBeforeReuse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "ping")
	target, err := url.Parse(upstream.url)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	var mu sync.Mutex
	forgotten, seen := map[string]bool{}, map[string]bool{}
	var pings, initializes atomic.Int64
	forgetful := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &request)
		switch request.Method {
		case "initialize":
			initializes.Add(1)
		case "ping":
			pings.Add(1)
		}
		mu.Lock()
		id := r.Header.Get("Mcp-Session-Id")
		seen[id] = id != ""
		gone := forgotten[id]
		mu.Unlock()
		if gone {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(forgetful.Close)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, forgetful.URL)
	served := func() int {
		n := 0
		for _, req := range upstream.journal() {
			if req.rpcMethod == "tools/call" {
				n++
			}
		}
		return n
	}
	execute := func() *codemode.ToolResult {
		t.Helper()
		backend := mcp.NewTestSharedMetaCodeBackend(t, ctx, ti.service, gateway.ID, "caller")
		host, err := codemode.NewHost(ctx, backend, uuid.NewString(), nil)
		require.NoError(t, err)
		result, err := host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
		require.NoError(t, err)
		return result
	}
	forgetAll := func() {
		mu.Lock()
		defer mu.Unlock()
		maps.Copy(forgotten, seen)
	}

	// Within the ping threshold a forgotten session's 404 on tools/call is
	// an unknown outcome, never replayed.
	mcp.SetTestCodeSessionPool(ti.service, time.Minute, time.Hour, 16)
	require.True(t, execute().OK)
	forgetAll()
	result := execute()
	require.Equal(t, "unknown", result.Outcome)
	require.Equal(t, 1, served())

	// Past it, the reuse pings first and connects again before calling.
	mcp.SetTestCodeSessionPool(ti.service, time.Minute, time.Nanosecond, 16)
	require.True(t, execute().OK)
	forgetAll()
	pingsBefore, initializesBefore := pings.Load(), initializes.Load()
	result = execute()
	require.True(t, result.OK, "%+v", result)
	require.Greater(t, pings.Load(), pingsBefore)
	require.Equal(t, initializesBefore+1, initializes.Load())
	require.Equal(t, 3, served(), "every call ran exactly once")

	// Concurrent borrowers of an expired session all wait for the one ping.
	forgetAll()
	initializesBefore = initializes.Load()
	backend := mcp.NewTestSharedMetaCodeBackend(t, ctx, ti.service, gateway.ID, "caller")
	host, err := codemode.NewHost(ctx, backend, uuid.NewString(), nil)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			result, err := host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
			if assert.NoError(t, err) {
				assert.True(t, result.OK, "%+v", result)
			}
		})
	}
	wg.Wait()
	require.Equal(t, initializesBefore+1, initializes.Load())
	require.Equal(t, 7, served())
}

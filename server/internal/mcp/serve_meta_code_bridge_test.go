package mcp_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	serverrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
			_, err := host.Describe(ctx, "remote--ping")
			require.NoError(t, err)
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

// TestCodeBridgeRefusesChangedDefinitionOrDestination pins the fingerprint
// check at dispatch: a definition that changed since the catalog read, or a
// member now served by a different backend, is refused before any call.
func TestCodeBridgeRefusesChangedDefinitionOrDestination(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	original := newRecordingUpstream(t, "ping")
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, original.url)
	backend := mcp.NewTestMetaCodeBackend(t, ctx, ti.service, gateway.ID, nil)
	toolCalls := func(u *recordingUpstream) int {
		count := 0
		for _, req := range u.journal() {
			if req.rpcMethod == "tools/call" {
				count++
			}
		}
		return count
	}

	catalog, err := backend.List(ctx, "remote")
	require.NoError(t, err)
	require.Len(t, catalog.Tools, 1)
	pinned := catalog.Tools[0].Fingerprint

	_, err = backend.Invoke(ctx, "remote--ping", pinned+"0", json.RawMessage(`{}`))
	dispatch, ok := errors.AsType[*codemode.DispatchError](err)
	require.True(t, ok, "%v", err)
	require.True(t, dispatch.BeforeInvoke)
	require.Equal(t, "tool_changed", dispatch.Code)
	require.Zero(t, toolCalls(original), "a changed definition must not dispatch")

	replacement := newRecordingUpstream(t, "ping")
	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{ProjectID: *auth.ProjectID, TransportType: "streamable-http", Url: replacement.url})
	row, err := serverrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, serverrepo.GetMCPServerByIDAndProjectIDParams{ID: memberID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	_, err = serverrepo.New(ti.conn).UpdateMCPServer(ctx, serverrepo.UpdateMCPServerParams{Name: row.Name, Slug: row.Slug, EnvironmentID: row.EnvironmentID, UserSessionIssuerID: row.UserSessionIssuerID, RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, TunneledMcpServerID: row.TunneledMcpServerID, ToolsetID: row.ToolsetID, UnproxiedMcpServerID: row.UnproxiedMcpServerID, ToolVariationsGroupID: row.ToolVariationsGroupID, Visibility: row.Visibility, ID: row.ID, ProjectID: row.ProjectID})
	require.NoError(t, err)

	_, err = backend.Invoke(ctx, "remote--ping", pinned, json.RawMessage(`{}`))
	dispatch, ok = errors.AsType[*codemode.DispatchError](err)
	require.True(t, ok, "%v", err)
	require.Equal(t, "tool_changed", dispatch.Code)
	require.Zero(t, toolCalls(original))
	require.Zero(t, toolCalls(replacement), "a swapped backend must not receive the call")
}

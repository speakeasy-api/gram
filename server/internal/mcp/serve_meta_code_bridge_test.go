package mcp_test

import (
	"bytes"
	"encoding/json"
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
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	serverrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
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
			before := upstream.requests.Load()
			_, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
			require.Error(t, err)
			require.Equal(t, before, upstream.requests.Load())
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
	before := upstream.requests.Load()
	_, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
	require.ErrorIs(t, err, codemode.ErrExecutionRevoked)
	require.Equal(t, before, upstream.requests.Load())
	_, err = metamcprepo.New(ti.conn).UpdateMetaMCPServer(ctx, metamcprepo.UpdateMetaMCPServerParams{ID: gateway.ID, ProjectID: gateway.ProjectID, OrganizationID: gateway.OrganizationID, Name: gateway.Name, UserSessionIssuerID: gateway.UserSessionIssuerID, Visibility: pgtype.Text{String: "private", Valid: true}})
	require.NoError(t, err)
	_, err = host.Call(ctx, "remote--ping", json.RawMessage(`{}`))
	require.ErrorIs(t, err, codemode.ErrExecutionRevoked, "re-enabling the gateway cannot resume this execution")
}

func TestCodeBridgeFrozenInventoryExcludesNewMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "code-"+uuid.NewString(), issuer)
	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Approved", "approved", 0, upstream.url)
	approved, err := ti.service.GatewayInventory(ctx, gateway.ID, *auth.ProjectID, urn.NewUserSubject(auth.UserID))
	require.NoError(t, err)
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service, gateway.ID, approved)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "New", "new", 1, upstream.url)
	page, err := host.Search(ctx, codemode.SearchArgs{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "approved--ping", page.Items[0].Path)
	_, err = host.Call(ctx, "new--ping", json.RawMessage(`{}`))
	require.ErrorIs(t, err, codemode.ErrToolUnavailable)
}

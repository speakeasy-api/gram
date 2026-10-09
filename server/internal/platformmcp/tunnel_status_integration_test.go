package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/tunnel/route"
)

// tunnelStatusSentinel is stored in every secret-adjacent tunnel field so a
// test can prove none of it reaches a Platform MCP result.
const tunnelStatusSentinel = "tunnel-status-sentinel-must-not-leak"

type recordingTunnelConnections struct {
	calls       atomic.Int32
	connections []route.Connection
	err         error
}

func (r *recordingTunnelConnections) Connections(_ context.Context, _ string) ([]route.Connection, error) {
	r.calls.Add(1)
	return r.connections, r.err
}

type tunnelStatusFixture struct {
	conn      *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	tunnelID  uuid.UUID
	wrapperID uuid.UUID
	otherMCP  uuid.UUID
}

func seedTunnelStatusFixture(t *testing.T, name string) tunnelStatusFixture {
	t.Helper()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlatformMCPAuthorizationMember(t, ctx, conn, principal.OrganizationID, principal.UserID, authz.SystemRoleMember)

	tunnel, err := tunneledmcprepo.New(conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          project.ID,
		Name:               "Private inventory tunnel",
		KeyHash:            tunnelStatusSentinel + "-hash",
		KeyPrefix:          tunnelStatusSentinel + "-prefix",
		ResourceIdentifier: pgtype.Text{String: "https://" + tunnelStatusSentinel + ".internal", Valid: true},
	})
	require.NoError(t, err)
	wrapper, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           project.ID,
		Name:                pgtype.Text{String: "Private inventory", Valid: true},
		Slug:                pgtype.Text{String: "private-inventory", Valid: true},
		TunneledMcpServerID: uuid.NullUUID{UUID: tunnel.ID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)

	candidates, err := platformrepo.New(conn).ListPlatformMCPInventoryAuthorizationCandidates(ctx, principal.OrganizationID)
	require.NoError(t, err)
	var otherMCP uuid.UUID
	for _, candidate := range candidates {
		if candidate.ProjectID == project.ID && candidate.ID != wrapper.ID {
			otherMCP = candidate.ID
		}
	}
	require.NotEqual(t, uuid.Nil, otherMCP)

	return tunnelStatusFixture{conn: conn, principal: principal, project: project, tunnelID: tunnel.ID, wrapperID: wrapper.ID, otherMCP: otherMCP}
}

func (f tunnelStatusFixture) grant(t *testing.T, scope authz.Scope, resourceID string, projectDimension bool) {
	t.Helper()
	selector := authz.NewSelector(scope, resourceID)
	if projectDimension {
		selector[authz.SelectorKeyProjectID] = f.project.ID.String()
	}
	encoded, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: f.principal.OrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, f.principal.UserID),
		Scope:          string(scope),
		Selectors:      encoded,
	})
	require.NoError(t, err)
}

// grantProjectSourceRead grants the project-level mcp:read that the dashboard's
// tunnel detail requires, which also covers every MCP server in the project.
func (f tunnelStatusFixture) grantProjectSourceRead(t *testing.T) {
	t.Helper()
	f.grant(t, authz.ScopeProjectRead, f.project.ID.String(), false)
	f.grant(t, authz.ScopeMCPRead, "*", true)
}

func (f tunnelStatusFixture) reader(t *testing.T, connections TunnelConnectionReader) (*PostgresReader, context.Context) {
	t.Helper()
	engine := authz.NewEngine(testenv.NewLogger(t), f.conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	prepared, err := NewLiveOrgAdminAuthorizer(f.conn, engine).PrepareExternalContext(t.Context(), f.principal)
	require.NoError(t, err)
	reader := NewPostgresReader(testenv.NewLogger(t), f.conn).WithAuthorization(engine).WithTunnelStatus(connections)
	reader.setInventoryCursorKey("tunnel-status-key")
	return reader, prepared
}

func (f tunnelStatusFixture) getMCP(t *testing.T, reader *PostgresReader, ctx context.Context, mcpID uuid.UUID) MCP {
	t.Helper()
	mcp, err := reader.GetMCP(ctx, f.principal, GetMCPInput{ProjectID: f.project.ID.String(), MCPID: mcpID.String()})
	require.NoError(t, err)
	return mcp
}

func TestTunnelStatusReportsConnectedForProjectSourceReader(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_connected")
	fixture.grantProjectSourceRead(t)
	connections := &recordingTunnelConnections{connections: []route.Connection{{GatewaySessionID: "session", RemoteAddr: "203.0.113.7:4040", AgentVersion: tunnelStatusSentinel}}}
	reader, ctx := fixture.reader(t, connections)

	mcp := fixture.getMCP(t, reader, ctx, fixture.wrapperID)

	require.Equal(t, MCPBackendTunneled, mcp.BackendKind)
	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionConnected}, mcp.Tunnel)
	require.Equal(t, int32(1), connections.calls.Load())
	encoded, err := json.Marshal(mcp)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), tunnelStatusSentinel)
	require.NotContains(t, string(encoded), "203.0.113.7")
	require.Contains(t, string(encoded), `"tunnel":{"connection_status":"connected"}`)
}

func TestTunnelStatusReportsNeverConnectedWithoutLiveConnections(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_never_connected")
	fixture.grantProjectSourceRead(t)
	reader, ctx := fixture.reader(t, &recordingTunnelConnections{})

	mcp := fixture.getMCP(t, reader, ctx, fixture.wrapperID)

	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionNeverConnected}, mcp.Tunnel)
}

func TestTunnelStatusReportsUnknownWhenRuntimeFails(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_runtime_error")
	fixture.grantProjectSourceRead(t)
	connections := &recordingTunnelConnections{err: errors.New("redis unavailable: " + tunnelStatusSentinel)}
	reader, ctx := fixture.reader(t, connections)

	mcp := fixture.getMCP(t, reader, ctx, fixture.wrapperID)

	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionUnknown}, mcp.Tunnel)
	encoded, err := json.Marshal(mcp)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), tunnelStatusSentinel)
}

func TestTunnelStatusReportsUnknownWithoutRuntimeStore(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_no_runtime")
	fixture.grantProjectSourceRead(t)
	reader, ctx := fixture.reader(t, nil)

	mcp := fixture.getMCP(t, reader, ctx, fixture.wrapperID)

	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionUnknown}, mcp.Tunnel)
}

func TestTunnelStatusOmittedForWrapperOnlyReader(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_wrapper_only")
	fixture.grant(t, authz.ScopeProjectRead, fixture.project.ID.String(), false)
	fixture.grant(t, authz.ScopeMCPRead, fixture.wrapperID.String(), true)
	connections := &recordingTunnelConnections{}
	reader, ctx := fixture.reader(t, connections)

	mcp := fixture.getMCP(t, reader, ctx, fixture.wrapperID)

	require.Equal(t, MCPBackendTunneled, mcp.BackendKind)
	require.Nil(t, mcp.Tunnel)
	require.Zero(t, connections.calls.Load(), "an unauthorized enrichment must not read the runtime store")
}

func TestTunnelStatusOmittedForNonTunneledServer(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_not_tunneled")
	fixture.grantProjectSourceRead(t)
	connections := &recordingTunnelConnections{}
	reader, ctx := fixture.reader(t, connections)

	mcp := fixture.getMCP(t, reader, ctx, fixture.otherMCP)

	require.NotEqual(t, MCPBackendTunneled, mcp.BackendKind)
	require.Nil(t, mcp.Tunnel)
	require.Zero(t, connections.calls.Load())
}

func TestTunnelStatusOmittedForDeletedSource(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_deleted_source")
	fixture.grantProjectSourceRead(t)
	_, err := tunneledmcprepo.New(fixture.conn).DeleteServer(t.Context(), tunneledmcprepo.DeleteServerParams{ID: fixture.tunnelID, ProjectID: fixture.project.ID})
	require.NoError(t, err)
	connections := &recordingTunnelConnections{}
	reader, ctx := fixture.reader(t, connections)

	mcp := fixture.getMCP(t, reader, ctx, fixture.wrapperID)

	require.Nil(t, mcp.Tunnel)
	require.Zero(t, connections.calls.Load())
}

func TestTunnelStatusNeverReadByFindMCP(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_find_mcp")
	fixture.grantProjectSourceRead(t)
	connections := &recordingTunnelConnections{}
	reader, ctx := fixture.reader(t, connections)

	inventory, err := reader.FindMCP(ctx, fixture.principal, FindMCPInput{ProjectID: fixture.project.ID.String()})
	require.NoError(t, err)

	var found bool
	for _, mcp := range inventory.MCPs {
		require.Nil(t, mcp.Tunnel)
		if mcp.ID == fixture.wrapperID.String() {
			found = true
		}
	}
	require.True(t, found)
	require.Zero(t, connections.calls.Load(), "list inventory must not read the runtime store")
}

func TestTunnelStatusDiagnosticsReadRequiresProjectSourceRead(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_diagnostics_project_read")
	fixture.grant(t, authz.ScopeProjectRead, fixture.project.ID.String(), false)
	connections := &recordingTunnelConnections{}
	reader, ctx := fixture.reader(t, connections)

	mcp, err := reader.GetMCPForDiagnostics(ctx, fixture.principal, GetMCPInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.wrapperID.String()})
	require.NoError(t, err)

	require.Equal(t, MCPBackendTunneled, mcp.BackendKind)
	require.Nil(t, mcp.Tunnel)
	require.Zero(t, connections.calls.Load())
}

func TestTunnelStatusDiagnosticsReadWithProjectSourceRead(t *testing.T) {
	t.Parallel()

	fixture := seedTunnelStatusFixture(t, "platform_mcp_tunnel_status_diagnostics_source_read")
	fixture.grantProjectSourceRead(t)
	reader, ctx := fixture.reader(t, &recordingTunnelConnections{})

	mcp, err := reader.GetMCPForDiagnostics(ctx, fixture.principal, GetMCPInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.wrapperID.String()})
	require.NoError(t, err)

	require.Equal(t, &MCPTunnel{ConnectionStatus: TunnelConnectionNeverConnected}, mcp.Tunnel)
}

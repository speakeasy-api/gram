package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	tunneledgen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// newTunnelService builds the tunneled MCP source service over the same
// database, so tests can race its delete against MCP server creates.
func newTunnelService(t *testing.T, ti *testInstance) *tunneledmcp.Service {
	t.Helper()

	logger := testenv.NewLogger(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	return tunneledmcp.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, authzEngine, audit.NewLogger(), nil, redisClient)
}

func requireAuth(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return authCtx
}

func deleteTunnelPayload(id uuid.UUID) *tunneledgen.DeleteServerPayload {
	return &tunneledgen.DeleteServerPayload{ID: id.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}
}

func createOnTunnelPayload(tunnelID uuid.UUID) *gen.CreateMcpServerPayload {
	id := tunnelID.String()
	return &gen.CreateMcpServerPayload{
		Name:                "on tunnel " + uuid.NewString(),
		TunneledMcpServerID: &id,
		Visibility:          types.McpServerVisibility("disabled"),
	}
}

func repointToTunnelPayload(serverID string, tunnelID uuid.UUID) *gen.UpdateMcpServerPayload {
	id := tunnelID.String()
	return &gen.UpdateMcpServerPayload{
		ID:                  serverID,
		TunneledMcpServerID: &id,
		Visibility:          types.McpServerVisibility("disabled"),
	}
}

func liveMCPServersOnTunnel(t *testing.T, ctx context.Context, ti *testInstance, projectID, tunnelID uuid.UUID) []mcpserversrepo.McpServer {
	t.Helper()

	servers, err := mcpserversrepo.New(ti.conn).ListMCPServersByProjectID(ctx, mcpserversrepo.ListMCPServersByProjectIDParams{
		ProjectID:           projectID,
		TunneledMcpServerID: uuid.NullUUID{UUID: tunnelID, Valid: true},
	})
	require.NoError(t, err)
	return servers
}

func tunnelIsLive(t *testing.T, ctx context.Context, ti *testInstance, projectID, tunnelID uuid.UUID) bool {
	t.Helper()

	_, err := tunneledmcprepo.New(ti.conn).GetServerByID(ctx, tunneledmcprepo.GetServerByIDParams{ID: tunnelID, ProjectID: projectID})
	return err == nil
}

// A create that reaches the tunnel while its delete is in flight waits for the
// delete and is then refused, rather than landing on a deleted tunnel.
func TestCreateMcpServer_RefusedWhenTunnelDeletedWhileWaiting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuth(t, ctx)
	tunnelID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	deleteTx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := tunneledmcprepo.New(deleteTx).GetServerByIDForUpdate(ctx, tunneledmcprepo.GetServerByIDForUpdateParams{ID: tunnelID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	createErr := make(chan error, 1)
	go func() {
		_, err := ti.service.CreateMcpServer(ctx, createOnTunnelPayload(tunnelID))
		createErr <- err
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(deleteTx), "%FOR SHARE%")

	_, err = tunneledmcprepo.New(deleteTx).DeleteServer(ctx, tunneledmcprepo.DeleteServerParams{ID: tunnelID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.NoError(t, deleteTx.Commit(ctx))

	requireOopsCode(t, <-createErr, oops.CodeInvalid)
	require.Empty(t, liveMCPServersOnTunnel(t, ctx, ti, *authCtx.ProjectID, tunnelID))
}

// A tunnel delete that arrives while an MCP server create on the tunnel is
// still open waits for it, then counts it and refuses.
func TestTunnelDelete_RefusedWhenMcpServerCreatedWhileWaiting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuth(t, ctx)
	tunnels := newTunnelService(t, ti)
	tunnelID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	createTx := testenv.BeginTx(t, ctx, ti.conn)
	created, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, createTx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		ActorUserID:         authCtx.UserID,
		ActorEmail:          authCtx.Email,
		Name:                "racing create",
		Visibility:          "disabled",
		NetworkAccessMode:   networkaccess.ModePublicOnly,
		TunneledMCPServerID: uuid.NullUUID{UUID: tunnelID, Valid: true},
	})
	require.NoError(t, err)

	deleteErr := make(chan error, 1)
	go func() { deleteErr <- tunnels.DeleteServer(ctx, deleteTunnelPayload(tunnelID)) }()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(createTx), "%FOR UPDATE%")

	require.NoError(t, createTx.Commit(ctx))

	requireOopsCode(t, <-deleteErr, oops.CodeConflict)
	require.True(t, tunnelIsLive(t, ctx, ti, *authCtx.ProjectID, tunnelID))
	servers := liveMCPServersOnTunnel(t, ctx, ti, *authCtx.ProjectID, tunnelID)
	require.Len(t, servers, 1)
	require.Equal(t, created.ID, servers[0].ID)
}

// When the racing create rolls back, the waiting delete proceeds.
func TestTunnelDelete_ProceedsWhenWaitingCreateRollsBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuth(t, ctx)
	tunnels := newTunnelService(t, ti)
	tunnelID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	createTx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, createTx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		ActorUserID:         authCtx.UserID,
		ActorEmail:          authCtx.Email,
		Name:                "rolled back create",
		Visibility:          "disabled",
		NetworkAccessMode:   networkaccess.ModePublicOnly,
		TunneledMCPServerID: uuid.NullUUID{UUID: tunnelID, Valid: true},
	})
	require.NoError(t, err)

	deleteErr := make(chan error, 1)
	go func() { deleteErr <- tunnels.DeleteServer(ctx, deleteTunnelPayload(tunnelID)) }()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(createTx), "%FOR UPDATE%")

	require.NoError(t, createTx.Rollback(ctx))

	require.NoError(t, <-deleteErr)
	require.False(t, tunnelIsLive(t, ctx, ti, *authCtx.ProjectID, tunnelID))
	require.Empty(t, liveMCPServersOnTunnel(t, ctx, ti, *authCtx.ProjectID, tunnelID))
}

// A repoint onto a tunnel whose delete is in flight waits, then is refused.
func TestUpdateMcpServer_RepointRefusedWhenTunnelDeletedWhileWaiting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuth(t, ctx)
	subject := seedMcpServerForBackend(t, ctx, ti, "repoint subject", seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String())
	tunnelID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	deleteTx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := tunneledmcprepo.New(deleteTx).GetServerByIDForUpdate(ctx, tunneledmcprepo.GetServerByIDForUpdateParams{ID: tunnelID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	updateErr := make(chan error, 1)
	go func() {
		_, err := ti.service.UpdateMcpServer(ctx, repointToTunnelPayload(subject, tunnelID))
		updateErr <- err
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(deleteTx), "%FOR SHARE%")

	_, err = tunneledmcprepo.New(deleteTx).DeleteServer(ctx, tunneledmcprepo.DeleteServerParams{ID: tunnelID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.NoError(t, deleteTx.Commit(ctx))

	requireOopsCode(t, <-updateErr, oops.CodeInvalid)
	require.Empty(t, liveMCPServersOnTunnel(t, ctx, ti, *authCtx.ProjectID, tunnelID))
}

// A tunnel delete waiting on a repoint onto the tunnel counts the repointed
// server and refuses. The repoint holds the tunnel row lock through the
// production update path until the test releases it.
func TestTunnelDelete_RefusedWhenMcpServerRepointedWhileWaiting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuth(t, ctx)
	tunnels := newTunnelService(t, ti)
	subject := seedMcpServerForBackend(t, ctx, ti, "repointed server", seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String())
	tunnelID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	// A third transaction holds the tunnel row so the repoint and the delete
	// both queue behind it in a known order: repoint first, then delete.
	gateTx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := tunneledmcprepo.New(gateTx).GetServerByIDForUpdate(ctx, tunneledmcprepo.GetServerByIDForUpdateParams{ID: tunnelID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	updateErr := make(chan error, 1)
	go func() {
		_, err := ti.service.UpdateMcpServer(ctx, repointToTunnelPayload(subject, tunnelID))
		updateErr <- err
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(gateTx), "%FOR SHARE%")

	deleteErr := make(chan error, 1)
	go func() { deleteErr <- tunnels.DeleteServer(ctx, deleteTunnelPayload(tunnelID)) }()
	testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, testenv.BackendPID(gateTx), 2)

	require.NoError(t, gateTx.Rollback(ctx))

	require.NoError(t, <-updateErr)
	requireOopsCode(t, <-deleteErr, oops.CodeConflict)
	require.True(t, tunnelIsLive(t, ctx, ti, *authCtx.ProjectID, tunnelID))
	require.Len(t, liveMCPServersOnTunnel(t, ctx, ti, *authCtx.ProjectID, tunnelID), 1)
}

func TestCreateMcpServer_RejectsTunnelInAnotherProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuth(t, ctx)
	otherCtx := authztest.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)
	otherAuthCtx := requireAuth(t, otherCtx)
	require.NotEqual(t, *authCtx.ProjectID, *otherAuthCtx.ProjectID)
	foreignTunnel := seedTunneledMcpServer(t, ctx, ti.conn, *otherAuthCtx.ProjectID)

	_, err := ti.service.CreateMcpServer(ctx, createOnTunnelPayload(foreignTunnel))
	requireOopsCode(t, err, oops.CodeInvalid)

	subject := seedMcpServerForBackend(t, ctx, ti, "cross project subject", seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String())
	_, err = ti.service.UpdateMcpServer(ctx, repointToTunnelPayload(subject, foreignTunnel))
	requireOopsCode(t, err, oops.CodeInvalid)

	require.Empty(t, liveMCPServersOnTunnel(t, ctx, ti, *otherAuthCtx.ProjectID, foreignTunnel))
}

// Deleting one MCP server on a shared tunnel removes only that server and its
// endpoints: the tunnel, the sibling and the sibling's endpoint stay live.
func TestDeleteMcpServer_KeepsSharedTunnelAndSibling(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuth(t, ctx)
	tunnelID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	deleted, err := ti.service.CreateMcpServer(ctx, createOnTunnelPayload(tunnelID))
	require.NoError(t, err)
	sibling, err := ti.service.CreateMcpServer(ctx, createOnTunnelPayload(tunnelID))
	require.NoError(t, err)
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, deleted.ID)
	seedEndpointFor(t, ctx, ti.conn, *authCtx.ProjectID, sibling.ID)

	// The caller may write only the server being deleted.
	wrapperOnly := authztest.WithExactGrants(t, ctx, authz.NewGrantWithSelector(authz.ScopeMCPWrite, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
		authz.SelectorKeyResourceID:   deleted.ID,
		authz.SelectorKeyProjectID:    authCtx.ProjectID.String(),
	}))
	require.NoError(t, ti.service.DeleteMcpServer(wrapperOnly, &gen.DeleteMcpServerPayload{ID: deleted.ID}))

	// The same grant cannot delete the sibling.
	err = ti.service.DeleteMcpServer(wrapperOnly, &gen.DeleteMcpServerPayload{ID: sibling.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	require.True(t, tunnelIsLive(t, ctx, ti, *authCtx.ProjectID, tunnelID))
	servers := liveMCPServersOnTunnel(t, ctx, ti, *authCtx.ProjectID, tunnelID)
	require.Len(t, servers, 1)
	require.Equal(t, sibling.ID, servers[0].ID.String())

	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.Equal(t, sibling.ID, endpoints[0].McpServerID.UUID.String())
}

// A gateway cannot hold two MCP servers on one tunnel: repointing a member
// onto a tunnel a sibling member already fronts is refused.
func TestUpdateMcpServer_RejectsTunnelSharedWithMetaMcpSibling(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuth(t, ctx)
	tunnelID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	sibling, err := ti.service.CreateMcpServer(ctx, createOnTunnelPayload(tunnelID))
	require.NoError(t, err)
	subject := seedMcpServerForBackend(t, ctx, ti, "gateway subject", seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String())

	meta, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                "tunnel collision holder",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	for _, memberID := range []string{sibling.ID, subject} {
		_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
			ProjectID:       *authCtx.ProjectID,
			MetaMcpServerID: meta.ID,
			McpServerID:     uuid.MustParse(memberID),
			SortOrder:       0,
		})
		require.NoError(t, err)
	}

	_, err = ti.service.UpdateMcpServer(ctx, repointToTunnelPayload(subject, tunnelID))
	requireOopsCode(t, err, oops.CodeConflict)
	require.Len(t, liveMCPServersOnTunnel(t, ctx, ti, *authCtx.ProjectID, tunnelID), 1)
}

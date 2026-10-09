package tunneledmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// createMCPServerOnTunnel creates a live MCP server on the tunnel through the
// production create path, so it holds the same tunnel lock real creates do.
func createMCPServerOnTunnel(t *testing.T, ctx context.Context, conn *pgxpool.Pool, authCtx *contextvalues.AuthContext, tunnelID uuid.UUID, visibility string) mcpserversrepo.McpServer {
	t.Helper()

	tx := testenv.BeginTx(t, ctx, conn)

	server, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		ActorUserID:         authCtx.UserID,
		ActorEmail:          authCtx.Email,
		Name:                "wrapper " + uuid.NewString(),
		Visibility:          visibility,
		NetworkAccessMode:   networkaccess.ModePublicOnly,
		TunneledMCPServerID: uuid.NullUUID{UUID: tunnelID, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	return server
}

func deleteServerPayload(id uuid.UUID) *gen.DeleteServerPayload {
	return &gen.DeleteServerPayload{
		ID:               id.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func requireTunnelLive(t *testing.T, ctx context.Context, conn *pgxpool.Pool, tunnel repo.TunneledMcpServer) {
	t.Helper()

	current, err := repo.New(conn).GetServerByID(ctx, repo.GetServerByIDParams{ID: tunnel.ID, ProjectID: tunnel.ProjectID})
	require.NoError(t, err)
	require.Equal(t, tunnel.Status, current.Status)
	require.Equal(t, tunnel.KeyHash, current.KeyHash)
}

func requireDeleteAuditCount(t *testing.T, ctx context.Context, conn *pgxpool.Pool, want int64) {
	t.Helper()

	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionTunneledMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, want, count)
}

func TestDeleteServerRefusesWhileMCPServersUseTheTunnel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	tunnel := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	createMCPServerOnTunnel(t, ctx, ti.conn, authCtx, tunnel.ID, "private")

	err := ti.service.DeleteServer(ctx, deleteServerPayload(tunnel.ID))
	requireOopsCode(t, err, oops.CodeConflict)

	requireTunnelLive(t, ctx, ti.conn, tunnel)
	requireDeleteAuditCount(t, ctx, ti.conn, 0)
}

func TestDeleteServerCountsDisabledMCPServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	tunnel := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	createMCPServerOnTunnel(t, ctx, ti.conn, authCtx, tunnel.ID, "disabled")

	err := ti.service.DeleteServer(ctx, deleteServerPayload(tunnel.ID))
	requireOopsCode(t, err, oops.CodeConflict)
	requireTunnelLive(t, ctx, ti.conn, tunnel)
}

func TestDeleteServerSucceedsOnceEveryMCPServerIsDeleted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	tunnel := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	first := createMCPServerOnTunnel(t, ctx, ti.conn, authCtx, tunnel.ID, "private")
	second := createMCPServerOnTunnel(t, ctx, ti.conn, authCtx, tunnel.ID, "disabled")

	_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: first.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	// One remaining MCP server still keeps the tunnel.
	err = ti.service.DeleteServer(ctx, deleteServerPayload(tunnel.ID))
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: second.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServer(ctx, deleteServerPayload(tunnel.ID)))
	requireDeleteAuditCount(t, ctx, ti.conn, 1)

	_, err = repo.New(ti.conn).GetServerByID(ctx, repo.GetServerByIDParams{ID: tunnel.ID, ProjectID: tunnel.ProjectID})
	require.Error(t, err)

	// Repeating the delete is a no-op: no error and no second audit event.
	require.NoError(t, ti.service.DeleteServer(ctx, deleteServerPayload(tunnel.ID)))
	requireDeleteAuditCount(t, ctx, ti.conn, 1)
}

func TestDeleteServerIgnoresAnotherProjectsTunnel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	otherProjectCtx := authztest.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)
	otherAuthCtx := requireAuthContext(t, otherProjectCtx)
	require.NotEqual(t, *authCtx.ProjectID, *otherAuthCtx.ProjectID)
	foreign := seedTunneledMcpServer(t, ctx, ti.conn, *otherAuthCtx.ProjectID)

	require.NoError(t, ti.service.DeleteServer(ctx, deleteServerPayload(foreign.ID)))

	requireTunnelLive(t, ctx, ti.conn, foreign)
	requireDeleteAuditCount(t, ctx, ti.conn, 0)
}

func TestDeleteServerRequiresProjectMCPWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	tunnel := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	wrapper := createMCPServerOnTunnel(t, ctx, ti.conn, authCtx, tunnel.ID, "private")
	_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: wrapper.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	cases := []struct {
		name   string
		grants []authz.Grant
	}{
		{name: "no grants", grants: nil},
		{name: "write on one MCP server", grants: []authz.Grant{authz.NewGrantWithSelector(authz.ScopeMCPWrite, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   wrapper.ID.String(),
			authz.SelectorKeyProjectID:    authCtx.ProjectID.String(),
		})}},
		{name: "write in another project", grants: []authz.Grant{projectScopedMCPGrant(authz.ScopeMCPWrite, uuid.New())}},
	}
	// Runs once every parallel case below has finished. t.Context is
	// cancelled by then, so the checks use a context that outlives it.
	t.Cleanup(func() {
		checkCtx := context.WithoutCancel(ctx)
		requireTunnelLive(t, checkCtx, ti.conn, tunnel)
		requireDeleteAuditCount(t, checkCtx, ti.conn, 0)
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ti.service.DeleteServer(authztest.WithExactGrants(t, ctx, tc.grants...), deleteServerPayload(tunnel.ID))
			requireOopsCode(t, err, oops.CodeForbidden)
		})
	}
}

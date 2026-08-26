// mcp_servers.remote_session_issuer_id is derived, so what matters is that it
// tracks the bindings through every shape they can take — including the ones
// where nothing deletes a row and the value would otherwise go stale.

package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// seedServerOnIssuer creates a remote-backed MCP server carrying userIssuerID,
// the shape every proxied server has: mcpservers mints the user session issuer
// unconditionally, which is why the column has to coexist with it.
func seedServerOnIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, userIssuerID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	remoteServer, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "sse",
		Url:           "https://" + slug + ".example.com/mcp",
	})
	require.NoError(t, err)

	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
	})
	require.NoError(t, err)
	return server.ID
}

func storedIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, serverID uuid.UUID) uuid.NullUUID {
	t.Helper()
	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	return server.RemoteSessionIssuerID
}

func resync(t *testing.T, ctx context.Context, conn *pgxpool.Pool, userIssuerIDs ...uuid.UUID) {
	t.Helper()
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, conn, userIssuerIDs))
}

func TestResyncMCPServerRemoteSessionIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	t.Run("one bound client stamps its issuer", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-one")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-one")
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "a fresh server names no issuer")

		_, remoteIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-one")
		resync(t, ctx, ti.conn, userIssuerID)

		require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID))
	})

	t.Run("two distinct issuers leave it unset", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-two")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-two")

		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-two-a")
		resync(t, ctx, ti.conn, userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "one issuer stamps, or the second adds nothing")

		// AIS-137 dropped the one-client-per-user-session-issuer index, so this
		// is representable; a scalar column cannot say which, so it says none.
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-two-b")
		resync(t, ctx, ti.conn, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "an ambiguous derivation must fail closed, not pick one")
	})

	t.Run("a soft-deleted client clears the stored issuer", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-del")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-del")

		clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-del")
		resync(t, ctx, ti.conn, userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)

		// The binding row survives the soft delete, so nothing but the resync
		// clears the value: the FK's ON DELETE SET NULL never fires because no
		// row is ever hard-deleted.
		_, err := repo.New(ti.conn).DeleteRemoteSessionClient(ctx, repo.DeleteRemoteSessionClientParams{
			ID:        clientID,
			ProjectID: conv.ToNullUUID(projectID),
		})
		require.NoError(t, err)
		resync(t, ctx, ti.conn, userIssuerID)

		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "a soft-deleted client must not keep qualifying a server")
	})

	t.Run("another project's issuer cannot stamp this project's server", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-tenant")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-tenant")
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-tenant")
		resync(t, ctx, ti.conn, userIssuerID)
		before := storedIssuer(t, ctx, ti.conn, projectID, serverID)
		require.True(t, before.Valid)

		// The statement joins the issuer back to the server's project, so an
		// id from elsewhere matches nothing rather than reaching across.
		otherProjectID := createProject(t, ctx, ti.conn, "sync-other-project")
		otherIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "usi-sync-other")
		resync(t, ctx, ti.conn, otherIssuerID)

		require.Equal(t, before, storedIssuer(t, ctx, ti.conn, projectID, serverID))
	})

	t.Run("resync is idempotent and safe on unknown issuers", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-idem")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-idem")
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-idem")

		resync(t, ctx, ti.conn, userIssuerID)
		first := storedIssuer(t, ctx, ti.conn, projectID, serverID)
		resync(t, ctx, ti.conn, userIssuerID, uuid.New())
		require.Equal(t, first, storedIssuer(t, ctx, ti.conn, projectID, serverID))

		require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, nil), "an empty set is a no-op, not an error")
	})
}

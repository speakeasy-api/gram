// mcp_servers.remote_session_issuer_id is derived, so what matters is that it
// tracks the bindings through every shape: the ones where nothing deletes a row
// and the value would go stale, and the ones where a row belongs to somebody
// else.

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
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedServerOnIssuer creates a remote-backed MCP server carrying userIssuerID —
// the shape every proxied server has, since mcpservers mints the issuer
// unconditionally. projectID and userIssuerID are independent so a test can
// build the cross-tenant shape the FK does not forbid.
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

func softDeleteRemoteIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, remoteIssuerID uuid.UUID) {
	t.Helper()

	require.NoError(t, testrepo.New(conn).ForceSoftDeleteRemoteSessionIssuerFixture(ctx, remoteIssuerID))
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

func stampIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, serverID, remoteIssuerID uuid.UUID) {
	t.Helper()

	require.NoError(t, testrepo.New(conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		ID:                    serverID,
		RemoteSessionIssuerID: conv.ToNullUUID(remoteIssuerID),
	}))
}

func resync(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, userIssuerIDs ...uuid.UUID) {
	t.Helper()
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, conn, organizationID, projectID, userIssuerIDs))
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
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)

		require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID))
	})

	t.Run("two distinct issuers leave it unset", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-two")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-two")

		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-two-a")
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "one issuer stamps, or the second adds nothing")

		// AIS-137 dropped the one-client-per-user-session-issuer index, so this
		// is representable; a scalar column cannot say which, so it says none.
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-two-b")
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "an ambiguous derivation must fail closed, not pick one")
	})

	t.Run("a soft-deleted client clears the stored issuer", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-del")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-del")

		clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-del")
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)

		// The binding row survives the soft delete, so nothing but the resync
		// clears the value: the FK's ON DELETE SET NULL never fires because no
		// row is ever hard-deleted.
		_, err := repo.New(ti.conn).DeleteRemoteSessionClient(ctx, repo.DeleteRemoteSessionClientParams{
			ID:        clientID,
			ProjectID: conv.ToNullUUID(projectID),
		})
		require.NoError(t, err)
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)

		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "a soft-deleted client must not keep qualifying a server")
	})

	t.Run("a soft-deleted remote issuer does not qualify a server", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-deadissuer")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-deadissuer")

		_, deadIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-dead")
		softDeleteRemoteIssuer(t, ctx, ti.conn, deadIssuerID)
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"the runtime resolver skips a tombstoned issuer, so the column must not name one")

		// With the tombstoned issuer excluded, a single live client is
		// unambiguous rather than one of two.
		_, liveIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-live")
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.Equal(t, uuid.NullUUID{UUID: liveIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID))
	})

	t.Run("another project's client cannot stamp this project's server", func(t *testing.T) {
		t.Parallel()

		// The join table has no tenancy column, so this cross-project binding is
		// representable. Only the client-visibility rule keeps that client's
		// issuer off this project's server.
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-xclient")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-xclient")

		otherProjectID := createProject(t, ctx, ti.conn, "sync-xclient-project")
		seedActiveClient(t, ctx, ti.conn, otherProjectID, userIssuerID, orgID, "rsi-sync-xclient")

		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"a client owned by another project must not qualify this project's server")
	})

	t.Run("another project's issuer cannot stamp this project's server", func(t *testing.T) {
		t.Parallel()

		// The FK is single-column, so a server in one project can reference an
		// issuer in another. Without the issuer-tenancy arm the resync would
		// match on user_session_issuer_id alone and stamp across the boundary.
		otherProjectID := createProject(t, ctx, ti.conn, "sync-xissuer-project")
		foreignIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "usi-sync-xissuer")
		_, foreignRemoteIssuerID := seedActiveClient(t, ctx, ti.conn, otherProjectID, foreignIssuerID, orgID, "rsi-sync-xissuer")

		strandedServerID := seedServerOnIssuer(t, ctx, ti.conn, projectID, foreignIssuerID, "sync-xissuer")

		resync(t, ctx, ti.conn, orgID, projectID, foreignIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, strandedServerID).Valid,
			"an issuer owned by another project must not reach this project's server")

		// The same issuer does stamp a server in its own project, so the
		// assertion above is about tenancy and not about the row being
		// unreachable for some other reason.
		ownServerID := seedServerOnIssuer(t, ctx, ti.conn, otherProjectID, foreignIssuerID, "sync-xissuer-own")
		resync(t, ctx, ti.conn, orgID, otherProjectID, foreignIssuerID)
		require.Equal(t, uuid.NullUUID{UUID: foreignRemoteIssuerID, Valid: true},
			storedIssuer(t, ctx, ti.conn, otherProjectID, ownServerID))
	})

	t.Run("another organization's caller cannot write these servers", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-xorg")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-xorg")
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-xorg")

		// The ids are read from join tables with no tenancy column, so the
		// caller's own scope is the only thing standing between a stray id and
		// a cross-tenant write.
		otherOrgID := createOrganization(t, ctx, ti.conn, "sync-xorg-other")
		resync(t, ctx, ti.conn, otherOrgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"a caller outside the organization must not write its servers")

		// And the same call from the owning organization does write it.
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)
	})

	t.Run("a project-scoped caller cannot write another project's servers", func(t *testing.T) {
		t.Parallel()

		otherProjectID := createProject(t, ctx, ti.conn, "sync-narrow-project")
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "usi-sync-narrow")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, otherProjectID, userIssuerID, "sync-narrow")
		seedActiveClient(t, ctx, ti.conn, otherProjectID, userIssuerID, orgID, "rsi-sync-narrow")

		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, otherProjectID, serverID).Valid,
			"a project scope must not widen to a sibling project in the same organization")

		resync(t, ctx, ti.conn, orgID, otherProjectID, userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, otherProjectID, serverID).Valid)
	})

	t.Run("an organization-level client counts toward a project issuer", func(t *testing.T) {
		t.Parallel()

		// The attach surface lets a project admin bind an org-level client to
		// their own issuer, so the derivation must count it — and stop counting
		// it once it is gone.
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-orgclient")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-orgclient")

		orgRemoteIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, orgID, "rsi-sync-orgclient")
		clientID := seedOrgLevelRemoteClient(t, ctx, ti.conn, orgID, orgRemoteIssuerID, "cid-sync-orgclient", userIssuerID)

		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.Equal(t, uuid.NullUUID{UUID: orgRemoteIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID))

		_, err := repo.New(ti.conn).DeleteOrganizationRemoteSessionClient(ctx, repo.DeleteOrganizationRemoteSessionClientParams{
			ID:             clientID,
			OrganizationID: conv.ToPGText(orgID),
		})
		require.NoError(t, err)

		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)
	})

	t.Run("resync is idempotent and safe on unknown issuers", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-idem")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-idem")
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-idem")

		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		first := storedIssuer(t, ctx, ti.conn, projectID, serverID)
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID, uuid.New())
		require.Equal(t, first, storedIssuer(t, ctx, ti.conn, projectID, serverID))

		require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, nil), "an empty set is a no-op, not an error")
	})

	t.Run("a resync without a tenant scope is rejected", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-noscope")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-noscope")
		_, remoteIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-noscope")
		stampIssuer(t, ctx, ti.conn, serverID, remoteIssuerID)

		// A missing scope is a caller bug and must not read as "every tenant" —
		// even with the empty id set that would otherwise short-circuit.
		require.Error(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, "", projectID, []uuid.UUID{userIssuerID}))
		require.Error(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, uuid.Nil, []uuid.UUID{userIssuerID}))
		require.Error(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, "", uuid.Nil, nil))
		require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID),
			"a rejected resync must not have written anything")
	})
}

// The read DeleteRemoteSessionClient takes before its purge: the tenancy has
// to live in the query, since an unowned client must yield nothing.
func TestListUserSessionIssuersBoundToProjectClient_Scoping(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	q := repo.New(ti.conn)

	userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-bindscope")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-bindscope")

	owned, err := q.ListUserSessionIssuersBoundToProjectClient(ctx, repo.ListUserSessionIssuersBoundToProjectClientParams{
		RemoteSessionClientID: clientID,
		ProjectID:             conv.ToNullUUID(projectID),
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{userIssuerID}, owned, "the owning project sees the bindings its delete will purge")

	otherProjectID := createProject(t, ctx, ti.conn, "bindscope-other-project")
	foreign, err := q.ListUserSessionIssuersBoundToProjectClient(ctx, repo.ListUserSessionIssuersBoundToProjectClientParams{
		RemoteSessionClientID: clientID,
		ProjectID:             conv.ToNullUUID(otherProjectID),
	})
	require.NoError(t, err)
	require.Empty(t, foreign, "a sibling project must not read another project's client's bindings")
}

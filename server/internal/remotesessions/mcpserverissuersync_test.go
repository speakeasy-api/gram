// mcp_servers.remote_session_issuer_id is written per server, so what matters
// here is the reconcile that keeps it honest: it clears a server whose own
// provider lost its client, leaves every other server on a shared user session
// issuer alone whatever that issuer's other clients do, leaves a server whose
// provider was never configured without one, and writes nothing outside the
// caller's own scope — the ids it takes come from join tables with no tenancy
// column.

package remotesessions_test

import (
	"context"
	"testing"
	"time"

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

// seedUnboundRemoteIssuer creates a project-owned provider with no client
// bound to any user session issuer: what a server stamped with a provider the
// sweep must clear names.
func seedUnboundRemoteIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID, slug string) uuid.UUID {
	t.Helper()

	issuer, err := repo.New(conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              slug,
		Issuer:                            "https://issuer.example.com/" + slug,
		AuthorizationEndpoint:             conv.ToPGText("https://issuer.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://issuer.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
	})
	require.NoError(t, err)
	return issuer.ID
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

func stampIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, serverID, remoteIssuerID uuid.UUID) {
	t.Helper()

	stamped, err := testrepo.New(conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		ID:                    serverID,
		ProjectID:             projectID,
		RemoteSessionIssuerID: conv.ToNullUUID(remoteIssuerID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped, "the issuer stamp must land on exactly one live server")
}

func reconcile(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, userIssuerIDs ...uuid.UUID) {
	t.Helper()
	require.NoError(t, remotesessions.ClearUnboundMCPServerRemoteSessionIssuers(ctx, conn, organizationID, projectID, userIssuerIDs))
}

func TestClearUnboundMCPServerRemoteSessionIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	t.Run("a server alone on its issuer is not configured from its clients", func(t *testing.T) {
		t.Parallel()

		// Nothing derives this column. A server signing in to nothing stays
		// that way until an operator configures it, which is what makes a
		// disable durable, and the identity commit accepts every backend so a
		// toolset-backed or tunneled server has that surface too.
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-solo")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-solo")
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sweep-solo")

		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)
	})

	t.Run("a provider that still has a client is left alone", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-keep")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-keep")
		seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-keep-sibling")

		_, remoteIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sweep-keep")
		stampIssuer(t, ctx, ti.conn, projectID, serverID, remoteIssuerID)
		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)

		require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID))
	})

	t.Run("a server sharing its issuer is never derived", func(t *testing.T) {
		t.Parallel()

		// A server with no provider configured gains one only from an explicit
		// identity commit — never from whichever client happens to be bound
		// for its sibling.
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-noderive")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-noderive")
		seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-noderive-sibling")
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sweep-noderive")

		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"a shared server's provider is configured on it, not derived from its user session issuer")
	})

	t.Run("a second provider on the issuer does not clear a server", func(t *testing.T) {
		t.Parallel()

		// AIM-577: a user session issuer shared by two OAuth-backed servers
		// binds a client per provider. The superseded per-issuer derivation
		// read that as ambiguous and blanked both servers, which left them
		// serving as if no sign-in were needed and failing upstream with 401.
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-two")
		firstID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-two-a")
		secondID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-two-b")

		_, firstProvider := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sweep-two-a")
		_, secondProvider := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sweep-two-b")
		stampIssuer(t, ctx, ti.conn, projectID, firstID, firstProvider)
		stampIssuer(t, ctx, ti.conn, projectID, secondID, secondProvider)

		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)

		require.Equal(t, uuid.NullUUID{UUID: firstProvider, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, firstID))
		require.Equal(t, uuid.NullUUID{UUID: secondProvider, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, secondID))
	})

	t.Run("a soft-deleted client clears only the server naming its provider", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-del")
		losingID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-del-losing")
		keepingID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-del-keeping")

		doomedClientID, doomedProvider := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sweep-del-doomed")
		_, keptProvider := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sweep-del-kept")
		stampIssuer(t, ctx, ti.conn, projectID, losingID, doomedProvider)
		stampIssuer(t, ctx, ti.conn, projectID, keepingID, keptProvider)

		// The binding row survives the soft delete, so nothing but this sweep
		// clears the value: the FK's ON DELETE SET NULL never fires because no
		// row is ever hard-deleted.
		_, err := repo.New(ti.conn).DeleteRemoteSessionClient(ctx, repo.DeleteRemoteSessionClientParams{
			ID:        doomedClientID,
			ProjectID: conv.ToNullUUID(projectID),
		})
		require.NoError(t, err)
		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)

		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, losingID).Valid, "a soft-deleted client must not keep qualifying its server")
		require.Equal(t, uuid.NullUUID{UUID: keptProvider, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, keepingID),
			"another provider losing its client says nothing about this server")
	})

	t.Run("a soft-deleted remote issuer does not qualify a server", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-deadissuer")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-deadissuer")

		_, deadIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sweep-dead")
		stampIssuer(t, ctx, ti.conn, projectID, serverID, deadIssuerID)
		softDeleteRemoteIssuer(t, ctx, ti.conn, deadIssuerID)

		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"the runtime resolver skips a tombstoned issuer, so the column must not name one")
	})

	t.Run("another project's client does not keep this project's server qualified", func(t *testing.T) {
		t.Parallel()

		// The join table has no tenancy column, so this cross-project binding is
		// representable. The client-visibility rule is what makes that client
		// invisible to this project's server, which therefore has nothing
		// keeping its provider alive.
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-xclient")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-xclient")

		otherProjectID := createProject(t, ctx, ti.conn, "sweep-xclient-project")
		_, foreignProvider := seedActiveClient(t, ctx, ti.conn, otherProjectID, userIssuerID, orgID, "rsi-sweep-xclient")
		stampIssuer(t, ctx, ti.conn, projectID, serverID, foreignProvider)

		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"a client owned by another project must not qualify this project's server")
	})

	t.Run("another project's issuer does not reach this project's server", func(t *testing.T) {
		t.Parallel()

		// The FK is single-column, so a server in one project can reference an
		// issuer in another. Without the issuer-tenancy arm the sweep would
		// match on user_session_issuer_id alone and clear across the boundary.
		otherProjectID := createProject(t, ctx, ti.conn, "sweep-xissuer-project")
		foreignIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "usi-sweep-xissuer")
		_, foreignProvider := seedActiveClient(t, ctx, ti.conn, otherProjectID, foreignIssuerID, orgID, "rsi-sweep-xissuer")

		strandedServerID := seedServerOnIssuer(t, ctx, ti.conn, projectID, foreignIssuerID, "sweep-xissuer")
		stampIssuer(t, ctx, ti.conn, projectID, strandedServerID, foreignProvider)

		reconcile(t, ctx, ti.conn, orgID, projectID, foreignIssuerID)
		require.Equal(t, uuid.NullUUID{UUID: foreignProvider, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, strandedServerID),
			"an issuer owned by another project must not reach this project's server")

		// A server in the issuer's own project is reachable, so the assertion
		// above is about tenancy and not about the row being unreachable for
		// some other reason.
		ownServerID := seedServerOnIssuer(t, ctx, ti.conn, otherProjectID, foreignIssuerID, "sweep-xissuer-own")
		stampIssuer(t, ctx, ti.conn, otherProjectID, ownServerID, seedUnboundRemoteIssuer(t, ctx, ti.conn, otherProjectID, orgID, "rsi-sweep-xissuer-unbound"))
		reconcile(t, ctx, ti.conn, orgID, otherProjectID, foreignIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, otherProjectID, ownServerID).Valid)
	})

	t.Run("another organization's caller cannot write these servers", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-xorg")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-xorg")
		unboundProvider := seedUnboundRemoteIssuer(t, ctx, ti.conn, projectID, orgID, "rsi-sweep-xorg")
		stampIssuer(t, ctx, ti.conn, projectID, serverID, unboundProvider)

		// The ids are read from join tables with no tenancy column, so the
		// caller's own scope is the only thing standing between a stray id and
		// a cross-tenant write.
		otherOrgID := createOrganization(t, ctx, ti.conn, "sweep-xorg-other")
		reconcile(t, ctx, ti.conn, otherOrgID, projectID, userIssuerID)
		require.Equal(t, uuid.NullUUID{UUID: unboundProvider, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID),
			"a caller outside the organization must not write its servers")

		// And the same call from the owning organization does write it.
		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)
	})

	t.Run("a project-scoped caller cannot write another project's servers", func(t *testing.T) {
		t.Parallel()

		otherProjectID := createProject(t, ctx, ti.conn, "sweep-narrow-project")
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "usi-sweep-narrow")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, otherProjectID, userIssuerID, "sweep-narrow")
		unboundProvider := seedUnboundRemoteIssuer(t, ctx, ti.conn, otherProjectID, orgID, "rsi-sweep-narrow")
		stampIssuer(t, ctx, ti.conn, otherProjectID, serverID, unboundProvider)

		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.Equal(t, uuid.NullUUID{UUID: unboundProvider, Valid: true}, storedIssuer(t, ctx, ti.conn, otherProjectID, serverID),
			"a project scope must not widen to a sibling project in the same organization")

		reconcile(t, ctx, ti.conn, orgID, otherProjectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, otherProjectID, serverID).Valid)
	})

	t.Run("an organization-level client keeps a project issuer's server qualified", func(t *testing.T) {
		t.Parallel()

		// The attach surface lets a project admin bind an org-level client to
		// their own issuer, so the sweep must count it — and stop counting it
		// once it is gone.
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-orgclient")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-orgclient")

		orgRemoteIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, orgID, "rsi-sweep-orgclient")
		clientID := seedOrgLevelRemoteClient(t, ctx, ti.conn, orgID, orgRemoteIssuerID, "cid-sweep-orgclient", userIssuerID)
		stampIssuer(t, ctx, ti.conn, projectID, serverID, orgRemoteIssuerID)

		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.Equal(t, uuid.NullUUID{UUID: orgRemoteIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID))

		_, err := repo.New(ti.conn).DeleteOrganizationRemoteSessionClient(ctx, repo.DeleteOrganizationRemoteSessionClientParams{
			ID:             clientID,
			OrganizationID: conv.ToPGText(orgID),
		})
		require.NoError(t, err)

		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)
	})

	t.Run("the reconcile is idempotent and safe on unknown issuers", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-idem")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-idem")
		_, remoteIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sweep-idem")
		stampIssuer(t, ctx, ti.conn, projectID, serverID, remoteIssuerID)

		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		first := storedIssuer(t, ctx, ti.conn, projectID, serverID)
		reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID, uuid.New())
		require.Equal(t, first, storedIssuer(t, ctx, ti.conn, projectID, serverID))

		require.NoError(t, remotesessions.ClearUnboundMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, nil), "an empty set is a no-op, not an error")
	})

	t.Run("a reconcile without a tenant scope is rejected", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sweep-noscope")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sweep-noscope")
		unboundProvider := seedUnboundRemoteIssuer(t, ctx, ti.conn, projectID, orgID, "rsi-sweep-noscope")
		stampIssuer(t, ctx, ti.conn, projectID, serverID, unboundProvider)

		// A missing scope is a caller bug and must not read as "every tenant" —
		// even with the empty id set that would otherwise short-circuit.
		require.Error(t, remotesessions.ClearUnboundMCPServerRemoteSessionIssuers(ctx, ti.conn, "", projectID, []uuid.UUID{userIssuerID}))
		require.Error(t, remotesessions.ClearUnboundMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, uuid.Nil, []uuid.UUID{userIssuerID}))
		require.Error(t, remotesessions.ClearUnboundMCPServerRemoteSessionIssuers(ctx, ti.conn, "", uuid.Nil, nil))
		require.Equal(t, uuid.NullUUID{UUID: unboundProvider, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID),
			"a rejected reconcile must not have written anything")
	})
}

// Addendum to AIM-577's review: the clear reads its candidates and then
// writes, so a configuration committed in between must win. A shared server's
// explicitly configured provider is not something a later pass can
// reconstruct, so losing it would be permanent.
func TestClearMCPServerRemoteSessionIssuerLosesToAConfigurationCommit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-clear-cas")
	serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "clear-cas")
	seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "clear-cas-sibling")
	gone := seedUnboundRemoteIssuer(t, ctx, ti.conn, projectID, orgID, "rsi-clear-cas-gone")
	stampIssuer(t, ctx, ti.conn, projectID, serverID, gone)

	q := mcpserversrepo.New(ti.conn)
	stale, err := q.ListMCPServerRemoteSessionIssuersToClear(ctx, mcpserversrepo.ListMCPServerRemoteSessionIssuersToClearParams{
		UserSessionIssuerIds: []uuid.UUID{userIssuerID},
		OrganizationID:       orgID,
		ProjectID:            projectID,
	})
	require.NoError(t, err)
	require.Len(t, stale, 1, "the server names a provider with no client")

	// A commit configures the server, and binds a client for what it chose,
	// after the candidates were read.
	_, configured := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-clear-cas-configured")
	rows, err := q.StampMCPServerRemoteSessionIssuer(ctx, mcpserversrepo.StampMCPServerRemoteSessionIssuerParams{
		RemoteSessionIssuerID: conv.ToNullUUID(configured),
		ID:                    serverID,
		ProjectID:             projectID,
		UserSessionIssuerID:   conv.ToNullUUID(userIssuerID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	cleared, err := q.ClearMCPServerRemoteSessionIssuerIfUnchanged(ctx, mcpserversrepo.ClearMCPServerRemoteSessionIssuerIfUnchangedParams{
		ID:                            stale[0].ID,
		ProjectID:                     projectID,
		OrganizationID:                orgID,
		ObservedRemoteSessionIssuerID: stale[0].RemoteSessionIssuerID,
		ObservedUpdatedAt:             stale[0].ObservedUpdatedAt,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), cleared, "the clear must not overwrite the provider committed after its read")
	require.Equal(t, conv.ToNullUUID(configured), storedIssuer(t, ctx, ti.conn, projectID, serverID))

	// Run end to end, as the post-commit pass does: the server keeps what the
	// commit configured, because its client is bound.
	reconcile(t, ctx, ti.conn, orgID, projectID, userIssuerID)
	require.Equal(t, conv.ToNullUUID(configured), storedIssuer(t, ctx, ti.conn, projectID, serverID))
}

// The operator backfill reads every server, decides, then writes. An
// administrator configuring User Identity in between is the authority on their
// own server, so the write is conditional on the provider and the row version
// the read observed and loses that race instead of reverting it.
func TestRepairMCPServerRemoteSessionIssuerIfUnchanged(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-repair")
	serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "repair")
	observed := seedUnboundRemoteIssuer(t, ctx, ti.conn, projectID, orgID, "rsi-repair-observed")
	repaired := seedUnboundRemoteIssuer(t, ctx, ti.conn, projectID, orgID, "rsi-repair-target")
	stampIssuer(t, ctx, ti.conn, projectID, serverID, observed)

	read := func() mcpserversrepo.McpServer {
		server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
			ID:        serverID,
			ProjectID: projectID,
		})
		require.NoError(t, err)
		return server
	}
	repair := func(from mcpserversrepo.McpServer, next uuid.NullUUID) int64 {
		rows, err := mcpserversrepo.New(ti.conn).RepairMCPServerRemoteSessionIssuerIfUnchanged(ctx, mcpserversrepo.RepairMCPServerRemoteSessionIssuerIfUnchangedParams{
			RemoteSessionIssuerID:         next,
			ID:                            serverID,
			ProjectID:                     projectID,
			UserSessionIssuerID:           conv.ToNullUUID(userIssuerID),
			ObservedRemoteSessionIssuerID: from.RemoteSessionIssuerID,
			ObservedUpdatedAt:             from.UpdatedAt,
		})
		require.NoError(t, err)
		return rows
	}

	// The identity commit's own write, which advances the row version the way
	// a real save does; the fixture stamp above does not.
	configure := func(provider uuid.UUID) {
		rows, err := mcpserversrepo.New(ti.conn).StampMCPServerRemoteSessionIssuer(ctx, mcpserversrepo.StampMCPServerRemoteSessionIssuerParams{
			RemoteSessionIssuerID: conv.ToNullUUID(provider),
			ID:                    serverID,
			ProjectID:             projectID,
			UserSessionIssuerID:   conv.ToNullUUID(userIssuerID),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), rows)
	}

	// An identity commit changing the provider lands between read and write.
	before := read()
	configure(repaired)
	require.Equal(t, int64(0), repair(before, conv.ToNullUUID(observed)),
		"a write against a stale provider must not land")
	require.Equal(t, conv.ToNullUUID(repaired), storedIssuer(t, ctx, ti.conn, projectID, serverID))

	// And one that re-saves the provider the row already named: the value is
	// unchanged, so only the version tells the backfill it lost the race.
	before = read()
	configure(repaired)
	require.Equal(t, before.RemoteSessionIssuerID, read().RemoteSessionIssuerID, "the save kept the same provider")
	require.Equal(t, int64(0), repair(before, uuid.NullUUID{UUID: uuid.Nil, Valid: false}),
		"an explicit save that keeps the provider must still win")
	require.Equal(t, conv.ToNullUUID(repaired), storedIssuer(t, ctx, ti.conn, projectID, serverID))

	// Against what is actually stored it lands, including a clear.
	require.Equal(t, int64(1), repair(read(), conv.ToNullUUID(observed)))
	require.Equal(t, conv.ToNullUUID(observed), storedIssuer(t, ctx, ti.conn, projectID, serverID))
	require.Equal(t, int64(1), repair(read(), uuid.NullUUID{UUID: uuid.Nil, Valid: false}))
	require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)

	// An unset provider is matched as unset, not skipped.
	require.Equal(t, int64(1), repair(read(), conv.ToNullUUID(repaired)))
	require.Equal(t, conv.ToNullUUID(repaired), storedIssuer(t, ctx, ti.conn, projectID, serverID))
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

// seedUnlinkedClient creates a provider with a live client that is bound to no
// user session issuer, so a server stamped with it reads as a clear candidate
// until something binds that client.
func seedUnlinkedClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID, slug string) (clientID, remoteIssuerID uuid.UUID) {
	t.Helper()

	q := repo.New(conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              slug,
		Issuer:                            "https://issuer.example.com/" + slug,
		AuthorizationEndpoint:             conv.ToPGText("https://issuer.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://issuer.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
	})
	require.NoError(t, err)

	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(projectID),
		OrganizationID:          conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "cid-" + slug,
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"),
	})
	require.NoError(t, err)
	return client.ID, issuer.ID
}

// requireBlockedOnMCPServers waits until a statement matching every fragment is
// parked on an mcp_servers row lock, which is how a test knows the write under
// test has taken its snapshot and is waiting for the configuration commit
// rather than racing it.
//
// Scoped to this test's own database: tests run in parallel against sibling
// clones of it, and matching a lock any of those holds would let the wait
// return before this write ever blocked.
func requireBlockedOnMCPServers(t *testing.T, ctx context.Context, conn *pgxpool.Pool, fragments ...string) {
	t.Helper()
	require.NotEmpty(t, fragments, "an unscoped wait would match another test's lock")
	require.Eventually(t, func() bool {
		var blocked bool
		//nolint:glint // notestingrawsql: pg_stat_activity is server state, not application data, and has no SQLc query.
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE wait_event_type = 'Lock'
			  AND datname = current_database()
			  AND query ILIKE ALL (SELECT '%' || f || '%' FROM unnest($1::text[]) AS f))`, fragments).Scan(&blocked); err != nil {
			return false
		}
		return blocked
	}, 10*time.Second, 20*time.Millisecond, "the write never blocked on the configuration commit's row lock")
}

// The configuration commit that races the clear may choose the provider the
// server already names. The provider comparison is satisfied by that, and the
// client the commit binds is invisible to the clear's statement snapshot even
// after the row lock is released, so only the row version refuses the write.
func TestClearMCPServerRemoteSessionIssuerLosesToASameProviderConfigurationCommit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-clear-same")
	serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "clear-same")
	clientID, provider := seedUnlinkedClient(t, ctx, ti.conn, projectID, orgID, "rsi-clear-same")
	stampIssuer(t, ctx, ti.conn, projectID, serverID, provider)

	q := mcpserversrepo.New(ti.conn)
	stale, err := q.ListMCPServerRemoteSessionIssuersToClear(ctx, mcpserversrepo.ListMCPServerRemoteSessionIssuersToClearParams{
		UserSessionIssuerIds: []uuid.UUID{userIssuerID},
		OrganizationID:       orgID,
		ProjectID:            projectID,
	})
	require.NoError(t, err)
	require.Len(t, stale, 1, "the server names a provider whose client is bound nowhere")
	require.Equal(t, conv.ToNullUUID(provider), stale[0].RemoteSessionIssuerID)

	// The commit holds the server row while it binds the provider's client and
	// configures the same provider again, exactly as reconfiguring User
	// Identity on that server does.
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only, so the clear can block on a row this commit still holds; every read and write below uses generated SQLc methods.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.NoError(t, repo.New(tx).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userIssuerID,
	}))
	restamped, err := mcpserversrepo.New(tx).StampMCPServerRemoteSessionIssuer(ctx, mcpserversrepo.StampMCPServerRemoteSessionIssuerParams{
		RemoteSessionIssuerID: conv.ToNullUUID(provider),
		ID:                    serverID,
		ProjectID:             projectID,
		UserSessionIssuerID:   conv.ToNullUUID(userIssuerID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), restamped)

	type clearResult struct {
		rows int64
		err  error
	}
	done := make(chan clearResult, 1)
	go func() {
		rows, err := q.ClearMCPServerRemoteSessionIssuerIfUnchanged(ctx, mcpserversrepo.ClearMCPServerRemoteSessionIssuerIfUnchangedParams{
			ID:                            stale[0].ID,
			ProjectID:                     projectID,
			OrganizationID:                orgID,
			ObservedRemoteSessionIssuerID: stale[0].RemoteSessionIssuerID,
			ObservedUpdatedAt:             stale[0].ObservedUpdatedAt,
		})
		done <- clearResult{rows: rows, err: err}
	}()

	requireBlockedOnMCPServers(t, ctx, ti.conn, "UPDATE mcp_servers")
	require.NoError(t, tx.Commit(ctx))

	var result clearResult
	select {
	case result = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the clear never finished after the configuration commit released the row")
	}
	require.NoError(t, result.err)
	require.Equal(t, int64(0), result.rows, "the clear must lose to a commit that reconfigured the same provider")
	require.Equal(t, conv.ToNullUUID(provider), storedIssuer(t, ctx, ti.conn, projectID, serverID))
}

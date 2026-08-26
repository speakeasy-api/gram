// mcp_servers.remote_session_issuer_id is derived, so what matters is that it
// tracks the bindings through every shape they can take — including the ones
// where nothing deletes a row and the value would otherwise go stale, and the
// ones where a row exists but belongs to somebody else.

package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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

// seedServerOnIssuer creates a remote-backed MCP server carrying userIssuerID,
// the shape every proxied server has: mcpservers mints the user session issuer
// unconditionally, which is why the column has to coexist with it. projectID
// and userIssuerID are independent so a test can build the cross-tenant shape
// the FK does not forbid.
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

// createOrgTierUserSessionIssuer creates an organization-tier user session
// issuer (project_id NULL). No repo method mints one yet — the column only
// became nullable recently — but the resync has to handle the shape, because
// the arm that skips it also skips clearing a value written earlier.
func createOrgTierUserSessionIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug string) uuid.UUID {
	t.Helper()

	id, err := testrepo.New(conn).CreateOrganizationTierUserSessionIssuerFixture(ctx, testrepo.CreateOrganizationTierUserSessionIssuerFixtureParams{
		OrganizationID:  conv.ToPGText(organizationID),
		Slug:            slug,
		SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return id
}

// softDeleteRemoteIssuer tombstones a remote session issuer directly. Every
// handler refuses to delete one while a live client references it, so this is
// the only way to build the state the derivation has to reject.
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

func resync(t *testing.T, ctx context.Context, conn *pgxpool.Pool, scope remotesessions.ResyncScope, userIssuerIDs ...uuid.UUID) {
	t.Helper()
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, conn, scope, userIssuerIDs))
}

func TestResyncMCPServerRemoteSessionIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	scope := remotesessions.ProjectResyncScope(orgID, projectID)

	t.Run("one bound client stamps its issuer", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-one")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-one")
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "a fresh server names no issuer")

		_, remoteIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-one")
		resync(t, ctx, ti.conn, scope, userIssuerID)

		require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID))
	})

	t.Run("two distinct issuers leave it unset", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-two")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-two")

		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-two-a")
		resync(t, ctx, ti.conn, scope, userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "one issuer stamps, or the second adds nothing")

		// AIS-137 dropped the one-client-per-user-session-issuer index, so this
		// is representable; a scalar column cannot say which, so it says none.
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-two-b")
		resync(t, ctx, ti.conn, scope, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "an ambiguous derivation must fail closed, not pick one")
	})

	t.Run("a soft-deleted client clears the stored issuer", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-del")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-del")

		clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-del")
		resync(t, ctx, ti.conn, scope, userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)

		// The binding row survives the soft delete, so nothing but the resync
		// clears the value: the FK's ON DELETE SET NULL never fires because no
		// row is ever hard-deleted.
		_, err := repo.New(ti.conn).DeleteRemoteSessionClient(ctx, repo.DeleteRemoteSessionClientParams{
			ID:        clientID,
			ProjectID: conv.ToNullUUID(projectID),
		})
		require.NoError(t, err)
		resync(t, ctx, ti.conn, scope, userIssuerID)

		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid, "a soft-deleted client must not keep qualifying a server")
	})

	t.Run("a soft-deleted remote issuer does not qualify a server", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-deadissuer")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-deadissuer")

		_, deadIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-dead")
		softDeleteRemoteIssuer(t, ctx, ti.conn, deadIssuerID)
		resync(t, ctx, ti.conn, scope, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"the runtime resolver skips a tombstoned issuer, so the column must not name one")

		// With the tombstoned issuer excluded, a single live client is
		// unambiguous rather than one of two.
		_, liveIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-live")
		resync(t, ctx, ti.conn, scope, userIssuerID)
		require.Equal(t, uuid.NullUUID{UUID: liveIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID))
	})

	t.Run("another project's client cannot stamp this project's server", func(t *testing.T) {
		t.Parallel()

		// The join table carries no tenancy column, so a binding between this
		// project's user session issuer and another project's client is
		// representable. Only the derivation's own client-visibility rule keeps
		// that client's issuer off this project's server.
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-xclient")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-xclient")

		otherProjectID := createProject(t, ctx, ti.conn, "sync-xclient-project")
		seedActiveClient(t, ctx, ti.conn, otherProjectID, userIssuerID, orgID, "rsi-sync-xclient")

		resync(t, ctx, ti.conn, scope, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"a client owned by another project must not qualify this project's server")
	})

	t.Run("another project's issuer cannot stamp this project's server", func(t *testing.T) {
		t.Parallel()

		// mcp_servers_user_session_issuer_id_fkey is a plain single-column FK,
		// so a server in one project can reference an issuer in another. This
		// builds exactly that row: without the issuer-tenancy arm the resync
		// would match it on user_session_issuer_id alone and stamp across the
		// boundary.
		otherProjectID := createProject(t, ctx, ti.conn, "sync-xissuer-project")
		foreignIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "usi-sync-xissuer")
		_, foreignRemoteIssuerID := seedActiveClient(t, ctx, ti.conn, otherProjectID, foreignIssuerID, orgID, "rsi-sync-xissuer")

		strandedServerID := seedServerOnIssuer(t, ctx, ti.conn, projectID, foreignIssuerID, "sync-xissuer")

		resync(t, ctx, ti.conn, remotesessions.OrganizationResyncScope(orgID), foreignIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, strandedServerID).Valid,
			"an issuer owned by another project must not reach this project's server")

		// The same issuer does stamp a server in its own project, so the
		// assertion above is about tenancy and not about the row being
		// unreachable for some other reason.
		ownServerID := seedServerOnIssuer(t, ctx, ti.conn, otherProjectID, foreignIssuerID, "sync-xissuer-own")
		resync(t, ctx, ti.conn, remotesessions.OrganizationResyncScope(orgID), foreignIssuerID)
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
		resync(t, ctx, ti.conn, remotesessions.OrganizationResyncScope(otherOrgID), userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"a caller outside the organization must not write its servers")

		// And the same call from the owning organization does write it.
		resync(t, ctx, ti.conn, scope, userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid)
	})

	t.Run("a project-scoped caller cannot write another project's servers", func(t *testing.T) {
		t.Parallel()

		otherProjectID := createProject(t, ctx, ti.conn, "sync-narrow-project")
		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "usi-sync-narrow")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, otherProjectID, userIssuerID, "sync-narrow")
		seedActiveClient(t, ctx, ti.conn, otherProjectID, userIssuerID, orgID, "rsi-sync-narrow")

		resync(t, ctx, ti.conn, scope, userIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, otherProjectID, serverID).Valid,
			"a project scope must not widen to a sibling project in the same organization")

		resync(t, ctx, ti.conn, remotesessions.ProjectResyncScope(orgID, otherProjectID), userIssuerID)
		require.True(t, storedIssuer(t, ctx, ti.conn, otherProjectID, serverID).Valid)
	})

	t.Run("an organization-tier issuer is stamped and cleared", func(t *testing.T) {
		t.Parallel()

		// project_id is NULL here, so the plain s.project_id = usi.project_id
		// arm evaluates to NULL and matches nothing. That fails closed for
		// stamping but open for clearing, which is the dangerous half: a value
		// written while the issuer was project-scoped could never go away.
		orgIssuerID := createOrgTierUserSessionIssuer(t, ctx, ti.conn, orgID, "usi-sync-orgtier")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, orgIssuerID, "sync-orgtier")

		orgRemoteIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, orgID, "rsi-sync-orgtier")
		clientID := seedOrgLevelRemoteClient(t, ctx, ti.conn, orgID, orgRemoteIssuerID, "cid-sync-orgtier", orgIssuerID)

		resync(t, ctx, ti.conn, scope, orgIssuerID)
		require.Equal(t, uuid.NullUUID{UUID: orgRemoteIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID),
			"an organization-tier issuer must reach the servers in its organization's projects")

		_, err := repo.New(ti.conn).DeleteOrganizationRemoteSessionClient(ctx, repo.DeleteOrganizationRemoteSessionClientParams{
			ID:             clientID,
			OrganizationID: conv.ToPGText(orgID),
		})
		require.NoError(t, err)

		resync(t, ctx, ti.conn, scope, orgIssuerID)
		require.False(t, storedIssuer(t, ctx, ti.conn, projectID, serverID).Valid,
			"clearing is the half that fails open when the organization arm is missing")
	})

	t.Run("resync is idempotent and safe on unknown issuers", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-idem")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-idem")
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-idem")

		resync(t, ctx, ti.conn, scope, userIssuerID)
		first := storedIssuer(t, ctx, ti.conn, projectID, serverID)
		resync(t, ctx, ti.conn, scope, userIssuerID, uuid.New())
		require.Equal(t, first, storedIssuer(t, ctx, ti.conn, projectID, serverID))

		require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, scope, nil), "an empty set is a no-op, not an error")
	})

	t.Run("a resync without a tenant scope is rejected", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-sync-noscope")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "sync-noscope")
		_, remoteIssuerID := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-sync-noscope")
		stampIssuer(t, ctx, ti.conn, serverID, remoteIssuerID)

		// The zero ResyncScope is what a caller that forgot to pass one would
		// produce, and it must not read as "every tenant".
		err := remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, remotesessions.ResyncScope{}, []uuid.UUID{userIssuerID})
		require.Error(t, err)
		require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, storedIssuer(t, ctx, ti.conn, projectID, serverID),
			"a rejected resync must not have written anything")

		// An empty set is the one input that would otherwise slip past the
		// scope check, and a caller holding one is no more entitled to skip
		// its scope than any other.
		require.Error(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, remotesessions.ResyncScope{}, nil),
			"a missing scope is a caller bug whatever the id set happens to be")
	})
}

// TestListUserSessionIssuersBoundToClient_Scoping covers the reads the three
// client deletes take before they have proven anything: they run ahead of the
// ownership check because the derivation locks they feed must precede every row
// lock, so the tenancy has to be in the query itself. The platform-admin form
// is deliberately unscoped, having no tenant to scope to, and is an integrity
// assertion rather than a resync input.
func TestListUserSessionIssuersBoundToClient_Scoping(t *testing.T) {
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

	orgIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-bindscope-org")
	orgRemoteIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, orgID, "rsi-bindscope-org")
	orgClientID := seedOrgLevelRemoteClient(t, ctx, ti.conn, orgID, orgRemoteIssuerID, "cid-bindscope-org", orgIssuerID)

	ownedByOrg, err := q.ListUserSessionIssuersBoundToOrganizationClient(ctx, repo.ListUserSessionIssuersBoundToOrganizationClientParams{
		RemoteSessionClientID: orgClientID,
		OrganizationID:        conv.ToPGText(orgID),
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{orgIssuerID}, ownedByOrg)

	otherOrgID := createOrganization(t, ctx, ti.conn, "bindscope-other-org")
	foreignByOrg, err := q.ListUserSessionIssuersBoundToOrganizationClient(ctx, repo.ListUserSessionIssuersBoundToOrganizationClientParams{
		RemoteSessionClientID: orgClientID,
		OrganizationID:        conv.ToPGText(otherOrgID),
	})
	require.NoError(t, err)
	require.Empty(t, foreignByOrg, "another organization must not read this organization's client's bindings")

	// The unscoped form still reports everything, which is the whole point of
	// keeping it: the platform-admin delete uses it to notice bindings that
	// should be impossible on a global client.
	unscoped, err := q.ListUserSessionIssuersBoundToClient(ctx, orgClientID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{orgIssuerID}, unscoped)
}

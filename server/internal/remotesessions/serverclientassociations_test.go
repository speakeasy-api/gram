package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgtype"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func associatedClients(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, serverID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := upstreamclientsrepo.New(conn).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{
		McpServerID: serverID,
		ProjectID:   projectID,
	})
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.RemoteSessionClientID)
	}
	return ids
}

func associate(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID, serverID, clientID uuid.UUID) int64 {
	t.Helper()
	n, err := upstreamclientsrepo.New(conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
		OrganizationID:        organizationID,
		RemoteSessionClientID: clientID,
		McpServerID:           serverID,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
	return n
}

// offeredAuthClients is the live links through which one MCP server offers
// typed auth clients. A retired link stops the offer; the client row survives.
func offeredAuthClients(t *testing.T, ctx context.Context, conn *pgxpool.Pool, serverID uuid.UUID) []uuid.UUID {
	t.Helper()
	ids, err := authclientbackfill.New(conn).ListServerAuthClientsFixture(ctx, serverID)
	require.NoError(t, err)
	return ids
}

// recordTypedClients writes the typed auth clients and links the servers'
// associations call for, as every association writer does.
func recordTypedClients(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, serverIDs ...uuid.UUID) {
	t.Helper()
	tx := testenv.BeginTx(t, ctx, conn)
	require.NoError(t, authclientbackfill.SyncServerAssociations(ctx, tx, organizationID, serverIDs...))
	require.NoError(t, tx.Commit(ctx))
}

// enforceUpstreamAuth turns on the organization's upstream auth enforcement,
// the state in which its servers are served from typed auth clients.
func enforceUpstreamAuth(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()
	_, err := productfeaturesrepo.New(conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
	})
	require.NoError(t, err)
}

// seedAdministratorAuthClient creates a static-credential auth client an
// administrator owns and offers it on the server, the records the sync must
// never touch.
func seedAdministratorAuthClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	client, err := authclientsrepo.New(conn).CreateAuthClient(ctx, authclientsrepo.CreateAuthClientParams{
		OrganizationID:        organizationID,
		ProjectID:             uuid.NullUUID{UUID: projectID, Valid: true},
		ClientType:            "static_credential",
		Name:                  "Administrator credential",
		CreatedByUserID:       conv.ToPGText("admin-user"),
		RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientAttachmentScope: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	_, err = authclientsrepo.New(conn).AttachAuthClientToServer(ctx, authclientsrepo.AttachAuthClientToServerParams{
		McpServerID:    serverID,
		AuthClientID:   client.ID,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return client.ID
}

// authClientLive reports whether the typed auth client row itself is still
// live. Retiring a link never deletes the client: only the offline backfill
// retires one no server reaches.
func authClientLive(t *testing.T, ctx context.Context, conn *pgxpool.Pool, authClientID uuid.UUID) bool {
	t.Helper()
	row, err := authclientbackfill.New(conn).GetAuthClientFixture(ctx, authClientID)
	require.NoError(t, err)
	return !row.Deleted
}

func TestMCPServerUpstreamClientAssociations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	t.Run("the only server on an issuer is associated with its bound clients", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-assoc-sole")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-sole")
		clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-assoc-sole")

		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, projectID, serverID))

		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, projectID, serverID), "reconciling again must not duplicate the association")
	})

	t.Run("a shared issuer's binding proves nothing about its servers", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-assoc-shared")
		first := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-shared-a")
		second := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-shared-b")
		seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-assoc-shared")

		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)
		require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, first), "a shared issuer's client must not be copied onto every server")
		require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, second))
	})

	t.Run("two servers on one issuer keep different clients", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-assoc-two")
		linear := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-two-linear")
		notion := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-two-notion")
		linearClient, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-assoc-two-linear")
		notionClient, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-assoc-two-notion")

		require.Equal(t, int64(1), associate(t, ctx, ti.conn, orgID, projectID, linear, linearClient))
		require.Equal(t, int64(1), associate(t, ctx, ti.conn, orgID, projectID, notion, notionClient))
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)

		require.Equal(t, []uuid.UUID{linearClient}, associatedClients(t, ctx, ti.conn, projectID, linear))
		require.Equal(t, []uuid.UUID{notionClient}, associatedClients(t, ctx, ti.conn, projectID, notion))
	})

	t.Run("detaching one server leaves the shared client and its other server", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-assoc-detach")
		first := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-detach-a")
		second := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-detach-b")
		clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-assoc-detach")
		associate(t, ctx, ti.conn, orgID, projectID, first, clientID)
		associate(t, ctx, ti.conn, orgID, projectID, second, clientID)

		listed, err := ti.service.ListServerClientAssociations(ctx, &gen.ListServerClientAssociationsPayload{McpServerID: first.String()})
		require.NoError(t, err)
		require.Len(t, listed.Associations, 1)
		require.Equal(t, int64(2), listed.Associations[0].ServerCount)
		require.Equal(t, "project", listed.Associations[0].Scope)

		require.NoError(t, ti.service.DetachServerClientAssociation(ctx, &gen.DetachServerClientAssociationPayload{
			McpServerID:           first.String(),
			RemoteSessionClientID: clientID.String(),
		}))

		require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, first))
		require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, projectID, second))
		client, err := repo.New(ti.conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
			ID:             clientID,
			ProjectID:      projectID,
			OrganizationID: orgID,
		})
		require.NoError(t, err, "detaching a server must not delete its client")
		require.Equal(t, clientID, client.RemoteSessionClient.ID)

		err = ti.service.DetachServerClientAssociation(ctx, &gen.DetachServerClientAssociationPayload{
			McpServerID:           first.String(),
			RemoteSessionClientID: clientID.String(),
		})
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeNotFound, shareable.Code, "a second detach finds nothing to remove")
	})

	t.Run("detaching a server stops it offering that client", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-assoc-typed")
		first := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-typed-a")
		second := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-typed-b")
		clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-assoc-typed")
		associate(t, ctx, ti.conn, orgID, projectID, first, clientID)
		associate(t, ctx, ti.conn, orgID, projectID, second, clientID)
		recordTypedClients(t, ctx, ti.conn, orgID, first, second)

		offered := offeredAuthClients(t, ctx, ti.conn, first)
		require.NotEmpty(t, offered, "the association must be offered through a link to retire")
		require.NotEmpty(t, offeredAuthClients(t, ctx, ti.conn, second))

		require.NoError(t, ti.service.DetachServerClientAssociation(ctx, &gen.DetachServerClientAssociationPayload{
			McpServerID:           first.String(),
			RemoteSessionClientID: clientID.String(),
		}))

		require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, first))
		require.Empty(t, offeredAuthClients(t, ctx, ti.conn, first),
			"the link goes with the association it was derived from")
		require.True(t, authClientLive(t, ctx, ti.conn, offered[0]),
			"the client row survives the retired link")
		require.Equal(t, offered, offeredAuthClients(t, ctx, ti.conn, second),
			"the other server on the same client keeps what it offers")
	})

	t.Run("the only server's own binding cannot be detached on its own", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-assoc-own")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-own")
		clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-assoc-own")
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)

		err := ti.service.DetachServerClientAssociation(ctx, &gen.DetachServerClientAssociationPayload{
			McpServerID:           serverID.String(),
			RemoteSessionClientID: clientID.String(),
		})
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeConflict, shareable.Code)
		require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, projectID, serverID))
	})

	t.Run("unbinding a client from the issuer removes its associations", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-assoc-unbind")
		first := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-unbind-a")
		second := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-unbind-b")
		clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-assoc-unbind")
		associate(t, ctx, ti.conn, orgID, projectID, first, clientID)
		associate(t, ctx, ti.conn, orgID, projectID, second, clientID)

		_, err := repo.New(ti.conn).DetachProjectRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachProjectRemoteSessionClientFromUserSessionIssuerParams{
			RemoteSessionClientID: clientID,
			UserSessionIssuerID:   userIssuerID,
			ProjectID:             projectID,
			OrganizationID:        orgID,
		})
		require.NoError(t, err)
		resync(t, ctx, ti.conn, orgID, projectID, userIssuerID)

		require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, first), "the current serving path cannot reach an unbound client")
		require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, second))
	})

	t.Run("a deleted client's associations are removed in every project", func(t *testing.T) {
		t.Parallel()

		otherProjectID := createProject(t, ctx, ti.conn, "assoc-orgclient-project")
		ownIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-assoc-orgclient-a")
		otherIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProjectID, "usi-assoc-orgclient-b")
		ownServer := seedServerOnIssuer(t, ctx, ti.conn, projectID, ownIssuerID, "assoc-orgclient-a")
		otherServer := seedServerOnIssuer(t, ctx, ti.conn, otherProjectID, otherIssuerID, "assoc-orgclient-b")

		remoteIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, orgID, "rsi-assoc-orgclient")
		clientID := seedOrgLevelRemoteClient(t, ctx, ti.conn, orgID, remoteIssuerID, "cid-assoc-orgclient", ownIssuerID, otherIssuerID)
		resync(t, ctx, ti.conn, orgID, projectID, ownIssuerID)
		resync(t, ctx, ti.conn, orgID, otherProjectID, otherIssuerID)
		require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, projectID, ownServer))
		require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, otherProjectID, otherServer))

		require.NoError(t, ti.service.DeleteClient(ctx, &orgclientsgen.DeleteClientPayload{ID: clientID.String(), SessionToken: nil, ApikeyToken: nil}))

		require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, ownServer))
		require.Empty(t, associatedClients(t, ctx, ti.conn, otherProjectID, otherServer))
	})

	t.Run("an association never crosses a tenant", func(t *testing.T) {
		t.Parallel()

		userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-assoc-tenant")
		serverID := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "assoc-tenant")

		otherOrgID := createOrganization(t, ctx, ti.conn, "assoc-tenant-other")
		otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
			Name:           "assoc-tenant-other",
			Slug:           "assoc-tenant-other",
			OrganizationID: otherOrgID,
		})
		require.NoError(t, err)
		foreignIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProject.ID, "usi-assoc-tenant-foreign")
		foreignClientID, _ := seedActiveClient(t, ctx, ti.conn, otherProject.ID, foreignIssuerID, otherOrgID, "rsi-assoc-tenant-foreign")

		require.Zero(t, associate(t, ctx, ti.conn, orgID, projectID, serverID, foreignClientID), "another organization's client must not attach")
		require.Zero(t, associate(t, ctx, ti.conn, otherOrgID, projectID, serverID, foreignClientID), "a foreign caller must not reach this server")
		require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, serverID))

		// The schema refuses the same row even when application checks are
		// bypassed: the client's scope must be the server's project,
		// organization, or the platform.
		require.Error(t, testrepo.New(ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
			ProjectID:             projectID,
			OrganizationID:        orgID,
			McpServerID:           serverID,
			RemoteSessionClientID: foreignClientID,
		}))
	})
}

func TestCommitServerIdentityConfigurationAssociatesOnlyTheTargetServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "assoc-commit-target")
	siblingID := createServerIdentityTargetOnIssuer(t, ctx, ti, "assoc-commit-sibling", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "assoc-commit-provider", "", false, []string{"client_secret_basic"})
	clientID := createServerIdentityClient(t, ctx, ti, "assoc-commit-client", providerID, false)

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(targetID, providerID, clientID))
	require.NoError(t, err)

	require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, projectID, targetID))
	require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, siblingID), "configuring one server must not associate the issuer's other servers")

	_, err = ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(targetID, providerID, clientID))
	require.NoError(t, err, "a repeated commit is idempotent")
	require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, projectID, targetID))
}

// TestDetachServerClientAssociation_EnforcedOrganization proves the older path
// still works for an organization served by typed auth clients, and that both
// models move: the write is not refused, and its link goes with it.
func TestDetachServerClientAssociation_EnforcedOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	enforceUpstreamAuth(t, ctx, ti.conn, orgID)

	userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-enforced")
	first := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "enforced-a")
	second := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "enforced-b")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-enforced")
	associate(t, ctx, ti.conn, orgID, projectID, first, clientID)
	associate(t, ctx, ti.conn, orgID, projectID, second, clientID)
	recordTypedClients(t, ctx, ti.conn, orgID, first, second)
	require.NotEmpty(t, offeredAuthClients(t, ctx, ti.conn, first))

	require.NoError(t, ti.service.DetachServerClientAssociation(ctx, &gen.DetachServerClientAssociationPayload{
		McpServerID:           first.String(),
		RemoteSessionClientID: clientID.String(),
	}), "an enforced organization is not refused the older path")

	require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, first))
	require.Empty(t, offeredAuthClients(t, ctx, ti.conn, first),
		"both models move together while enforced")
	require.NotEmpty(t, offeredAuthClients(t, ctx, ti.conn, second))
}

// TestDetachServerClientAssociation_KeepsAdministratorClients proves the sync
// never reaches records an administrator owns: a client and link they created
// survive a legacy detach of the registration-derived ones.
func TestDetachServerClientAssociation_KeepsAdministratorClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-adminkeep")
	first := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "adminkeep-a")
	second := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "adminkeep-b")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-adminkeep")
	associate(t, ctx, ti.conn, orgID, projectID, first, clientID)
	associate(t, ctx, ti.conn, orgID, projectID, second, clientID)
	recordTypedClients(t, ctx, ti.conn, orgID, first, second)

	adminClientID := seedAdministratorAuthClient(t, ctx, ti.conn, orgID, projectID, first)
	require.Contains(t, offeredAuthClients(t, ctx, ti.conn, first), adminClientID)

	require.NoError(t, ti.service.DetachServerClientAssociation(ctx, &gen.DetachServerClientAssociationPayload{
		McpServerID:           first.String(),
		RemoteSessionClientID: clientID.String(),
	}))

	require.Equal(t, []uuid.UUID{adminClientID}, offeredAuthClients(t, ctx, ti.conn, first),
		"only the link derived from the association is retired")
}

// TestLegacyRemovalAndTypedSyncRollBackTogether proves the two writes are
// mutually revertible: the legacy association removal and the typed link
// retirement share one transaction, so a failure after either leaves both
// models as they were. It drives the pair directly and abandons the
// transaction, which is what any error between them does to the handler that
// owns it.
func TestLegacyRemovalAndTypedSyncRollBackTogether(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	userIssuerID := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "usi-atomic")
	first := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "atomic-a")
	second := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuerID, "atomic-b")
	clientID, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuerID, orgID, "rsi-atomic")
	associate(t, ctx, ti.conn, orgID, projectID, first, clientID)
	associate(t, ctx, ti.conn, orgID, projectID, second, clientID)
	recordTypedClients(t, ctx, ti.conn, orgID, first, second)

	associatedBefore := associatedClients(t, ctx, ti.conn, projectID, first)
	offeredBefore := offeredAuthClients(t, ctx, ti.conn, first)
	require.NotEmpty(t, associatedBefore)
	require.NotEmpty(t, offeredBefore)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	removed, err := upstreamclientsrepo.New(tx).DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
		McpServerID:           first,
		RemoteSessionClientID: clientID,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)
	require.NoError(t, authclientbackfill.SyncServerAssociations(ctx, tx, orgID, first))
	// Whatever fails after this point — the audit write, the commit — takes
	// both writes with it.
	require.NoError(t, tx.Rollback(ctx))

	require.Equal(t, associatedBefore, associatedClients(t, ctx, ti.conn, projectID, first),
		"the legacy association is restored by the rollback")
	require.Equal(t, offeredBefore, offeredAuthClients(t, ctx, ti.conn, first),
		"the typed link is restored by the same rollback")
}

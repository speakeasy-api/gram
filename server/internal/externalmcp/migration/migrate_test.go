package migration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRunMigratesServerOntoItsOwnIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "templated", remoteURL: "https://mcp.example.com/v1/mcp?{TOOLSETS}", headers: nil, noIssuer: false, noWrapper: false})

	endpointsBefore, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: ti.projectID, McpServerID: server.toolset.ID})
	require.NoError(t, err)
	require.Len(t, endpointsBefore, 1)

	pluginServerID, err := repo.New(ti.conn).SeedPluginServerFixture(ctx, repo.SeedPluginServerFixtureParams{
		ProjectID: conv.ToNullUUID(ti.projectID), ToolsetID: conv.ToNullUUID(server.toolset.ID), Name: "support-tools", OrganizationID: ti.organizationID,
	})
	require.NoError(t, err)

	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: ti.organizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeRole, "support"), Scope: "mcp:connect",
		Selectors: []byte(`{"resource_kind":"mcp","resource_id":"` + server.toolset.ID.String() + `"}`),
	})
	require.NoError(t, err)

	sessionID, err := repo.New(ti.conn).SeedRemoteSessionFixture(ctx, repo.SeedRemoteSessionFixtureParams{
		SubjectUrn: urn.NewUserSubject("person"), UserSessionIssuerID: server.issuerID, RemoteSessionClientID: server.clientID, Resource: pgtype.Text{},
	})
	require.NoError(t, err)

	before := countIssuersAndClients(t, ctx, ti)
	migrationsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetMigrateToRemoteMcp)
	require.NoError(t, err)

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{{ToolsetID: server.toolset.ID, TargetMcpServerID: uuid.NullUUID{}}}, false)
	require.Len(t, outcomes, 1)
	outcome := outcomes[0]
	require.Equal(t, migration.StatusMigrated, outcome.Status, outcome.Reason)
	require.Equal(t, "https://mcp.example.com/v1/mcp", outcome.RemoteURL)

	// The replacement signs in through the toolset's own issuer, and no
	// issuer, client or client link was created.
	replacement, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: outcome.McpServerID, OrganizationID: ti.organizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(server.issuerID), replacement.UserSessionIssuerID)
	require.True(t, replacement.RemoteMcpServerID.Valid)
	require.True(t, replacement.RemoteSessionIssuerID.Valid)
	require.Equal(t, before, countIssuersAndClients(t, ctx, ti))

	// The address keeps its endpoint row and now serves the replacement.
	require.Equal(t, []uuid.UUID{endpointsBefore[0].ID}, outcome.EndpointIDs)
	endpointsAfter, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: ti.projectID, McpServerID: outcome.McpServerID})
	require.NoError(t, err)
	require.Len(t, endpointsAfter, 1)
	require.Equal(t, endpointsBefore[0].ID, endpointsAfter[0].ID)
	require.Equal(t, "templated-mcp", endpointsAfter[0].Slug)

	_, err = toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: server.toolset.ID, ProjectID: ti.projectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = mcpserversrepo.New(ti.conn).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: server.toolset.ID, OrganizationID: ti.organizationID})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	pluginServer, err := repo.New(ti.conn).GetPluginServerFixture(ctx, pluginServerID)
	require.NoError(t, err)
	require.False(t, pluginServer.Deleted)
	require.False(t, pluginServer.ToolsetID.Valid)
	require.Equal(t, conv.ToNullUUID(outcome.McpServerID), pluginServer.McpServerID)

	grants, err := repo.New(ti.conn).ListGrantResourceIDsFixture(ctx, ti.organizationID)
	require.NoError(t, err)
	require.Contains(t, grants, repo.ListGrantResourceIDsFixtureRow{ResourceID: outcome.McpServerID.String(), Scope: "mcp:connect"})

	resource, err := repo.New(ti.conn).GetRemoteSessionResourceFixture(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, conv.ToPGText("https://mcp.example.com/v1/mcp"), resource)

	migrationsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetMigrateToRemoteMcp)
	require.NoError(t, err)
	require.Equal(t, migrationsBefore+1, migrationsAfter)

	candidates, err := ti.migrator.ListCandidates(ctx)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestRunGivesLegacyAddressAnEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "legacy", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: false, noWrapper: true})

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{{ToolsetID: server.toolset.ID, TargetMcpServerID: uuid.NullUUID{}}}, false)
	require.Equal(t, migration.StatusMigrated, outcomes[0].Status, outcomes[0].Reason)

	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: ti.projectID, McpServerID: outcomes[0].McpServerID})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.Equal(t, "legacy-mcp", endpoints[0].Slug)
}

func TestRunMergesIntoExistingServerOnTheSameIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "merged", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: false, noWrapper: false})

	existing, remoteID := seedRemoteServer(t, ctx, ti, server.issuerID)

	candidates, err := ti.migrator.ListCandidates(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Len(t, candidates[0].Targets, 1)
	require.Equal(t, existing.ID, candidates[0].Targets[0].ID)

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{{ToolsetID: server.toolset.ID, TargetMcpServerID: conv.ToNullUUID(existing.ID)}}, false)
	require.Equal(t, migration.StatusMigrated, outcomes[0].Status, outcomes[0].Reason)
	require.True(t, outcomes[0].MergedIntoExisting)
	require.Equal(t, existing.ID, outcomes[0].McpServerID)
	require.Equal(t, remoteID, outcomes[0].RemoteMcpServerID)

	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: ti.projectID, McpServerID: existing.ID})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.Equal(t, "merged-mcp", endpoints[0].Slug)
}

func TestRunRefusesServerWithoutIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "no-issuer", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: true, noWrapper: false})
	before := countIssuersAndClients(t, ctx, ti)

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{{ToolsetID: server.toolset.ID, TargetMcpServerID: uuid.NullUUID{}}}, false)
	require.Equal(t, migration.StatusFailed, outcomes[0].Status)
	require.Contains(t, outcomes[0].Reason, "no sign-in provider")
	require.Equal(t, before, countIssuersAndClients(t, ctx, ti))

	_, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: server.toolset.ID, ProjectID: ti.projectID})
	require.NoError(t, err)
}

func TestRunRefusesServerWithHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "headers", remoteURL: "https://mcp.example.com/mcp", headers: []byte(`[{"name":"Authorization","isSecret":true,"isRequired":true}]`), noIssuer: false, noWrapper: false})

	candidates, err := ti.migrator.ListCandidates(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.NotEmpty(t, candidates[0].Blockers)

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{{ToolsetID: server.toolset.ID, TargetMcpServerID: uuid.NullUUID{}}}, false)
	require.Equal(t, migration.StatusFailed, outcomes[0].Status)
	require.Contains(t, outcomes[0].Reason, "headers")
}

func TestPreviewRollsBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "preview", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: false, noWrapper: false})
	migrationsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetMigrateToRemoteMcp)
	require.NoError(t, err)

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{{ToolsetID: server.toolset.ID, TargetMcpServerID: uuid.NullUUID{}}}, true)
	require.Equal(t, migration.StatusPreviewed, outcomes[0].Status, outcomes[0].Reason)
	require.NotEmpty(t, outcomes[0].Actions)

	_, err = toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: server.toolset.ID, ProjectID: ti.projectID})
	require.NoError(t, err)
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: ti.projectID, McpServerID: server.toolset.ID})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	migrationsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetMigrateToRemoteMcp)
	require.NoError(t, err)
	require.Equal(t, migrationsBefore, migrationsAfter)
}

func TestRunStopsAtFirstFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	blocked := seedServer(t, ctx, ti, serverOptions{slug: "blocked", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: true, noWrapper: false})
	ready := seedServer(t, ctx, ti, serverOptions{slug: "ready", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: false, noWrapper: false})

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{
		{ToolsetID: blocked.toolset.ID, TargetMcpServerID: uuid.NullUUID{}},
		{ToolsetID: ready.toolset.ID, TargetMcpServerID: uuid.NullUUID{}},
	}, false)
	require.Len(t, outcomes, 2)
	require.Equal(t, migration.StatusFailed, outcomes[0].Status)
	require.Equal(t, migration.StatusNotAttempted, outcomes[1].Status)

	_, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: ready.toolset.ID, ProjectID: ti.projectID})
	require.NoError(t, err)
}

func TestRunRefusesMergeIntoPrivateTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "private-target", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: false, noWrapper: false})
	existing, _ := seedRemoteServer(t, ctx, ti, server.issuerID)
	require.NoError(t, repo.New(ti.conn).SetNetworkAccessModeFixture(ctx, repo.SetNetworkAccessModeFixtureParams{
		NetworkAccessMode: conv.ToPGText(string(networkaccess.ModePrivateOnly)), ID: existing.ID,
	}))

	candidates, err := ti.migrator.ListCandidates(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Empty(t, candidates[0].Targets)

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{{ToolsetID: server.toolset.ID, TargetMcpServerID: conv.ToNullUUID(existing.ID)}}, false)
	require.Equal(t, migration.StatusFailed, outcomes[0].Status)
	require.Contains(t, outcomes[0].Reason, "public-only")
}

func TestListCandidatesBlocksPrivateWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "private-wrapper", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: false, noWrapper: false})
	require.NoError(t, repo.New(ti.conn).SetNetworkAccessModeFixture(ctx, repo.SetNetworkAccessModeFixtureParams{
		NetworkAccessMode: conv.ToPGText(string(networkaccess.ModePrivateOnly)), ID: server.toolset.ID,
	}))

	candidates, err := ti.migrator.ListCandidates(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Contains(t, strings.Join(candidates[0].Blockers, "; "), "private network")
}

func TestListCandidatesBlocksInvalidURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	seedServer(t, ctx, ti, serverOptions{slug: "hostless", remoteURL: "https:///mcp", headers: nil, noIssuer: false, noWrapper: false})

	candidates, err := ti.migrator.ListCandidates(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Contains(t, candidates[0].Blockers, migration.ErrInvalidURL.Error())
}

func TestRunDeduplicatesCollectionAttachments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "collections", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: false, noWrapper: false})
	existing, _ := seedRemoteServer(t, ctx, ti, server.issuerID)
	queries := repo.New(ti.conn)

	// One collection holds both the toolset-keyed and the wrapper-keyed
	// attachment; another already carries the merge target.
	twins, err := queries.SeedCollectionFixture(ctx, repo.SeedCollectionFixtureParams{OrganizationID: ti.organizationID, Slug: "twins"})
	require.NoError(t, err)
	carrying, err := queries.SeedCollectionFixture(ctx, repo.SeedCollectionFixtureParams{OrganizationID: ti.organizationID, Slug: "carrying"})
	require.NoError(t, err)
	for _, seed := range []repo.SeedCollectionAttachmentFixtureParams{
		{CollectionID: twins, ToolsetID: conv.ToNullUUID(server.toolset.ID), McpServerID: uuid.NullUUID{}},
		{CollectionID: twins, ToolsetID: uuid.NullUUID{}, McpServerID: conv.ToNullUUID(server.toolset.ID)},
		{CollectionID: carrying, ToolsetID: conv.ToNullUUID(server.toolset.ID), McpServerID: uuid.NullUUID{}},
		{CollectionID: carrying, ToolsetID: uuid.NullUUID{}, McpServerID: conv.ToNullUUID(existing.ID)},
	} {
		_, err := queries.SeedCollectionAttachmentFixture(ctx, seed)
		require.NoError(t, err)
	}

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{{ToolsetID: server.toolset.ID, TargetMcpServerID: conv.ToNullUUID(existing.ID)}}, false)
	require.Equal(t, migration.StatusMigrated, outcomes[0].Status, outcomes[0].Reason)

	for _, collectionID := range []uuid.UUID{twins, carrying} {
		attachments, err := queries.ListCollectionAttachmentsFixture(ctx, collectionID)
		require.NoError(t, err)
		live := make([]repo.ListCollectionAttachmentsFixtureRow, 0, len(attachments))
		for _, attachment := range attachments {
			if !attachment.Deleted {
				live = append(live, attachment)
			}
		}
		require.Len(t, live, 1)
		require.Equal(t, conv.ToNullUUID(existing.ID), live[0].McpServerID)
	}
}

func TestRunMovesDistributionOffRemovedPluginEntry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "distributed", remoteURL: "https://mcp.example.com/mcp", headers: nil, noIssuer: false, noWrapper: false})
	existing, _ := seedRemoteServer(t, ctx, ti, server.issuerID)
	queries := repo.New(ti.conn)

	// The plugin already carries the merge target, so the old server's
	// distributed entry is removed and its distribution must follow.
	oldEntry, err := queries.SeedPluginServerFixture(ctx, repo.SeedPluginServerFixtureParams{
		ProjectID: conv.ToNullUUID(ti.projectID), ToolsetID: conv.ToNullUUID(server.toolset.ID), Name: "support-tools", OrganizationID: ti.organizationID,
	})
	require.NoError(t, err)
	pluginID, err := queries.GetPluginIDFixture(ctx, oldEntry)
	require.NoError(t, err)
	keptEntry, err := queries.SeedPluginServerForServerFixture(ctx, repo.SeedPluginServerForServerFixtureParams{
		PluginID: pluginID, ProjectID: conv.ToNullUUID(ti.projectID), McpServerID: conv.ToNullUUID(existing.ID), DisplayName: "Existing",
	})
	require.NoError(t, err)
	distributionID, err := queries.SeedDistributionFixture(ctx, repo.SeedDistributionFixtureParams{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, PluginID: pluginID,
		PluginServerID: conv.ToNullUUID(oldEntry), CatalogReference: "distributed",
	})
	require.NoError(t, err)

	outcomes := ti.migrator.Run(ctx, testActor, []migration.Request{{ToolsetID: server.toolset.ID, TargetMcpServerID: conv.ToNullUUID(existing.ID)}}, false)
	require.Equal(t, migration.StatusMigrated, outcomes[0].Status, outcomes[0].Reason)

	removed, err := queries.GetPluginServerFixture(ctx, oldEntry)
	require.NoError(t, err)
	require.True(t, removed.Deleted)
	pluginServerID, err := queries.GetDistributionPluginServerFixture(ctx, distributionID)
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(keptEntry), pluginServerID)
}

// seedRemoteServer creates a public-only remote-backed server on the issuer,
// returning it and its remote source id.
func seedRemoteServer(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) (mcpserversrepo.McpServer, uuid.UUID) {
	t.Helper()

	tx := testenv.BeginTx(t, ctx, ti.conn)
	remote, err := remotemcp.CreateRemoteMCPSourceInTransaction(ctx, tx, audit.NewLogger(), remotemcp.RemoteMCPSourceTransactionInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, ActorUserID: "seed", ActorEmail: nil,
		Name: new("Existing"), URL: "https://mcp.example.com/mcp", TransportType: "streamable-http",
	})
	require.NoError(t, err)
	existing, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, ActorUserID: "seed", ActorEmail: nil,
		Name: "Existing", Visibility: mcpservers.VisibilityPrivate, NetworkAccessMode: networkaccess.ModePublicOnly,
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: conv.ToNullUUID(issuerID),
		RemoteMCPServerID: conv.ToNullUUID(remote.ID), TunneledMCPServerID: uuid.NullUUID{}, ToolsetID: uuid.NullUUID{},
		UnproxiedMCPServerID: uuid.NullUUID{}, ToolVariationsGroupID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	return existing, remote.ID
}

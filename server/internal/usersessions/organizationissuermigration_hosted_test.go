package usersessions_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

func seedMigrationToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string, mcpSlug string, issuerID uuid.UUID) toolsetsrepo.Toolset {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolsets := toolsetsrepo.New(ti.conn)
	toolset, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Name: slug, Slug: slug, Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug: conv.ToPGText(mcpSlug), McpEnabled: true,
	})
	require.NoError(t, err)
	toolset, err = toolsets.UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: conv.ToNullUUID(issuerID), Slug: toolset.Slug, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	return toolset
}

func syncMigrationWrapper(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsetsrepo.Toolset) {
	t.Helper()
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := hostedmcp.Sync(ctx, tx, audit.NewLogger(), hostedmcp.SystemActor("test"), toolset, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

func TestOrganizationUserSessionIssuerMigrateSyncsHostedServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	sourceID := seedIssuer(t, ctx, ti, "hosted-migrate-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "hosted-migrate-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	targetID := uuid.MustParse(target.ID)

	hosted := seedMigrationToolset(t, ctx, ti, "hosted-migrate", "hosted-migrate-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, hosted)
	servers := mcpserversrepo.New(ti.conn)
	endpoints := mcpendpointsrepo.New(ti.conn)
	endpointsBefore, err := endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: hosted.ID})
	require.NoError(t, err)
	require.Len(t, endpointsBefore, 1)
	second := seedMigrationToolset(t, ctx, ti, "hosted-migrate-second", "hosted-migrate-second-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, second)

	// Drifted wrappers keep their name and URL; only the issuer moves, through the bulk path.
	renamed := seedMigrationToolset(t, ctx, ti, "renamed-migrate", "renamed-migrate-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, renamed)
	_, err = servers.SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: conv.ToPGText("Stale wrapper name"), Slug: renamed.McpSlug, Visibility: mcpservers.VisibilityPrivate,
		UserSessionIssuerID: conv.ToNullUUID(sourceID), ToolVariationsGroupID: uuid.NullUUID{}, NetworkAccessMode: pgtype.Text{},
		ID: renamed.ID, ProjectID: projectID,
	})
	require.NoError(t, err)
	moved := seedMigrationToolset(t, ctx, ti, "moved-migrate", "moved-migrate-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, moved)
	movedEndpoints, err := endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: moved.ID})
	require.NoError(t, err)
	require.Len(t, movedEndpoints, 1)
	_, err = endpoints.UpdateMCPEndpointAddress(ctx, mcpendpointsrepo.UpdateMCPEndpointAddressParams{
		CustomDomainID: uuid.NullUUID{}, Slug: "moved-migrate-drifted", IsDomainRoot: pgtype.Bool{}, ID: movedEndpoints[0].ID, ProjectID: projectID,
	})
	require.NoError(t, err)

	// A wrapper on the source whose toolset is elsewhere keeps the bulk path.
	otherID := seedIssuer(t, ctx, ti, "hosted-migrate-other")
	elsewhere := seedMigrationToolset(t, ctx, ti, "elsewhere-migrate", "elsewhere-migrate-mcp", otherID)
	syncMigrationWrapper(t, ctx, ti, elsewhere)
	_, err = servers.SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: conv.ToPGText(elsewhere.Name), Slug: elsewhere.McpSlug, Visibility: mcpservers.VisibilityPrivate,
		UserSessionIssuerID: conv.ToNullUUID(sourceID), ToolVariationsGroupID: uuid.NullUUID{}, NetworkAccessMode: pgtype.Text{},
		ID: elsewhere.ID, ProjectID: projectID,
	})
	require.NoError(t, err)
	// A toolset on the source whose wrapper is on another issuer: the bulk update never moved that wrapper.
	unrelated := seedMigrationToolset(t, ctx, ti, "unrelated-migrate", "unrelated-migrate-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, unrelated)
	_, err = servers.SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: conv.ToPGText(unrelated.Name), Slug: unrelated.McpSlug, Visibility: mcpservers.VisibilityPrivate,
		UserSessionIssuerID: conv.ToNullUUID(otherID), ToolVariationsGroupID: uuid.NullUUID{}, NetworkAccessMode: pgtype.Text{},
		ID: unrelated.ID, ProjectID: projectID,
	})
	require.NoError(t, err)

	// Old row shapes keep today's bulk handling.
	legacy := seedMigrationToolset(t, ctx, ti, "legacy-migrate", "legacy-migrate-mcp", sourceID)
	deletedToolset := seedMigrationToolset(t, ctx, ti, "deleted-migrate", "deleted-migrate-mcp", sourceID)
	_, err = toolsetsrepo.New(ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: deletedToolset.Slug, ProjectID: projectID})
	require.NoError(t, err)
	freeID := uuid.New()
	_, err = servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: freeID, ProjectID: projectID, Name: conv.ToPGText("Free server"), Slug: conv.ToPGText("free-migrate-server"),
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: conv.ToNullUUID(sourceID), RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID: conv.ToNullUUID(legacy.ID), ToolVariationsGroupID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	updatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)

	hostedAfter, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: hosted.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), hostedAfter.UserSessionIssuerID)
	wrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: hosted.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), wrapper.UserSessionIssuerID)
	require.Equal(t, hosted.Name, wrapper.Name.String)
	renamedWrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: renamed.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), renamedWrapper.UserSessionIssuerID)
	require.Equal(t, "Stale wrapper name", renamedWrapper.Name.String)
	movedWrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: moved.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), movedWrapper.UserSessionIssuerID)
	movedEndpointsAfter, err := endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: moved.ID})
	require.NoError(t, err)
	require.Len(t, movedEndpointsAfter, 1)
	require.Equal(t, movedEndpoints[0].ID, movedEndpointsAfter[0].ID)
	require.Equal(t, "moved-migrate-drifted", movedEndpointsAfter[0].Slug)
	movedToolset, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: moved.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), movedToolset.UserSessionIssuerID)
	endpointsAfter, err := endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: hosted.ID})
	require.NoError(t, err)
	require.Len(t, endpointsAfter, 1)
	require.Equal(t, endpointsBefore[0].ID, endpointsAfter[0].ID)
	require.Equal(t, endpointsBefore[0].Slug, endpointsAfter[0].Slug)

	updatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, updatesBefore+2, updatesAfter)
	secondWrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: second.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), secondWrapper.UserSessionIssuerID)

	unrelatedToolset, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: unrelated.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), unrelatedToolset.UserSessionIssuerID)
	unrelatedWrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: unrelated.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(otherID), unrelatedWrapper.UserSessionIssuerID)

	elsewhereToolset, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: elsewhere.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(otherID), elsewhereToolset.UserSessionIssuerID)
	elsewhereWrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: elsewhere.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), elsewhereWrapper.UserSessionIssuerID)

	legacyAfter, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: legacy.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), legacyAfter.UserSessionIssuerID)
	_, err = servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: legacy.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	deletedAfter, err := toolsetsrepo.New(ti.conn).GetToolsetByIDIncludingDeletedFixture(ctx, toolsetsrepo.GetToolsetByIDIncludingDeletedFixtureParams{ID: deletedToolset.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), deletedAfter.UserSessionIssuerID)
	freeAfter, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: freeID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), freeAfter.UserSessionIssuerID)
}

func TestOrganizationUserSessionIssuerMigrateSkipsMultiEndpointHostedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	sourceID := seedIssuer(t, ctx, ti, "conflict-migrate-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "conflict-migrate-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	targetID := uuid.MustParse(target.ID)

	hosted := seedMigrationToolset(t, ctx, ti, "conflict-migrate", "conflict-migrate-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, hosted)
	healthy := seedMigrationToolset(t, ctx, ti, "healthy-migrate", "healthy-migrate-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, healthy)
	servers := mcpserversrepo.New(ti.conn)
	// Drift plus a second endpoint, a state toolset saves refuse to sync (the backfill leaves such wrappers blocked).
	_, err = servers.SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: conv.ToPGText("Stale wrapper name"), Slug: hosted.McpSlug, Visibility: mcpservers.VisibilityPrivate,
		UserSessionIssuerID: conv.ToNullUUID(sourceID), ToolVariationsGroupID: uuid.NullUUID{}, NetworkAccessMode: pgtype.Text{},
		ID: hosted.ID, ProjectID: projectID,
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, CustomDomainID: uuid.NullUUID{}, McpServerID: conv.ToNullUUID(hosted.ID), MetaMcpServerID: uuid.NullUUID{}, Slug: "conflict-migrate-extra",
	})
	require.NoError(t, err)
	updatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)

	hostedAfter, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: hosted.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), hostedAfter.UserSessionIssuerID)
	wrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: hosted.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), wrapper.UserSessionIssuerID)
	require.Equal(t, "Stale wrapper name", wrapper.Name.String)
	endpointsAfter, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: hosted.ID})
	require.NoError(t, err)
	require.Len(t, endpointsAfter, 2)
	updatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, updatesBefore+1, updatesAfter)
	updateAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, healthy.ID.String(), updateAudit.SubjectID)
	healthyWrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: healthy.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), healthyWrapper.UserSessionIssuerID)
}

func TestOrganizationUserSessionIssuerMigrateFallsBackWhenHostedSyncRejects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	sourceID := seedIssuer(t, ctx, ti, "reject-migrate-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "reject-migrate-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	targetID := uuid.MustParse(target.ID)
	rejected := seedMigrationToolset(t, ctx, ti, "reject-migrate", "reject-migrate-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, rejected)
	accepted := seedMigrationToolset(t, ctx, ti, "accept-migrate", "accept-migrate-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, accepted)

	ti.service.SetHostedSync(func(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor hostedmcp.Actor, toolset toolsetsrepo.Toolset) ([]uuid.UUID, error) {
		cleared, err := hostedmcp.SyncDeferringIssuerResync(ctx, tx, auditLogger, actor, toolset)
		if err == nil && toolset.ID == rejected.ID {
			return nil, errors.New("forced rejection")
		}
		return cleared, err //nolint:wrapcheck // passes hostedmcp errors through unchanged, as the production sync does.
	})
	updatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)

	servers := mcpserversrepo.New(ti.conn)
	for _, toolset := range []toolsetsrepo.Toolset{rejected, accepted} {
		toolsetAfter, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: toolset.ID, ProjectID: projectID})
		require.NoError(t, err)
		require.Equal(t, conv.ToNullUUID(targetID), toolsetAfter.UserSessionIssuerID)
		wrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: toolset.ID, OrganizationID: authCtx.ActiveOrganizationID})
		require.NoError(t, err)
		require.Equal(t, conv.ToNullUUID(targetID), wrapper.UserSessionIssuerID)
	}
	updatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, updatesBefore+1, updatesAfter)
	updateAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, accepted.ID.String(), updateAudit.SubjectID)
}

// migrateWithRemoteBinding migrates two hosted wrappers and a free server sharing a project and returns their remote issuers.
func migrateWithRemoteBinding(t *testing.T, syncWrapper usersessions.HostedSyncFunc) (uuid.UUID, []uuid.NullUUID) {
	t.Helper()
	ctx, ti := newTestService(t)
	if syncWrapper != nil {
		ti.service.SetHostedSync(syncWrapper)
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	sourceID := seedIssuer(t, ctx, ti, "resync-migrate-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "resync-migrate-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	remoteRepo := remotesessionsrepo.New(ti.conn)
	remoteIssuer, err := remoteRepo.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID: conv.ToNullUUID(projectID), OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug: "resync-migrate-remote", Issuer: "https://resync-migrate.example.com",
		AuthorizationEndpoint: conv.ToPGText("https://resync-migrate.example.com/authorize"), TokenEndpoint: conv.ToPGText("https://resync-migrate.example.com/token"),
		ScopesSupported: []string{"openid"}, GrantTypesSupported: []string{"authorization_code", "refresh_token"}, ResponseTypesSupported: []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"}, CodeChallengeMethodsSupported: []string{"S256"},
	})
	require.NoError(t, err)
	remoteClient, err := remoteRepo.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID: conv.ToNullUUID(projectID), OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID), RemoteSessionIssuerID: remoteIssuer.ID,
		ClientID: "resync-migrate-client", ClientIDIssuedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, remoteRepo.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: remoteClient.ID, UserSessionIssuerID: sourceID,
	}))

	first := seedMigrationToolset(t, ctx, ti, "resync-first", "resync-first-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, first)
	second := seedMigrationToolset(t, ctx, ti, "resync-second", "resync-second-mcp", sourceID)
	syncMigrationWrapper(t, ctx, ti, second)
	backing := seedMigrationToolset(t, ctx, ti, "resync-backing", "resync-backing-mcp", sourceID)
	freeID := uuid.New()
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: freeID, ProjectID: projectID, Name: conv.ToPGText("Resync free server"), Slug: conv.ToPGText("resync-free-server"),
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: conv.ToNullUUID(sourceID), RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID: conv.ToNullUUID(backing.ID), ToolVariationsGroupID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)

	remoteIssuers := make([]uuid.NullUUID, 0, 3)
	for _, id := range []uuid.UUID{first.ID, second.ID, freeID} {
		server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID})
		require.NoError(t, err)
		require.Equal(t, conv.ToNullUUID(uuid.MustParse(target.ID)), server.UserSessionIssuerID)
		remoteIssuers = append(remoteIssuers, server.RemoteSessionIssuerID)
	}
	return remoteIssuer.ID, remoteIssuers
}

func TestOrganizationUserSessionIssuerMigrateDeferredResyncMatchesPerWrapperResync(t *testing.T) {
	t.Parallel()

	remoteIssuerID, deferred := migrateWithRemoteBinding(t, nil)
	for _, got := range deferred {
		require.Equal(t, conv.ToNullUUID(remoteIssuerID), got)
	}
	perWrapperIssuerID, perWrapper := migrateWithRemoteBinding(t, func(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, actor hostedmcp.Actor, toolset toolsetsrepo.Toolset) ([]uuid.UUID, error) {
		return hostedmcp.Sync(ctx, tx, auditLogger, actor, toolset, nil)
	})
	for _, got := range perWrapper {
		require.Equal(t, conv.ToNullUUID(perWrapperIssuerID), got)
	}
}

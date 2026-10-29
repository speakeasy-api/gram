package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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
)

// seedMigrationToolset creates a toolset hosted by a private canonical server with a platform endpoint at mcpSlug, on issuerID.
func seedMigrationToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string, mcpSlug string, issuerID uuid.UUID) toolsetsrepo.Toolset {
	t.Helper()
	toolset := seedMigrationBackingToolset(t, ctx, ti, slug)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := hostedmcp.CreateCanonical(ctx, tx, audit.NewLogger(), hostedmcp.SystemActor("test"), hostedmcp.CreateCanonicalInput{
		Toolset: toolset, Slug: mcpSlug, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	issuer := conv.ToNullUUID(issuerID)
	testenv.SetHostedMCP(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, testenv.HostedMCP{Visibility: "", UserSessionIssuerID: &issuer, Slug: "", CustomDomainID: uuid.NullUUID{}})
	return toolset
}

func seedMigrationBackingToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string) toolsetsrepo.Toolset {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Name: slug, Slug: slug, Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{},
	})
	require.NoError(t, err)
	return toolset
}

// Hosted servers move with the bulk server update; readers take the issuer from the canonical server.
func TestOrganizationUserSessionIssuerMigrateMovesHostedWrappers(t *testing.T) {
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
	endpoints := mcpendpointsrepo.New(ti.conn)
	endpointsBefore, err := endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: hosted.ID})
	require.NoError(t, err)
	require.Len(t, endpointsBefore, 1)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)

	servers := mcpserversrepo.New(ti.conn)
	wrapper, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: hosted.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), wrapper.UserSessionIssuerID)
	hostedAfter, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: hosted.ID, ProjectID: projectID})
	require.NoError(t, err)
	effective, _, err := hostedmcp.LoadServerState(ctx, ti.conn, hostedAfter)
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), effective.UserSessionIssuerID, "readers take the issuer from the wrapper")
	endpointsAfter, err := endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: hosted.ID})
	require.NoError(t, err)
	require.Equal(t, endpointsBefore, endpointsAfter)
}

// migrateWithRemoteBinding migrates two hosted wrappers and a free server sharing a project and returns their remote issuers.
func migrateWithRemoteBinding(t *testing.T) (uuid.UUID, []uuid.NullUUID) {
	t.Helper()
	ctx, ti := newTestService(t)
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
	second := seedMigrationToolset(t, ctx, ti, "resync-second", "resync-second-mcp", sourceID)
	backing := seedMigrationBackingToolset(t, ctx, ti, "resync-backing")
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

func TestOrganizationUserSessionIssuerMigrateResyncsHostedWrapperRemoteIssuers(t *testing.T) {
	t.Parallel()

	remoteIssuerID, remoteIssuers := migrateWithRemoteBinding(t)
	for _, got := range remoteIssuers {
		require.Equal(t, conv.ToNullUUID(remoteIssuerID), got)
	}
}

// A deleted hosted server on the source issuer does not stop the migration; live consumers still move.
func TestOrganizationUserSessionIssuerMigrateSkipsDeletedHostedServerAudit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	sourceID := seedIssuer(t, ctx, ti, "deleted-migrate-source")
	target, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: "deleted-migrate-target", AuthnChallengeMode: "chain", SessionDurationHours: 24})
	require.NoError(t, err)
	targetID := uuid.MustParse(target.ID)

	deleted := seedMigrationToolset(t, ctx, ti, "deleted-migrate", "deleted-migrate-mcp", sourceID)
	_, err = mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: deleted.ID, ProjectID: projectID})
	require.NoError(t, err)
	live := seedMigrationToolset(t, ctx, ti, "live-migrate", "live-migrate-mcp", sourceID)
	backing := seedMigrationBackingToolset(t, ctx, ti, "free-migrate-backing")
	freeID := uuid.New()
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: freeID, ProjectID: projectID, Name: conv.ToPGText("Free migrate server"), Slug: conv.ToPGText("free-migrate-server"),
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: conv.ToNullUUID(sourceID), RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID: conv.ToNullUUID(backing.ID), ToolVariationsGroupID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	updatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)

	_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: target.ID})
	require.NoError(t, err)

	updatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, updatesBefore+1, updatesAfter, "only the live canonical hosted server's move is audited")
	free, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: freeID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), free.UserSessionIssuerID, "a non-canonical server still moves")
	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpServerUpdate, live.ID.String())
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, sourceID.String(), before["UserSessionIssuerID"])
	require.Equal(t, targetID.String(), after["UserSessionIssuerID"])

	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: live.ID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(targetID), server.UserSessionIssuerID)
}

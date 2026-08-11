package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestDeleteMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "test mcp server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)
	issuerID := uuid.MustParse(*created.UserSessionIssuerID)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)
	beforeIssuerCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerDelete)
	require.NoError(t, err)

	err = ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
	afterIssuerCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeIssuerCount+1, afterIssuerCount)

	_, err = usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// Confirm subsequent get returns not-found.
	_, err = ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteMcpServer_DetachesFromPlugins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	backendID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "detach test server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &backendID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	attached, err := plugins.AttachToDefaultPlugin(ctx, tx, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ToolsetID:      uuid.NullUUID{},
		McpServerID:    uuid.NullUUID{UUID: uuid.MustParse(created.ID), Valid: true},
		DisplayName:    "detach test server",
	})
	require.NoError(t, err)
	require.NotNil(t, attached)
	require.NoError(t, tx.Commit(ctx))

	beforeRemoveCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)

	err = ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	servers, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, attached.PluginID)
	require.NoError(t, err)
	require.Empty(t, servers, "deleting the mcp server must soft-delete its plugin attachments")

	afterRemoveCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)
	require.Equal(t, beforeRemoveCount+1, afterRemoveCount)

	// The display name is free again: a replacement server with the same name
	// attaches under the original, un-suffixed name — the scenario where a
	// stale attachment from a deleted server blocked enabling its successor.
	replacementBackendID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	replacement, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "detach test server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &replacementBackendID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	tx2 := testenv.BeginTx(t, ctx, ti.conn)
	reattached, err := plugins.AttachToDefaultPlugin(ctx, tx2, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ToolsetID:      uuid.NullUUID{},
		McpServerID:    uuid.NullUUID{UUID: uuid.MustParse(replacement.ID), Valid: true},
		DisplayName:    "detach test server",
	})
	require.NoError(t, err)
	require.NotNil(t, reattached)
	require.NoError(t, tx2.Commit(ctx))
	require.Equal(t, "detach test server", reattached.Server.DisplayName)
}

func TestDeleteMcpServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteMcpServer_PreservesIssuerWithAnotherActiveOwner(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	firstBackendID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	first, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "first shared issuer server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &firstBackendID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, first.UserSessionIssuerID)
	sharedIssuerID := uuid.MustParse(*first.UserSessionIssuerID)

	secondBackendID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	second, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "second shared issuer server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &secondBackendID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	serverRepo := mcpserversrepo.New(ti.conn)
	secondRow, err := serverRepo.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        uuid.MustParse(second.ID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	_, err = serverRepo.UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		Name:                  secondRow.Name,
		Slug:                  secondRow.Slug,
		EnvironmentID:         secondRow.EnvironmentID,
		UserSessionIssuerID:   uuid.NullUUID{UUID: sharedIssuerID, Valid: true},
		RemoteMcpServerID:     secondRow.RemoteMcpServerID,
		TunneledMcpServerID:   secondRow.TunneledMcpServerID,
		ToolsetID:             secondRow.ToolsetID,
		ToolVariationsGroupID: secondRow.ToolVariationsGroupID,
		Visibility:            secondRow.Visibility,
		ID:                    secondRow.ID,
		ProjectID:             secondRow.ProjectID,
	})
	require.NoError(t, err)

	beforeIssuerCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerDelete)
	require.NoError(t, err)

	err = ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		ID:               first.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:        sharedIssuerID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	afterIssuerCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeIssuerCount, afterIssuerCount)
}

func TestDeleteMcpServer_CascadesSoftDeleteToSlugs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create a frontend and two slugs that both point at it.
	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	frontend, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "test mcp server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	frontendUUID := uuid.MustParse(frontend.ID)
	slugRepo := mcpendpointsrepo.New(ti.conn)
	for _, v := range []string{"-one", "-two"} {
		_, err := slugRepo.CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
			ProjectID:      *authCtx.ProjectID,
			CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			McpServerID:    frontendUUID,
			Slug:           authCtx.OrganizationSlug + v,
		})
		require.NoError(t, err)
	}

	beforeSlugDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)

	// Delete the frontend.
	err = ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		ID:               frontend.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// Both child slugs must now be absent from the active set.
	remaining, err := slugRepo.ListMCPEndpointsByProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	for _, s := range remaining {
		require.NotEqual(t, frontendUUID, s.McpServerID, "slug pointing at deleted frontend should have been soft-deleted")
	}

	// The cascade must produce one mcp-endpoint:delete audit event per child slug.
	afterSlugDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	require.Equal(t, beforeSlugDeletes+2, afterSlugDeletes)
}

func TestDeleteMcpServer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	err := ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

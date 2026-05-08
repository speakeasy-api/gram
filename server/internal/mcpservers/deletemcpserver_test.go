package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     &serverID,
		ToolsetID:             nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerDelete)
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

	// Confirm subsequent get returns not-found.
	_, err = ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
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

func TestDeleteMcpServer_CascadesSoftDeleteToSlugs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create a frontend and two slugs that both point at it.
	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	frontend, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     &serverID,
		ToolsetID:             nil,
		Visibility:            types.McpServerVisibility("disabled"),
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

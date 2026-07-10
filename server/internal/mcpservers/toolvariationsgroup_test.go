package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	variationsrepo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

// seedToolVariationsGroup creates the project-default tool variations group for
// the given project directly through the generated repo.
func seedToolVariationsGroup(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	groupID, err := variationsrepo.New(conn).InitGlobalToolVariationsGroup(ctx, variationsrepo.InitGlobalToolVariationsGroupParams{
		ProjectID:   projectID,
		Name:        "Global tool variations",
		Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return groupID
}

// seedOtherProjectToolVariationsGroup seeds a tool variations group under a
// different project in the caller's organization. Used to exercise
// cross-tenant rejection on tool_variations_group_id.
func seedOtherProjectToolVariationsGroup(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-" + uuid.New().String()[:8]
	otherProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return seedToolVariationsGroup(t, ctx, conn, otherProject.ID)
}

func TestUpdateMcpServer_SetsAndClearsToolVariationsGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ownServerID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "test mcp server",
		EnvironmentID:       nil,
		UserSessionIssuerID: &issuerID,
		RemoteMcpServerID:   &ownServerID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, created.ToolVariationsGroupID, "new servers default to disabled filtering")

	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID).String()

	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    created.ID,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &ownServerID,
		ToolsetID:             nil,
		ToolVariationsGroupID: &groupID,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.ToolVariationsGroupID)
	require.Equal(t, groupID, *updated.ToolVariationsGroupID)

	// Omitting the field clears it (full-record replace semantics).
	cleared, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    created.ID,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &ownServerID,
		ToolsetID:             nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Nil(t, cleared.ToolVariationsGroupID)
}

func TestCreateMcpServer_WithToolVariationsGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ownServerID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID).String()
	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "test mcp server",
		EnvironmentID:         nil,
		UserSessionIssuerID:   &issuerID,
		RemoteMcpServerID:     &ownServerID,
		ToolsetID:             nil,
		ToolVariationsGroupID: &groupID,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.ToolVariationsGroupID)
	require.Equal(t, groupID, *created.ToolVariationsGroupID)
}

func TestUpdateMcpServer_RejectsCrossTenantToolVariationsGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ownServerID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "test mcp server",
		EnvironmentID:       nil,
		UserSessionIssuerID: &issuerID,
		RemoteMcpServerID:   &ownServerID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	otherGroupID := seedOtherProjectToolVariationsGroup(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    created.ID,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &ownServerID,
		ToolsetID:             nil,
		ToolVariationsGroupID: &otherGroupID,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

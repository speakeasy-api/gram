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
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// seedEnvironment inserts an environment in the caller's project so it can be
// referenced by environment_id in mcp_servers create/update payloads.
func seedEnvironment(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) environmentsrepo.Environment {
	t.Helper()

	slug := "env-" + uuid.New().String()[:8]
	env, err := environmentsrepo.New(conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           slug,
		Slug:           slug,
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return env
}

// AIS-308: environment_id is inert at runtime for remote-backed servers (the
// remote serve path never reads it), so it is rejected at write-time. The
// column stays supported for toolset- and tunneled-backed servers.

func TestCreateMcpServer_RejectsEnvironmentWithRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	envID := seedEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID.String()

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "remote server with environment",
		EnvironmentID:     &envID,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpServer_RejectsEnvironmentWithRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "remote server without environment",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	envID := seedEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID.String()

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		ID:                created.ID,
		EnvironmentID:     &envID,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

// TestCreateMcpServer_ToolsetWithEnvironmentSucceeds guards backward
// compatibility: environment_id remains fully supported for toolset-backed
// servers, so this combination must still succeed.
func TestCreateMcpServer_ToolsetWithEnvironmentSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	toolsetID := toolset.ID.String()
	envID := seedEnvironment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID.String()

	result, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "toolset server with environment",
		EnvironmentID:     &envID,
		RemoteMcpServerID: nil,
		ToolsetID:         &toolsetID,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, result.EnvironmentID)
	require.Equal(t, envID, *result.EnvironmentID)
	require.NotNil(t, result.ToolsetID)
	require.Equal(t, toolsetID, *result.ToolsetID)
}

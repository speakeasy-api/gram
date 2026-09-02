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
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedHostedToolset inserts a live, addressable hosted toolset the way the
// legacy dashboard flow left them: enabled, private, platform slug.
func seedHostedToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) toolsetsrepo.Toolset {
	t.Helper()

	slug := "hosted-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	return toolset
}

func createHostedServer(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID, visibility string) *types.McpServer {
	t.Helper()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "hosted " + toolsetID.String()[:8],
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             conv.PtrEmpty(toolsetID.String()),
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility(visibility),
	})
	require.NoError(t, err)

	return created
}

func getToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, toolsetID uuid.UUID) toolsetsrepo.Toolset {
	t.Helper()

	toolset, err := toolsetsrepo.New(conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        toolsetID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	return toolset
}

func TestCreateMcpServer_ToolsetBacked_ProjectsVisibilityOntoToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	createHostedServer(t, ctx, ti, toolset.ID, "public")

	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.True(t, after.McpEnabled)
	require.True(t, after.McpIsPublic)
}

func TestCreateMcpServer_SecondWrapperForToolsetConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	createHostedServer(t, ctx, ti, toolset.ID, "private")

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "second wrapper",
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             conv.PtrEmpty(toolset.ID.String()),
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Contains(t, err.Error(), "toolset already has an mcp server")
}

func TestUpdateMcpServer_ToolsetBacked_ProjectsVisibilityOntoToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createHostedServer(t, ctx, ti, toolset.ID, "private")

	update := func(visibility string) {
		t.Helper()
		_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
			ID:                    created.ID,
			Name:                  nil,
			EnvironmentID:         nil,
			RemoteMcpServerID:     nil,
			TunneledMcpServerID:   nil,
			ToolsetID:             conv.PtrEmpty(toolset.ID.String()),
			UnproxiedMcpServerID:  nil,
			ToolVariationsGroupID: nil,
			Visibility:            types.McpServerVisibility(visibility),
		})
		require.NoError(t, err)
	}

	update("public")
	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.True(t, after.McpEnabled)
	require.True(t, after.McpIsPublic)

	// Disabling clears mcp_is_public: the four toolset states fold onto three.
	update("disabled")
	after = getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, after.McpEnabled)
	require.False(t, after.McpIsPublic)

	update("private")
	after = getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.True(t, after.McpEnabled)
	require.False(t, after.McpIsPublic)
}

func TestDeleteMcpServer_ToolsetBacked_ClearsToolsetHosting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createHostedServer(t, ctx, ti, toolset.ID, "private")
	serverID := uuid.MustParse(created.ID)

	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: serverID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            toolset.McpSlug.String,
	})
	require.NoError(t, err)

	endpointDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	serverDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	}))

	// The toolset survives as a build artifact; only its hosting columns go.
	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, after.McpSlug.Valid)
	require.False(t, after.McpEnabled)
	require.False(t, after.McpIsPublic)
	require.False(t, after.CustomDomainID.Valid)

	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: serverID,
	})
	require.NoError(t, err)
	require.Empty(t, endpoints)

	endpointDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	require.Equal(t, endpointDeletesBefore+1, endpointDeletesAfter)
	serverDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, serverDeletesBefore+1, serverDeletesAfter)
}

func TestUpdateMcpServer_RejectsBackingToolsetChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	other := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createHostedServer(t, ctx, ti, toolset.ID, "private")

	for name, payload := range map[string]gen.UpdateMcpServerPayload{
		"toolset":   {ToolsetID: conv.PtrEmpty(other.ID.String())},
		"remote":    {RemoteMcpServerID: conv.PtrEmpty(uuid.NewString())},
		"tunneled":  {TunneledMcpServerID: conv.PtrEmpty(uuid.NewString())},
		"unproxied": {UnproxiedMcpServerID: conv.PtrEmpty(uuid.NewString())},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			payload.ID = created.ID
			payload.Visibility = types.McpServerVisibility("public")
			_, err := ti.service.UpdateMcpServer(ctx, &payload)
			requireOopsCode(t, err, oops.CodeInvalid)
		})
	}
}

func TestUpdateMcpServer_ToolsetBacked_PrivateDetachesExternalOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createHostedServer(t, ctx, ti, toolset.ID, "public")
	oauth, err := oauthrepo.New(ti.conn).CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "ext-" + uuid.NewString()[:8],
		Metadata:  []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).UpdateToolsetExternalOAuthServer(ctx, toolsetsrepo.UpdateToolsetExternalOAuthServerParams{
		ExternalOauthServerID: uuid.NullUUID{UUID: oauth.ID, Valid: true},
		Slug:                  toolset.Slug,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)
	detachesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID:         created.ID,
		ToolsetID:  conv.PtrEmpty(toolset.ID.String()),
		Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, after.McpIsPublic)
	require.False(t, after.ExternalOauthServerID.Valid, "a private server cannot keep an external OAuth server attached")
	detachesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, detachesBefore+1, detachesAfter)
}

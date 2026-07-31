package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsRepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// wrapperForToolset loads the single live wrapper mcp_servers row mirrored
// for a toolset, requiring exactly one to exist.
func wrapperForToolset(t *testing.T, ti *testInstance, toolsetID, projectID uuid.UUID) mcpserversRepo.McpServer {
	t.Helper()
	wrappers, err := mcpserversRepo.New(ti.conn).GetMCPServersByToolsetID(t.Context(), mcpserversRepo.GetMCPServersByToolsetIDParams{
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Len(t, wrappers, 1, "expected exactly one mirrored wrapper mcp server")
	return wrappers[0]
}

func TestCreateToolset_MirrorsWrapperAndEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Mirrored Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.Nil(t, result.McpSlug, "new toolsets carry no publishing columns")

	wrapper := wrapperForToolset(t, ti, uuid.MustParse(result.ID), *authCtx.ProjectID)
	require.True(t, wrapper.ToolsetID.Valid)
	require.False(t, wrapper.RemoteMcpServerID.Valid)
	require.False(t, wrapper.TunneledMcpServerID.Valid)
	require.False(t, wrapper.UserSessionIssuerID.Valid, "auth stays on the toolset; the wrapper carries no issuer")

	endpoints, err := mcpendpointsRepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsRepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: wrapper.ID,
	})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.Contains(t, endpoints[0].Slug, "-", "platform endpoint slug is organization-prefixed")
	require.False(t, endpoints[0].CustomDomainID.Valid, "new toolsets publish on the platform domain")
}

func TestUpdateToolset_DisableMirrorsDisabledVisibility(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Disable Mirror Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// The translation path only applies to rows that still carry pre-swap
	// publishing columns.
	seedPublishingColumns(t, ctx, ti, uuid.MustParse(created.ID), "seed-"+string(created.Slug), true)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpIsPublic:            new(true),
		McpEnabled:             new(false),
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	wrapper := wrapperForToolset(t, ti, uuid.MustParse(created.ID), *authCtx.ProjectID)
	require.Equal(t, mcpservers.VisibilityDisabled, wrapper.Visibility, "mcp_enabled=false must win over mcp_is_public=true")
}

func TestDeleteToolset_TombstonesWrapperAndEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Delete Mirror Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	wrapper := wrapperForToolset(t, ti, uuid.MustParse(created.ID), *authCtx.ProjectID)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		SessionToken:     nil,
		Slug:             created.Slug,
		ProjectSlugInput: nil,
	}))

	wrappers, err := mcpserversRepo.New(ti.conn).GetMCPServersByToolsetID(ctx, mcpserversRepo.GetMCPServersByToolsetIDParams{
		ToolsetID: uuid.NullUUID{UUID: uuid.MustParse(created.ID), Valid: true},
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Empty(t, wrappers, "wrapper must be tombstoned with the toolset")

	endpoints, err := mcpendpointsRepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsRepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: wrapper.ID,
	})
	require.NoError(t, err)
	require.Empty(t, endpoints, "wrapper endpoints must be tombstoned with the toolset")
}

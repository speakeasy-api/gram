package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestToolsetsService_CreateStagingVersion_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a base toolset first
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	// Create base toolset
	baseToolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:         "Base Toolset",
		Description:  conv.Ptr("Base toolset for staging test"),
		ToolUrns:     toolUrns[:2],
		ResourceUrns: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, baseToolset)

	// Create staging version
	stagingToolset, err := ti.service.CreateStagingVersion(ctx, &gen.CreateStagingVersionPayload{
		Slug: baseToolset.Slug,
	})
	require.NoError(t, err)
	require.NotNil(t, stagingToolset)

	// Verify staging toolset has correct naming
	require.Equal(t, "Base Toolset (staging)", stagingToolset.Name)
	require.Equal(t, "base-toolset-staging", string(stagingToolset.Slug))
	require.Equal(t, baseToolset.Description, stagingToolset.Description)

	// Verify MCP is disabled for staging
	require.NotNil(t, stagingToolset.McpEnabled)
	require.False(t, *stagingToolset.McpEnabled)
}

func TestToolsetsService_CreateStagingVersion_AlreadyExists(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create base toolset
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	baseToolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:     "Base Toolset 2",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Create staging version first time
	staging1, err := ti.service.CreateStagingVersion(ctx, &gen.CreateStagingVersionPayload{
		Slug: baseToolset.Slug,
	})
	require.NoError(t, err)
	require.NotNil(t, staging1)

	// Create staging version second time - should return existing
	staging2, err := ti.service.CreateStagingVersion(ctx, &gen.CreateStagingVersionPayload{
		Slug: baseToolset.Slug,
	})
	require.NoError(t, err)
	require.NotNil(t, staging2)

	// Should return the same staging version
	require.Equal(t, staging1.ID, staging2.ID)
	require.Equal(t, staging1.Slug, staging2.Slug)
}

func TestToolsetsService_GetStagingVersion_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create base toolset and staging version
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	baseToolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:     "Base Toolset 3",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	stagingToolset, err := ti.service.CreateStagingVersion(ctx, &gen.CreateStagingVersionPayload{
		Slug: baseToolset.Slug,
	})
	require.NoError(t, err)

	// Get staging version
	retrieved, err := ti.service.GetStagingVersion(ctx, &gen.GetStagingVersionPayload{
		Slug: baseToolset.Slug,
	})
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	require.Equal(t, stagingToolset.ID, retrieved.ID)
	require.Equal(t, stagingToolset.Slug, retrieved.Slug)
}

func TestToolsetsService_GetStagingVersion_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create base toolset without staging version
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	baseToolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:     "Base Toolset 4",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Try to get non-existent staging version
	_, err = ti.service.GetStagingVersion(ctx, &gen.GetStagingVersionPayload{
		Slug: baseToolset.Slug,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "staging version not found")
}

func TestToolsetsService_DiscardStagingVersion_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create base toolset and staging version
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	baseToolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:     "Base Toolset 5",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	_, err = ti.service.CreateStagingVersion(ctx, &gen.CreateStagingVersionPayload{
		Slug: baseToolset.Slug,
	})
	require.NoError(t, err)

	// Discard staging version
	err = ti.service.DiscardStagingVersion(ctx, &gen.DiscardStagingVersionPayload{
		Slug: baseToolset.Slug,
	})
	require.NoError(t, err)

	// Verify staging version is gone
	_, err = ti.service.GetStagingVersion(ctx, &gen.GetStagingVersionPayload{
		Slug: baseToolset.Slug,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "staging version not found")
}

func TestToolsetsService_SwitchEditingMode_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create base toolset
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	baseToolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:     "Base Toolset 6",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Switch to staging mode
	updated, err := ti.service.SwitchEditingMode(ctx, &gen.SwitchEditingModePayload{
		Slug: baseToolset.Slug,
		Mode: "staging",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	// Note: editing_mode is internal state not exposed in API

	// Switch back to iteration mode (verify no error)
	updated, err = ti.service.SwitchEditingMode(ctx, &gen.SwitchEditingModePayload{
		Slug: baseToolset.Slug,
		Mode: "iteration",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
}

func TestToolsetsService_SwitchEditingMode_InvalidMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create base toolset
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	baseToolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:     "Base Toolset 7",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Try invalid mode
	_, err = ti.service.SwitchEditingMode(ctx, &gen.SwitchEditingModePayload{
		Slug: baseToolset.Slug,
		Mode: "invalid-mode",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid editing mode")
}

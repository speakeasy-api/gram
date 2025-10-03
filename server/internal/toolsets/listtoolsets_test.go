package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestToolsetsService_ListToolsets_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 4, "expected at least 4 tools from petstore")

	// Create a few toolsets
	toolset1, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "First Toolset",
		Description:            conv.Ptr("First test toolset"),
		ToolUrns:               []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	toolset2, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Second Toolset",
		Description:            conv.Ptr("Second test toolset"),
		ToolUrns:               []string{tools[2].ToolUrn.String(), tools[3].ToolUrn.String()},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// List toolsets
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 2)

	// Check that both toolsets are present and have HTTP tools populated
	toolsetIDs := make(map[string]bool)
	for _, ts := range result.Toolsets {
		toolsetIDs[ts.ID] = true
		require.NotEmpty(t, ts.Tools, "HTTP tools should be populated")
		require.Len(t, ts.Tools, 2, "each toolset should have 2 tools")
	}
	require.True(t, toolsetIDs[toolset1.ID])
	require.True(t, toolsetIDs[toolset2.ID])
}

func TestToolsetsService_ListToolsets_EmptyList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// List toolsets when none exist
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Toolsets)
}

func TestToolsetsService_ListToolsets_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_ListToolsets_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_ListToolsets_VerifyDetails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset with specific details
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Detailed Toolset",
		Description:            conv.Ptr("A toolset with details"),
		ToolUrns:               []string{},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// List toolsets and verify details
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 1)

	toolset := result.Toolsets[0]
	require.Equal(t, created.ID, toolset.ID)
	require.Equal(t, "Detailed Toolset", toolset.Name)
	require.Equal(t, "detailed-toolset", string(toolset.Slug))
	require.Equal(t, "A toolset with details", *toolset.Description)
	require.Empty(t, toolset.Tools)
	require.NotNil(t, toolset.CreatedAt)
	require.NotNil(t, toolset.UpdatedAt)
}

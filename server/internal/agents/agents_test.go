package agents_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// --- LoadToolsetTools Tests ---

func TestAgentsService_LoadToolsetTools_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools from petstore")

	// Create a toolset with the tools
	created, err := ti.toolsets.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Agent Test Toolset",
		Description:            new("A test toolset for agents"),
		ToolUrns:               []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Get auth context for project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Load tools using the agents service
	agentTools, err := ti.agentsService.LoadToolsetTools(
		ctx,
		*authCtx.ProjectID,
		string(created.Slug),
		"", // no environment
	)
	require.NoError(t, err)
	require.Len(t, agentTools, 2, "should have 2 tools")

	// Verify tool properties
	for _, tool := range agentTools {
		require.True(t, tool.IsMCPTool)
		require.Equal(t, string(created.Slug), tool.ServerLabel)
		require.NotNil(t, tool.ToolURN)
		require.NotNil(t, tool.Definition.Function)
		require.NotEmpty(t, tool.Definition.Function.Name)
		require.Equal(t, "function", tool.Definition.Type)
	}
}

func TestAgentsService_LoadToolsetTools_EmptyToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	// Create an empty toolset
	created, err := ti.toolsets.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Empty Toolset",
		Description:            new("An empty toolset"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Get auth context for project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Load tools - should return empty slice
	agentTools, err := ti.agentsService.LoadToolsetTools(
		ctx,
		*authCtx.ProjectID,
		string(created.Slug),
		"",
	)
	require.NoError(t, err)
	require.Empty(t, agentTools)
}

func TestAgentsService_LoadToolsetTools_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Try to load non-existent toolset
	_, err := ti.agentsService.LoadToolsetTools(
		ctx,
		*authCtx.ProjectID,
		"non-existent-toolset",
		"",
	)
	require.Error(t, err)
}

func TestAgentsService_LoadToolsetTools_VerifyToolDefinition(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	// Create deployment
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.NotEmpty(t, tools)

	// Create toolset with one tool
	created, err := ti.toolsets.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Single Tool Toolset",
		Description:            new("Toolset with single tool"),
		ToolUrns:               []string{tools[0].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Load and verify
	agentTools, err := ti.agentsService.LoadToolsetTools(
		ctx,
		*authCtx.ProjectID,
		string(created.Slug),
		"",
	)
	require.NoError(t, err)
	require.Len(t, agentTools, 1)

	tool := agentTools[0]
	require.Equal(t, "function", tool.Definition.Type)
	require.NotNil(t, tool.Definition.Function)
	require.NotEmpty(t, tool.Definition.Function.Name)
	require.NotNil(t, tool.Definition.Function.Parameters)
}

// --- LoadToolsByURN Tests ---

func TestAgentsService_LoadToolsByURN_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	// Create deployment
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tools), 2)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Load tools by URN
	toolURNs := []urn.Tool{tools[0].ToolUrn, tools[1].ToolUrn}
	agentTools, err := ti.agentsService.LoadToolsByURN(
		ctx,
		*authCtx.ProjectID,
		toolURNs,
		"",
	)
	require.NoError(t, err)
	require.Len(t, agentTools, 2)

	// Verify each tool
	for i, tool := range agentTools {
		require.True(t, tool.IsMCPTool)
		require.NotNil(t, tool.ToolURN)
		require.Equal(t, toolURNs[i].Source, tool.ServerLabel)
		require.NotNil(t, tool.Definition.Function)
		require.NotEmpty(t, tool.Definition.Function.Name)
	}
}

func TestAgentsService_LoadToolsByURN_EmptyList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Load with empty URN list
	agentTools, err := ti.agentsService.LoadToolsByURN(
		ctx,
		*authCtx.ProjectID,
		[]urn.Tool{},
		"",
	)
	require.NoError(t, err)
	require.Empty(t, agentTools)
}

func TestAgentsService_LoadToolsByURN_SingleTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	// Create deployment
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get one tool
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.NotEmpty(t, tools)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Load single tool by URN
	agentTools, err := ti.agentsService.LoadToolsByURN(
		ctx,
		*authCtx.ProjectID,
		[]urn.Tool{tools[0].ToolUrn},
		"",
	)
	require.NoError(t, err)
	require.Len(t, agentTools, 1)

	tool := agentTools[0]
	require.True(t, tool.IsMCPTool)
	require.Equal(t, tools[0].ToolUrn.Source, tool.ServerLabel)
	require.Equal(t, tools[0].ToolUrn.Name, tool.Definition.Function.Name)
}

func TestAgentsService_LoadToolsByURN_InvalidURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create an invalid tool URN
	invalidURN := urn.NewTool(urn.ToolKindHTTP, "non-existent-source", "non-existent-tool")

	// Should return error for non-existent tool
	_, err := ti.agentsService.LoadToolsByURN(
		ctx,
		*authCtx.ProjectID,
		[]urn.Tool{invalidURN},
		"",
	)
	require.Error(t, err)
}

func TestAgentsService_LoadToolsByURN_VerifyServerLabel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	// Create deployment
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.NotEmpty(t, tools)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Load tool and verify ServerLabel matches URN source
	agentTools, err := ti.agentsService.LoadToolsByURN(
		ctx,
		*authCtx.ProjectID,
		[]urn.Tool{tools[0].ToolUrn},
		"",
	)
	require.NoError(t, err)
	require.Len(t, agentTools, 1)

	// ServerLabel should be the source from the URN
	require.Equal(t, tools[0].ToolUrn.Source, agentTools[0].ServerLabel)
}

func TestAgentsService_LoadToolsByURN_VerifyToolURNPointer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentsService(t)

	// Create deployment
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tools), 2)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Load multiple tools
	toolURNs := []urn.Tool{tools[0].ToolUrn, tools[1].ToolUrn}
	agentTools, err := ti.agentsService.LoadToolsByURN(
		ctx,
		*authCtx.ProjectID,
		toolURNs,
		"",
	)
	require.NoError(t, err)
	require.Len(t, agentTools, 2)

	// Verify each tool has a unique ToolURN pointer (not sharing references)
	require.NotSame(t, agentTools[0].ToolURN, agentTools[1].ToolURN)

	// Verify URN values match
	require.Equal(t, toolURNs[0].String(), agentTools[0].ToolURN.String())
	require.Equal(t, toolURNs[1].String(), agentTools[1].ToolURN.String())
}

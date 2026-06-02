package mcpmetadata_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestService_GetMcpMetadata_WithInstructions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test MCP Server",
		Slug:                   "test-mcp-get",
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	instructions := "You have tools for searching the Test Hub. Use them wisely."
	docURL := "https://docs.example.com"

	// Set metadata first
	setPayload := &gen.SetMcpMetadataPayload{
		ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
		LogoAssetID:              nil,
		ExternalDocumentationURL: &docURL,
		Instructions:             &instructions,
		SessionToken:             nil,
		ProjectSlugInput:         nil,
	}

	_, err = ti.service.SetMcpMetadata(ctx, setPayload)
	require.NoError(t, err)

	// Now fetch it
	getPayload := &gen.GetMcpMetadataPayload{
		ToolsetSlug:      conv.PtrEmpty(types.Slug(toolset.Slug)),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}

	result, err := ti.service.GetMcpMetadata(ctx, getPayload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Metadata)

	require.NotEmpty(t, result.Metadata.ID)
	require.NotNil(t, result.Metadata.ToolsetID)
	require.Equal(t, toolset.ID.String(), *result.Metadata.ToolsetID)
	require.NotNil(t, result.Metadata.Instructions)
	require.Equal(t, instructions, *result.Metadata.Instructions)
	require.NotNil(t, result.Metadata.ExternalDocumentationURL)
	require.Equal(t, docURL, *result.Metadata.ExternalDocumentationURL)
	require.Nil(t, result.Metadata.LogoAssetID)
}

func TestService_GetMcpMetadata_WithoutInstructions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test MCP Server",
		Slug:                   "test-mcp-get-no-instructions",
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	docURL := "https://docs.example.com"

	// Set metadata without instructions
	setPayload := &gen.SetMcpMetadataPayload{
		ToolsetSlug:              conv.PtrEmpty(types.Slug(toolset.Slug)),
		LogoAssetID:              nil,
		ExternalDocumentationURL: &docURL,
		Instructions:             nil,
		SessionToken:             nil,
		ProjectSlugInput:         nil,
	}

	_, err = ti.service.SetMcpMetadata(ctx, setPayload)
	require.NoError(t, err)

	// Now fetch it
	getPayload := &gen.GetMcpMetadataPayload{
		ToolsetSlug:      conv.PtrEmpty(types.Slug(toolset.Slug)),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}

	result, err := ti.service.GetMcpMetadata(ctx, getPayload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Metadata)

	require.NotEmpty(t, result.Metadata.ID)
	require.NotNil(t, result.Metadata.ToolsetID)
	require.Equal(t, toolset.ID.String(), *result.Metadata.ToolsetID)
	require.Nil(t, result.Metadata.Instructions)
	require.NotNil(t, result.Metadata.ExternalDocumentationURL)
	require.Equal(t, docURL, *result.Metadata.ExternalDocumentationURL)
}

func TestService_GetMcpMetadata_ByMcpServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{
		visibility: mcpservers.VisibilityPrivate,
	})

	docURL := "https://docs.example.com"
	instructions := "Server instructions for the remote MCP."

	serverID := server.ID.String()
	_, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID:              &serverID,
		ExternalDocumentationURL: &docURL,
		Instructions:             &instructions,
	})
	require.NoError(t, err)

	result, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{
		McpServerID: &serverID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Metadata)

	require.Nil(t, result.Metadata.ToolsetID)
	require.NotNil(t, result.Metadata.McpServerID)
	require.Equal(t, serverID, *result.Metadata.McpServerID)
	require.NotNil(t, result.Metadata.Instructions)
	require.Equal(t, instructions, *result.Metadata.Instructions)
	require.NotNil(t, result.Metadata.ExternalDocumentationURL)
	require.Equal(t, docURL, *result.Metadata.ExternalDocumentationURL)
}

func TestService_GetMcpMetadata_RejectsBothBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	toolset := createTestToolset(t, ctx, ti, "xor-both-toolset")
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{})

	serverID := server.ID.String()
	_, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{
		ToolsetSlug: conv.PtrEmpty(types.Slug(toolset.Slug)),
		McpServerID: &serverID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_GetMcpMetadata_RejectsNeitherBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	_, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_GetMcpMetadata_RejectsCrossProjectMcpServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	// A random UUID that does not exist in the caller's project should look
	// indistinguishable from a foreign-project id to the IDOR guard: the
	// project-scoped lookup returns no rows in either case.
	foreignID, err := uuid.NewV7()
	require.NoError(t, err)

	idStr := foreignID.String()
	_, err = ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{
		McpServerID: &idStr,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// requireOopsCode asserts that err is an oops error carrying the expected
// shareable code. Mirrors the helper convention used in other services'
// setup_test.go files (e.g. internal/usersessions, internal/remotemcp).
func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	require.Error(t, err)
	var shareErr *oops.ShareableError
	require.ErrorAs(t, err, &shareErr)
	require.Equal(t, code, shareErr.Code)
}

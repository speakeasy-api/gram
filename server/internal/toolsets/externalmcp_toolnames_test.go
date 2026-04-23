package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	externalmcpRepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcpTypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestToolsetsService_ListToolsetsForOrg_ExternalMCPToolNames(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a deployment so external MCP attachments have something to reference.
	dep := createPetstoreDeployment(t, ctx, ti)
	deploymentID := uuid.MustParse(dep.Deployment.ID)

	// Create an MCP registry entry (no SQLc query exists for mcp_registries).
	var registryID uuid.UUID
	err := ti.conn.QueryRow(ctx, `
		INSERT INTO mcp_registries (name, url)
		VALUES ($1, $2)
		RETURNING id
	`, "test-registry-toolnames", "https://example.com/mcp").Scan(&registryID)
	require.NoError(t, err)

	// Create an external MCP attachment with slug "my-github-mcp".
	attachmentSlug := "my-github-mcp"
	emcpRepo := externalmcpRepo.New(ti.conn)
	attachment, err := emcpRepo.CreateExternalMCPAttachment(ctx, externalmcpRepo.CreateExternalMCPAttachmentParams{
		DeploymentID:            deploymentID,
		RegistryID:              uuid.NullUUID{UUID: registryID, Valid: true},
		Name:                    "My GitHub MCP",
		Slug:                    attachmentSlug,
		RegistryServerSpecifier: "test-server",
	})
	require.NoError(t, err)

	// Create two external MCP tool definitions with distinct tool names in the URN.
	toolURN1 := urn.NewTool(urn.ToolKindExternalMCP, attachmentSlug, "create-issue").String()
	toolURN2 := urn.NewTool(urn.ToolKindExternalMCP, attachmentSlug, "list-repos").String()

	_, err = emcpRepo.CreateExternalMCPToolDefinition(ctx, externalmcpRepo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachment.ID,
		ToolUrn:                    toolURN1,
		Type:                       "proxy",
		Name:                       pgtype.Text{},
		Description:                pgtype.Text{},
		Schema:                     nil,
		RemoteUrl:                  "https://example.com/mcp",
		TransportType:              externalmcpTypes.TransportTypeStreamableHTTP,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
		HeaderDefinitions:          nil,
		Title:                      pgtype.Text{},
		ReadOnlyHint:               pgtype.Bool{},
		DestructiveHint:            pgtype.Bool{},
		IdempotentHint:             pgtype.Bool{},
		OpenWorldHint:              pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = emcpRepo.CreateExternalMCPToolDefinition(ctx, externalmcpRepo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachment.ID,
		ToolUrn:                    toolURN2,
		Type:                       "proxy",
		Name:                       pgtype.Text{},
		Description:                pgtype.Text{},
		Schema:                     nil,
		RemoteUrl:                  "https://example.com/mcp",
		TransportType:              externalmcpTypes.TransportTypeStreamableHTTP,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
		HeaderDefinitions:          nil,
		Title:                      pgtype.Text{},
		ReadOnlyHint:               pgtype.Bool{},
		DestructiveHint:            pgtype.Bool{},
		IdempotentHint:             pgtype.Bool{},
		OpenWorldHint:              pgtype.Bool{},
	})
	require.NoError(t, err)

	// Create a toolset that references these external MCP tool URNs.
	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "External MCP Toolset",
		Description:            nil,
		ToolUrns:               []string{toolURN1, toolURN2},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, toolset)

	// ListToolsetsForOrg and verify tool names come from URN, not attachment slug.
	result, err := ti.service.ListToolsetsForOrg(ctx, &gen.ListToolsetsForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Find our toolset in the results.
	var found bool
	for _, ts := range result.Toolsets {
		if ts.ID != toolset.ID {
			continue
		}
		found = true
		require.Len(t, ts.Tools, 2)

		namesByURN := make(map[string]string)
		for _, tool := range ts.Tools {
			namesByURN[tool.ToolUrn] = tool.Name
		}

		// Names must be the URN tool name, NOT the attachment slug.
		require.Equal(t, "create-issue", namesByURN[toolURN1],
			"tool name should be 'create-issue' from URN, not the attachment slug")
		require.Equal(t, "list-repos", namesByURN[toolURN2],
			"tool name should be 'list-repos' from URN, not the attachment slug")
	}
	require.True(t, found, "toolset should appear in ListToolsetsForOrg results")
}

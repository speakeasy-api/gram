package collections_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/collections"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

func TestCollectionsService_AttachServer_AllowsOriginBackedToolsets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	toolset := createMCPEnabledToolset(
		t,
		ctx,
		ti,
		"Origin Toolset",
		"com.speakeasy.example/server",
	)
	collection := createCollection(
		t,
		ctx,
		ti,
		"Registry",
		"registry",
		"com.speakeasy.registry",
	)

	result, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, collection.ID, result.ID)
}

func TestCollectionsService_ListServers_PreservesOriginLineage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	toolset := createMCPEnabledToolset(
		t,
		ctx,
		ti,
		"Origin Toolset",
		"com.speakeasy.example/server",
	)
	collection := createCollection(
		t,
		ctx,
		ti,
		"Registry",
		"registry",
		"com.speakeasy.registry",
	)

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	require.NotNil(t, toolset.McpSlug)
	require.Equal(t, "com.speakeasy.registry/"+string(*toolset.McpSlug), result.Servers[0].RegistrySpecifier)
	require.NotNil(t, result.Servers[0].ToolsetID)
	require.Equal(t, toolset.ID, *result.Servers[0].ToolsetID)
	require.Nil(t, result.Servers[0].RegistryID)
	require.NotNil(t, result.Servers[0].OrganizationMcpCollectionRegistryID)
	require.NotNil(t, result.Servers[0].Remotes)
	require.Len(t, result.Servers[0].Remotes, 1)
}

func TestCollectionsService_ListServers_IncludesUserProvidedEnvironmentHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createMCPEnabledToolset(
		t,
		ctx,
		ti,
		"Livestorm Toolset",
		"com.speakeasy.example/livestorm",
	)
	toolsetID, err := uuid.Parse(toolset.ID)
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, "UPDATE toolsets SET mcp_is_public = TRUE WHERE id = $1", toolsetID)
	require.NoError(t, err)

	mcpRepo := mcpmetarepo.New(ti.conn)
	metadata, err := mcpRepo.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID:                 toolsetID,
		ProjectID:                 *authCtx.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{Valid: false},
		ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID:                    uuid.NullUUID{Valid: false},
		Instructions:              pgtype.Text{Valid: false},
		DefaultEnvironmentID:      uuid.NullUUID{Valid: false},
		InstallationOverrideUrl:   pgtype.Text{Valid: false},
	})
	require.NoError(t, err)

	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "LIVESTORM_API_KEY",
		HeaderDisplayName: pgtype.Text{String: "MCP-Livestorm-API-Key", Valid: true},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "SYSTEM_ONLY",
		HeaderDisplayName: pgtype.Text{String: "X-System-Only", Valid: true},
		ProvidedBy:        "system",
	})
	require.NoError(t, err)
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "HEADERLESS_SECRET",
		HeaderDisplayName: pgtype.Text{Valid: false},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)

	collection := createCollection(
		t,
		ctx,
		ti,
		"Registry",
		"registry",
		"com.speakeasy.registry",
	)

	_, err = ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	require.Len(t, result.Servers[0].Remotes, 1)

	headers := result.Servers[0].Remotes[0].Headers
	require.Len(t, headers, 2)

	headersByName := make(map[string]*types.ExternalMCPRemoteHeader, len(headers))
	for _, header := range headers {
		headersByName[header.Name] = header
	}

	livestormHeader := headersByName[toolconfig.ToHTTPHeader("MCP-LIVESTORM_API_KEY")]
	require.NotNil(t, livestormHeader)
	require.NotNil(t, livestormHeader.IsSecret)
	require.True(t, *livestormHeader.IsSecret)
	require.NotNil(t, livestormHeader.IsRequired)
	require.True(t, *livestormHeader.IsRequired)
	require.NotNil(t, livestormHeader.Placeholder)
	require.Equal(t, "${MCP_LIVESTORM_API_KEY}", *livestormHeader.Placeholder)

	headerlessHeader := headersByName[toolconfig.ToHTTPHeader("MCP-HEADERLESS_SECRET")]
	require.NotNil(t, headerlessHeader)
	require.NotNil(t, headerlessHeader.Placeholder)
	require.Equal(t, "${MCP_HEADERLESS_SECRET}", *headerlessHeader.Placeholder)
	require.Nil(t, result.Servers[0].Remotes[0].Variables)
}

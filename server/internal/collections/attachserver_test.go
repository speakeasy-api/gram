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
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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

	headers := result.Servers[0].Remotes[0].Headers
	require.Len(t, headers, 2)
	headersByName := make(map[string]*types.ExternalMCPRemoteHeader, len(headers))
	for _, header := range headers {
		headersByName[header.Name] = header
	}

	environmentHeader := headersByName[toolconfig.ToHTTPHeader("gram_environment")]
	require.NotNil(t, environmentHeader)
	require.NotNil(t, environmentHeader.Placeholder)
	require.Equal(t, "${GRAM_ENVIRONMENT}", *environmentHeader.Placeholder)

	authorizationHeader := headersByName[toolconfig.ToHTTPHeader("authorization")]
	require.NotNil(t, authorizationHeader)
	require.NotNil(t, authorizationHeader.IsSecret)
	require.True(t, *authorizationHeader.IsSecret)
	require.NotNil(t, authorizationHeader.Placeholder)
	require.Equal(t, "${GRAM_KEY}", *authorizationHeader.Placeholder)
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
		"Example Toolset",
		"com.speakeasy.example/server",
	)
	toolsetID, err := uuid.Parse(toolset.ID)
	require.NoError(t, err)
	err = toolsetsRepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsRepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolsetID,
		ProjectID:   *authCtx.ProjectID,
	})
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
		VariableName:      "SERVICE_API_KEY",
		HeaderDisplayName: pgtype.Text{String: "External API Key", Valid: true},
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

	customDisplayHeader := headersByName[toolconfig.ToHTTPHeader("MCP-SERVICE_API_KEY")]
	require.NotNil(t, customDisplayHeader)
	require.NotNil(t, customDisplayHeader.IsSecret)
	require.True(t, *customDisplayHeader.IsSecret)
	require.NotNil(t, customDisplayHeader.IsRequired)
	require.True(t, *customDisplayHeader.IsRequired)
	require.NotNil(t, customDisplayHeader.Placeholder)
	require.Equal(t, "${EXTERNAL_API_KEY}", *customDisplayHeader.Placeholder)

	headerlessHeader := headersByName[toolconfig.ToHTTPHeader("MCP-HEADERLESS_SECRET")]
	require.NotNil(t, headerlessHeader)
	require.NotNil(t, headerlessHeader.Placeholder)
	require.Equal(t, "${MCP_HEADERLESS_SECRET}", *headerlessHeader.Placeholder)
	require.Nil(t, result.Servers[0].Remotes[0].Variables)
}

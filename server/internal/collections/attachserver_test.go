package collections_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/collections"
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

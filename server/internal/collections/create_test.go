package collections_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/collections"
)

func TestCollectionsService_Create_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	desc := "A test collection"
	result, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "My Collection",
		Slug:                 "my-collection",
		Description:          &desc,
		McpRegistryNamespace: "com.example.my-tools",
		Visibility:           "private",
		ToolsetIds:           []string{},
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.ID)
	require.Equal(t, "My Collection", result.Name)
	require.Equal(t, "my-collection", result.Slug)
	require.Equal(t, "A test collection", *result.Description)
	require.Equal(t, "com.example.my-tools", *result.McpRegistryNamespace)
	require.Equal(t, "private", result.Visibility)
}

func TestCollectionsService_Create_WithToolsetIds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	ts := createMCPEnabledToolset(t, ctx, ti, "Attach Me")

	result, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "Collection With Toolsets",
		Slug:                 "collection-with-toolsets",
		McpRegistryNamespace: "com.example.with-toolsets",
		Visibility:           "private",
		ToolsetIds:           []string{ts.ID},
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	servers, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug:   "collection-with-toolsets",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, servers.Servers, 1)
}

func TestCollectionsService_Create_InvalidToolsetIdsRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	result, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "Collection Bad IDs",
		Slug:                 "collection-bad-ids",
		McpRegistryNamespace: "com.example.bad-ids",
		Visibility:           "private",
		ToolsetIds:           []string{"not-a-uuid", "also-invalid"},
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "invalid toolset_id")
}

func TestCollectionsService_Create_DuplicateSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	createCollection(t, ctx, ti, "First", "duplicate-slug", "com.example.first")

	_, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "Second",
		Slug:                 "duplicate-slug",
		McpRegistryNamespace: "com.example.second",
		Visibility:           "private",
		ToolsetIds:           []string{},
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "collection slug already exists")
}

func TestCollectionsService_Create_DuplicateNamespace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	createCollection(t, ctx, ti, "First", "ns-first", "com.example.shared-namespace")

	_, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "Second",
		Slug:                 "ns-second",
		McpRegistryNamespace: "com.example.shared-namespace",
		Visibility:           "private",
		ToolsetIds:           []string{},
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "registry namespace already exists")
}

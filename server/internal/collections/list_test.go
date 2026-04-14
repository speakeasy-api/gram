package collections_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/collections"
	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
	collectionsRepo "github.com/speakeasy-api/gram/server/internal/collections/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestCollectionsService_List_CreatesDefaultRegistryCollection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	result, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	var found bool
	for _, c := range result.Collections {
		if c.Slug == "registry" {
			found = true
			require.Equal(t, "Registry", c.Name)
			require.Equal(t, "private", c.Visibility)
			require.NotNil(t, c.McpRegistryNamespace)
			require.Equal(t, fmt.Sprintf("com.speakeasy.%s.registry", mockidp.MockOrgSlug), *c.McpRegistryNamespace)
			break
		}
	}
	require.True(t, found, "default registry collection should be present in list results")
}

func TestCollectionsService_List_DefaultRegistryIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	first, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	second, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	third, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Len(t, second.Collections, len(first.Collections))
	require.Len(t, third.Collections, len(first.Collections))

	repo := collectionsRepo.New(ti.conn)
	rows, err := repo.ListOrganizationMcpCollections(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	var registryCount int
	for _, r := range rows {
		if r.Slug == "registry" {
			registryCount++
		}
	}
	require.Equal(t, 1, registryCount, "exactly one default registry collection should exist")
}

func TestCollectionsService_List_DefaultRegistryCoexistsWithUserCollections(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	createCollection(t, ctx, ti, "Custom", "custom-collection", "com.example.custom")

	result, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Collections), 2)

	slugs := make(map[string]bool, len(result.Collections))
	for _, c := range result.Collections {
		slugs[c.Slug] = true
	}
	require.True(t, slugs["registry"], "default registry should be present")
	require.True(t, slugs["custom-collection"], "user-created collection should be present")
}

func TestCollectionsService_List_DefaultRegistryNotDuplicatedWhenAlreadyExists(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	createCollection(t, ctx, ti, "Registry", "registry", "com.example.preexisting")

	result, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	var registryCount int
	for _, c := range result.Collections {
		if c.Slug == "registry" {
			registryCount++
		}
	}
	require.Equal(t, 1, registryCount, "should not duplicate the registry collection")
}

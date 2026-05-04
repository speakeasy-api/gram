package collections_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/server/internal/testenv/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/collections"
	collectionsRepo "github.com/speakeasy-api/gram/server/internal/collections/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestCollectionsService_List_CreatesDefaultRegistryCollection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	result, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
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
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	second, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	third, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
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
		SessionToken: nil,
		ApikeyToken:  nil,
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
		SessionToken: nil,
		ApikeyToken:  nil,
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

func TestCollectionsService_List_DefaultRegistryConcurrent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	start := make(chan struct{})
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			_, err := ti.service.List(ctx, &gen.ListPayload{
				SessionToken: nil,
				ApikeyToken:  nil,
			})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "concurrent ensure of default registry collection must not fail")
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	cRepo := collectionsRepo.New(ti.conn)
	rows, err := cRepo.ListOrganizationMcpCollections(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	var registryCount int
	for _, r := range rows {
		if r.Slug == "registry" {
			registryCount++
		}
	}
	require.Equal(t, 1, registryCount, "exactly one default registry collection should exist after concurrent calls")
}

func TestCollectionsService_List_EnsureDefaultRegistryFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	_, ti := newTestCollectionsService(t)

	// Create a context with an org ID that doesn't exist in organization_metadata.
	// This causes ensureDefaultRegistryCollection to fail (FK violation), but List
	// should still succeed and return an empty result.
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_nonexistent",
		OrganizationSlug:     "nonexistent",
		UserID:               "user_test",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Collections)
}

func TestCollectionsService_List_DefaultRegistryBackfillsMissingNamespace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	cRepo := collectionsRepo.New(ti.conn)
	_, err := cRepo.CreateOrganizationMcpCollection(ctx, collectionsRepo.CreateOrganizationMcpCollectionParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "Registry",
		Description:    pgtype.Text{String: "", Valid: false},
		Slug:           "registry",
		Visibility:     "private",
	})
	require.NoError(t, err)

	result, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	var found bool
	for _, c := range result.Collections {
		if c.Slug == "registry" {
			found = true
			require.NotNil(t, c.McpRegistryNamespace)
			require.Equal(t, fmt.Sprintf("com.speakeasy.%s.registry", mockidp.MockOrgSlug), *c.McpRegistryNamespace)
		}
	}
	require.True(t, found, "default registry collection should be present with a backfilled namespace")
}

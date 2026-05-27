package deployments_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDeploymentsService_ListDeployments_Success(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload OpenAPI asset
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	// Create deployment
	created, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-deployments",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "test-doc",
				Slug:    "test-doc",
			},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment")

	// Test ListDeployments
	result, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list deployments")

	// Verify response structure
	require.NotNil(t, result.Items, "items should not be nil")
	require.GreaterOrEqual(t, len(result.Items), 1, "should have at least one deployment")

	// Find our created deployment in the list
	var foundDeployment *gen.DeploymentSummary
	for _, item := range result.Items {
		if item.ID == created.Deployment.ID {
			foundDeployment = item
			break
		}
	}
	require.NotNil(t, foundDeployment, "should find created deployment in list")

	// Verify deployment summary structure
	require.Equal(t, created.Deployment.ID, foundDeployment.ID, "deployment ID should match")
	require.Equal(t, created.Deployment.UserID, foundDeployment.UserID, "user ID should match")
	require.NotEmpty(t, foundDeployment.CreatedAt, "created at should not be empty")
	require.NotEmpty(t, foundDeployment.Status, "status should not be empty")
	require.GreaterOrEqual(t, foundDeployment.Openapiv3AssetCount, int64(1), "openapi asset count should be at least 1")
	require.GreaterOrEqual(t, foundDeployment.Openapiv3ToolCount, int64(0), "openapi tool count should be at least 0")
}

func TestDeploymentsService_ListDeployments_EmptyList(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test ListDeployments when no deployments exist
	result, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "should not error when no deployments exist")
	require.NotNil(t, result.Items, "items should not be nil")
	require.Empty(t, result.Items, "items should be empty when no deployments exist")
	require.Nil(t, result.NextCursor, "next cursor should be nil for empty results")
}

func TestDeploymentsService_ListDeployments_WithCursor(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload OpenAPI asset
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	// Create deployment
	_, err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-deployments-cursor",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "test-doc",
				Slug:    "test-doc",
			},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment")

	// Get first page
	firstPage, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get first page of deployments")
	require.NotNil(t, firstPage.Items, "first page items should not be nil")

	// If there's a next cursor, test pagination
	if firstPage.NextCursor != nil {
		secondPage, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
			Cursor:           firstPage.NextCursor,
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err, "get second page of deployments")
		require.NotNil(t, secondPage.Items, "second page items should not be nil")
	}
}

func TestDeploymentsService_ListDeployments_InvalidCursor(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with invalid cursor UUID
	invalidCursor := "invalid-cursor-uuid"
	_, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           &invalidCursor,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cursor")
}

func TestDeploymentsService_ListDeployments_Unauthorized(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	_, ti := newTestDeploymentService(t, assetStorage)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestDeploymentsService_ListDeployments_MultipleDeployments(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload assets for multiple deployments
	bs1 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares1, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs1.Len()),
	}, io.NopCloser(bs1))
	require.NoError(t, err, "upload first openapi v3 asset")

	bs2 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares2, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs2.Len()),
	}, io.NopCloser(bs2))
	require.NoError(t, err, "upload second openapi v3 asset")

	bs3 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/crm-valid.yaml"))
	ares3, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs3.Len()),
	}, io.NopCloser(bs3))
	require.NoError(t, err, "upload third openapi v3 asset")

	// Create multiple deployments
	deployments := make([]*gen.CreateDeploymentResult, 3)

	deployments[0], err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-multiple-first",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "first-doc",
				Slug:    "first-doc",
			},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create first deployment")

	deployments[1], err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-multiple-second",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares2.Asset.ID,
				Name:    "second-doc",
				Slug:    "second-doc",
			},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create second deployment")

	deployments[2], err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-multiple-third",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares3.Asset.ID,
				Name:    "third-doc",
				Slug:    "third-doc",
			},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create third deployment")

	// List deployments
	result, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list deployments")
	require.NotNil(t, result.Items, "items should not be nil")
	require.GreaterOrEqual(t, len(result.Items), 3, "should have at least 3 deployments")

	// Verify all created deployments are in the list
	foundDeployments := make(map[string]bool)
	for _, item := range result.Items {
		for _, dep := range deployments {
			if item.ID == dep.Deployment.ID {
				foundDeployments[dep.Deployment.ID] = true
				// Verify the summary fields
				require.Equal(t, dep.Deployment.UserID, item.UserID, "user ID should match for deployment %s", dep.Deployment.ID)
				require.NotEmpty(t, item.CreatedAt, "created at should not be empty for deployment %s", dep.Deployment.ID)
				require.NotEmpty(t, item.Status, "status should not be empty for deployment %s", dep.Deployment.ID)
				require.GreaterOrEqual(t, item.Openapiv3AssetCount, int64(1), "openapi asset count should be at least 1 for deployment %s", dep.Deployment.ID)
				require.GreaterOrEqual(t, item.Openapiv3ToolCount, int64(0), "openapi tool count should be at least 0 for deployment %s", dep.Deployment.ID)
			}
		}
	}

	require.Len(t, foundDeployments, 3, "should find all 3 created deployments in the list")
	for _, dep := range deployments {
		require.True(t, foundDeployments[dep.Deployment.ID], "deployment %s should be found in list", dep.Deployment.ID)
	}
}

func TestDeploymentsService_ListDeployments_OrderedByCreationTime(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload assets for multiple deployments
	bs1 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares1, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs1.Len()),
	}, io.NopCloser(bs1))
	require.NoError(t, err, "upload first openapi v3 asset")

	bs2 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares2, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs2.Len()),
	}, io.NopCloser(bs2))
	require.NoError(t, err, "upload second openapi v3 asset")

	// Create deployments in sequence
	first, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-ordering-first",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "first-doc",
				Slug:    "first-doc",
			},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create first deployment")

	second, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-ordering-second",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares2.Asset.ID,
				Name:    "second-doc",
				Slug:    "second-doc",
			},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create second deployment")

	// List deployments
	result, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "list deployments")
	require.NotNil(t, result.Items, "items should not be nil")
	require.GreaterOrEqual(t, len(result.Items), 2, "should have at least 2 deployments")

	// Find the positions of our deployments in the list
	var firstPos, secondPos = -1, -1
	for i, item := range result.Items {
		if item.ID == first.Deployment.ID {
			firstPos = i
		}
		if item.ID == second.Deployment.ID {
			secondPos = i
		}
	}

	require.NotEqual(t, -1, firstPos, "first deployment should be found in list")
	require.NotEqual(t, -1, secondPos, "second deployment should be found in list")

	// The second deployment should appear before the first (newer items first)
	require.Less(t, secondPos, firstPos, "second deployment should appear before first deployment (newer items first)")
}

func TestDeploymentsService_ListDeployments_ValidCursor(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload OpenAPI asset
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	// Create deployment
	_, err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-deployments-valid-cursor",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "test-doc",
				Slug:    "test-doc",
			},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment")

	// Test with valid cursor (using a valid UUID format)
	validCursor := uuid.New().String()
	result, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           &validCursor,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "should not error with valid cursor format")
	require.NotNil(t, result.Items, "items should not be nil")
}

func TestDeploymentsService_ListDeployments_FilterBySourceSlugs(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload two distinct OpenAPI assets that will back two different sources.
	bsTodo := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	aresTodo, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bsTodo.Len()),
	}, io.NopCloser(bsTodo))
	require.NoError(t, err, "upload todo openapi asset")

	bsPet := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	aresPet, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bsPet.Len()),
	}, io.NopCloser(bsPet))
	require.NoError(t, err, "upload petstore openapi asset")

	// Deployment A contains source slug "todos".
	depA, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "filter-by-source-a",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{AssetID: aresTodo.Asset.ID, Name: "todos", Slug: "todos"},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment A")

	// Deployment B contains source slug "pets".
	depB, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "filter-by-source-b",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{AssetID: aresPet.Asset.ID, Name: "pets", Slug: "pets"},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment B")

	// Filter by "todos" → only deployment A returned.
	filtered, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SourceSlugs:      []string{"todos"},
	})
	require.NoError(t, err, "list deployments filtered by todos")
	ids := make(map[string]bool, len(filtered.Items))
	for _, item := range filtered.Items {
		ids[item.ID] = true
	}
	require.True(t, ids[depA.Deployment.ID], "deployment A should match filter for todos")
	require.False(t, ids[depB.Deployment.ID], "deployment B should not match filter for todos")

	// Filter with both slugs (OR semantics) → both deployments returned.
	both, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SourceSlugs:      []string{"todos", "pets"},
	})
	require.NoError(t, err, "list deployments filtered by todos OR pets")
	ids = make(map[string]bool, len(both.Items))
	for _, item := range both.Items {
		ids[item.ID] = true
	}
	require.True(t, ids[depA.Deployment.ID], "deployment A should match OR filter")
	require.True(t, ids[depB.Deployment.ID], "deployment B should match OR filter")

	// Filter by a slug that doesn't exist → empty result.
	empty, err := ti.service.ListDeployments(ctx, &gen.ListDeploymentsPayload{
		Cursor:           nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		SourceSlugs:      []string{"does-not-exist"},
	})
	require.NoError(t, err, "list deployments filtered by unknown slug")
	for _, item := range empty.Items {
		require.NotEqual(t, depA.Deployment.ID, item.ID, "no matching slug should hide deployment A")
		require.NotEqual(t, depB.Deployment.ID, item.ID, "no matching slug should hide deployment B")
	}
}

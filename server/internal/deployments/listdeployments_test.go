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
	require.GreaterOrEqual(t, foundDeployment.AssetCount, int64(1), "asset count should be at least 1")
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
				require.GreaterOrEqual(t, item.AssetCount, int64(1), "asset count should be at least 1 for deployment %s", dep.Deployment.ID)
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

package deployments_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDeploymentsService_GetActiveDeployment_Success(t *testing.T) {
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

	// Create first deployment
	first, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-get-active-deployment-first",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "test-doc-1",
				Slug:    "test-doc-1",
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

	// Test GetActiveDeployment after first deployment
	result, err := ti.service.GetActiveDeployment(ctx, &gen.GetActiveDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get active deployment")
	require.NotNil(t, result.Deployment, "deployment should not be nil")
	require.Equal(t, first.Deployment.ID, result.Deployment.ID, "should return first deployment as active")

	// Upload another asset for second deployment
	bs2 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares2, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs2.Len()),
	}, io.NopCloser(bs2))
	require.NoError(t, err, "upload second openapi v3 asset")

	// Create second deployment
	second, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-get-active-deployment-second",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares2.Asset.ID,
				Name:    "test-doc-2",
				Slug:    "test-doc-2",
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

	// Test GetActiveDeployment after second deployment
	result2, err := ti.service.GetActiveDeployment(ctx, &gen.GetActiveDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get active deployment after second")
	require.NotNil(t, result2.Deployment, "deployment should not be nil")
	require.Equal(t, second.Deployment.ID, result2.Deployment.ID, "should return second deployment as active")

	// Verify deployment details are complete
	require.NotEmpty(t, result2.Deployment.ID, "deployment ID should not be empty")
	require.NotEmpty(t, result2.Deployment.Status, "deployment status should not be empty")
	require.NotEmpty(t, result2.Deployment.CreatedAt, "deployment created at should not be empty")
	require.NotEmpty(t, result2.Deployment.OrganizationID, "deployment organization ID should not be empty")
	require.NotEmpty(t, result2.Deployment.ProjectID, "deployment project ID should not be empty")
	require.NotEmpty(t, result2.Deployment.UserID, "deployment user ID should not be empty")
	require.NotNil(t, result2.Deployment.IdempotencyKey, "deployment idempotency key should not be nil")
	require.Len(t, result2.Deployment.Openapiv3Assets, 1, "should have one openapi asset")
	require.Equal(t, "test-doc-2", result2.Deployment.Openapiv3Assets[0].Name, "should have correct asset name")
}

func TestDeploymentsService_GetActiveDeployment_NoDeployments(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test GetActiveDeployment when no deployments exist
	result, err := ti.service.GetActiveDeployment(ctx, &gen.GetActiveDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "should not error when no deployments exist")
	require.Nil(t, result.Deployment, "deployment should be nil when no deployments exist")
}

func TestDeploymentsService_GetActiveDeployment_Unauthorized(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	_, ti := newTestDeploymentService(t, assetStorage)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.GetActiveDeployment(ctx, &gen.GetActiveDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestDeploymentsService_GetActiveDeployment_WithExternalFields(t *testing.T) {
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

	// Create deployment with external fields
	externalID := "ext-active-123"
	externalURL := "https://example.com/active-deployment"
	githubRepo := "owner/active-repo"
	githubPr := "100"
	githubSha := "def456"

	created, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-get-active-deployment-external",
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
		GithubRepo:       &githubRepo,
		GithubPr:         &githubPr,
		GithubSha:        &githubSha,
		ExternalID:       &externalID,
		ExternalURL:      &externalURL,
	})
	require.NoError(t, err, "create deployment with external fields")

	// Test GetActiveDeployment
	result, err := ti.service.GetActiveDeployment(ctx, &gen.GetActiveDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get active deployment")
	require.NotNil(t, result.Deployment, "deployment should not be nil")
	require.Equal(t, created.Deployment.ID, result.Deployment.ID, "should return created deployment")

	// Verify external fields are preserved
	require.Equal(t, &externalID, result.Deployment.ExternalID, "external ID should match")
	require.Equal(t, &externalURL, result.Deployment.ExternalURL, "external URL should match")
	require.Equal(t, &githubRepo, result.Deployment.GithubRepo, "github repo should match")
	require.Equal(t, &githubPr, result.Deployment.GithubPr, "github PR should match")
	require.Equal(t, &githubSha, result.Deployment.GithubSha, "github SHA should match")
}

func TestDeploymentsService_GetActiveDeployment_OrderingByCreationTime(t *testing.T) {
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

	// Create multiple deployments in sequence
	_, err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-ordering-first",
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

	_, err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-ordering-second",
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

	third, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-ordering-third",
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

	// GetActiveDeployment should return the most recently created (third) deployment
	result, err := ti.service.GetActiveDeployment(ctx, &gen.GetActiveDeploymentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get active deployment")
	require.NotNil(t, result.Deployment, "deployment should not be nil")
	require.Equal(t, third.Deployment.ID, result.Deployment.ID, "should return the third (most recent) deployment")
	require.Equal(t, "third-doc", result.Deployment.Openapiv3Assets[0].Name, "should have the correct asset name from third deployment")
}

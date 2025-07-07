package deployments_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	pkggen "github.com/speakeasy-api/gram/server/gen/packages"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDeploymentsService_GetDeployment_Success(t *testing.T) {
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
		IdempotencyKey: "test-get-deployment",
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

	// Test GetDeployment
	result, err := ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{
		ID:               created.Deployment.ID,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get deployment")

	// Verify response
	require.Equal(t, created.Deployment.ID, result.ID, "deployment ID mismatch")
	require.Equal(t, created.Deployment.Status, result.Status, "deployment status mismatch")
	require.Equal(t, created.Deployment.CreatedAt, result.CreatedAt, "deployment created at mismatch")
	require.Equal(t, created.Deployment.OrganizationID, result.OrganizationID, "deployment organization ID mismatch")
	require.Equal(t, created.Deployment.ProjectID, result.ProjectID, "deployment project ID mismatch")
	require.Equal(t, created.Deployment.UserID, result.UserID, "deployment user ID mismatch")
	require.Equal(t, created.Deployment.IdempotencyKey, result.IdempotencyKey, "deployment idempotency key mismatch")
	require.Equal(t, created.Deployment.ExternalID, result.ExternalID, "deployment external ID mismatch")
	require.Equal(t, created.Deployment.ExternalURL, result.ExternalURL, "deployment external URL mismatch")
	require.Equal(t, created.Deployment.GithubRepo, result.GithubRepo, "deployment github repo mismatch")
	require.Equal(t, created.Deployment.GithubPr, result.GithubPr, "deployment github PR mismatch")
	require.Equal(t, created.Deployment.GithubSha, result.GithubSha, "deployment github SHA mismatch")
	require.Len(t, result.Openapiv3Assets, 1, "expected 1 openapi asset")
	require.Equal(t, "test-doc", result.Openapiv3Assets[0].Name, "unexpected asset name")
	require.Empty(t, result.Packages, "expected 0 packages")
}

func TestDeploymentsService_GetDeployment_WithPackages(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Create a package in another project to avoid circular dependency
	otherCtx := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)

	// Upload asset for package creation
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	packageAsset, err := ti.assets.UploadOpenAPIv3(otherCtx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload package asset")

	// Create deployment for package
	packageDep, err := ti.service.CreateDeployment(otherCtx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-package-deployment",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: packageAsset.Asset.ID,
				Name:    "package-doc",
				Slug:    "package-doc",
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
	require.NoError(t, err, "create package deployment")

	// Create and publish package
	_, err = ti.packages.CreatePackage(otherCtx, &pkggen.CreatePackagePayload{
		Name:             "test-package",
		Title:            "Test Package",
		Summary:          "Test Package Summary",
		Description:      nil,
		URL:              nil,
		Keywords:         nil,
		ImageAssetID:     nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "create package")

	_, err = ti.packages.Publish(otherCtx, &pkggen.PublishPayload{
		ProjectSlugInput: nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		Name:             "test-package",
		Version:          "1.0.0",
		DeploymentID:     packageDep.Deployment.ID,
		Visibility:       "public",
	})
	require.NoError(t, err, "publish package")

	// Create deployment with package in main project
	created, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-get-deployment-with-package",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Packages: []*gen.AddDeploymentPackageForm{
			{
				Name:    "test-package",
				Version: &[]string{"1.0.0"}[0],
			},
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment with package")

	// Test GetDeployment
	result, err := ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{
		ID:               created.Deployment.ID,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get deployment")

	// Verify response includes package
	require.Equal(t, created.Deployment.ID, result.ID, "deployment ID mismatch")
	require.Empty(t, result.Openapiv3Assets, "expected 0 openapi assets")
	require.Len(t, result.Packages, 1, "expected 1 package")
	require.Equal(t, "test-package", result.Packages[0].Name, "unexpected package name")
	require.Equal(t, "1.0.0", result.Packages[0].Version, "unexpected package version")
}

func TestDeploymentsService_GetDeployment_InvalidID(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with invalid UUID
	_, err := ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{
		ID:               "invalid-uuid",
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "error parsing deployment id")
}

func TestDeploymentsService_GetDeployment_NotFound(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test with non-existent deployment ID
	nonExistentID := uuid.New().String()
	_, err := ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{
		ID:               nonExistentID,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestDeploymentsService_GetDeployment_Unauthorized(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	_, ti := newTestDeploymentService(t, assetStorage)

	// Test with context that has no auth context
	ctx := t.Context()
	deploymentID := uuid.New().String()

	_, err := ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{
		ID:               deploymentID,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestDeploymentsService_GetDeployment_WithExternalFields(t *testing.T) {
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
	externalID := "ext-123"
	externalURL := "https://example.com/deployment"
	githubRepo := "owner/repo"
	githubPr := "42"
	githubSha := "abc123"

	created, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-get-deployment-external",
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
		GithubRepo:       &githubRepo,
		GithubPr:         &githubPr,
		GithubSha:        &githubSha,
		ExternalID:       &externalID,
		ExternalURL:      &externalURL,
	})
	require.NoError(t, err, "create deployment")

	// Test GetDeployment
	result, err := ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{
		ID:               created.Deployment.ID,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "get deployment")

	// Verify external fields
	require.Equal(t, &externalID, result.ExternalID, "external ID mismatch")
	require.Equal(t, &externalURL, result.ExternalURL, "external URL mismatch")
	require.Equal(t, &githubRepo, result.GithubRepo, "github repo mismatch")
	require.Equal(t, &githubPr, result.GithubPr, "github PR mismatch")
	require.Equal(t, &githubSha, result.GithubSha, "github SHA mismatch")
}

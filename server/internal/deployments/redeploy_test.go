package deployments_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	pkggen "github.com/speakeasy-api/gram/server/gen/packages"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestDeploymentsService_Redeploy_BasicCloning(t *testing.T) {
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

	// Create initial deployment
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-initial-deployment",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "initial-doc",
				Slug:    "initial-doc",
			},
		},
		Functions:        []*gen.AddFunctionsForm{},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       new("test/repo"),
		GithubPr:         new("123"),
		GithubSha:        new("abc123"),
		ExternalID:       new("ext-123"),
		ExternalURL:      new("https://example.com"),
	})
	require.NoError(t, err, "create initial deployment")
	require.Equal(t, "completed", initial.Deployment.Status, "initial deployment status is not completed")

	// Redeploy the deployment (clone without modifications)
	redeployed, err := ti.service.Redeploy(ctx, &gen.RedeployPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     initial.Deployment.ID,
	})
	require.NoError(t, err, "redeploy deployment")

	// Verify redeployed deployment is different but has same content
	require.NotEqual(t, initial.Deployment.ID, redeployed.Deployment.ID, "redeployed deployment should have different ID")
	require.Equal(t, "completed", redeployed.Deployment.Status, "redeployed deployment status is not completed")
	require.Equal(t, initial.Deployment.ID, *redeployed.Deployment.ClonedFrom, "redeployed deployment should reference original")

	// Verify assets are identical
	require.Len(t, redeployed.Deployment.Openapiv3Assets, 1, "expected 1 openapi asset")
	require.Equal(t, "initial-doc", redeployed.Deployment.Openapiv3Assets[0].Name, "unexpected asset name")
	require.Equal(t, "initial-doc", string(redeployed.Deployment.Openapiv3Assets[0].Slug), "unexpected asset slug")

	// Verify metadata fields are cloned
	require.Equal(t, initial.Deployment.GithubRepo, redeployed.Deployment.GithubRepo, "github repo should be cloned")
	require.Equal(t, initial.Deployment.GithubPr, redeployed.Deployment.GithubPr, "github pr should be cloned")
	require.Equal(t, initial.Deployment.GithubSha, redeployed.Deployment.GithubSha, "github sha should be cloned")
	require.Equal(t, initial.Deployment.ExternalID, redeployed.Deployment.ExternalID, "external id should be cloned")
	require.Equal(t, initial.Deployment.ExternalURL, redeployed.Deployment.ExternalURL, "external url should be cloned")

	// Verify tools were generated for redeployed deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(redeployed.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.Len(t, tools, 5, "expected 5 tools")
}

func TestDeploymentsService_Redeploy_WithOpenAPIv3(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload multiple OpenAPI assets
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

	// Create initial deployment with multiple assets
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-multi-asset-deployment",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "todo-doc",
				Slug:    "todo-doc",
			},
			{
				AssetID: ares2.Asset.ID,
				Name:    "petstore-doc",
				Slug:    "petstore-doc",
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
	require.NoError(t, err, "create initial deployment")
	require.Equal(t, "completed", initial.Deployment.Status, "initial deployment status is not completed")
	require.Len(t, initial.Deployment.Openapiv3Assets, 2, "expected 2 assets in initial deployment")

	// Redeploy the deployment
	redeployed, err := ti.service.Redeploy(ctx, &gen.RedeployPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     initial.Deployment.ID,
	})
	require.NoError(t, err, "redeploy deployment")

	// Verify all assets are cloned
	require.NotEqual(t, initial.Deployment.ID, redeployed.Deployment.ID, "redeployed deployment should have different ID")
	require.Equal(t, "completed", redeployed.Deployment.Status, "redeployed deployment status is not completed")
	require.Len(t, redeployed.Deployment.Openapiv3Assets, 2, "expected 2 openapi assets")

	// Verify asset names are preserved
	assetNames := lo.Map(redeployed.Deployment.Openapiv3Assets, func(a *types.OpenAPIv3DeploymentAsset, _ int) string {
		return a.Name
	})
	require.ElementsMatch(t, assetNames, []string{"todo-doc", "petstore-doc"}, "unexpected asset names")
}

func TestDeploymentsService_Redeploy_WithPackages(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Create external package
	otherCtx := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	packageAsset, err := ti.assets.UploadOpenAPIv3(otherCtx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload package asset")

	packageDep, err := ti.service.CreateDeployment(otherCtx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-package-deployment",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: packageAsset.Asset.ID,
				Name:    "package-doc",
				Slug:    "package-doc",
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
	require.NoError(t, err, "create package deployment")

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

	ver, err := ti.packages.Publish(otherCtx, &pkggen.PublishPayload{
		ProjectSlugInput: nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		Name:             "test-package",
		Version:          "1.0.0",
		DeploymentID:     packageDep.Deployment.ID,
		Visibility:       "public",
	})
	require.NoError(t, err, "publish package")

	// Upload asset for main project
	bs1 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares1, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs1.Len()),
	}, io.NopCloser(bs1))
	require.NoError(t, err, "upload asset")

	// Create deployment with asset and package
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-deployment-with-package",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "main-doc",
				Slug:    "main-doc",
			},
		},
		Functions: []*gen.AddFunctionsForm{},
		Packages: []*gen.AddDeploymentPackageForm{
			{
				Name:    "test-package",
				Version: new("1.0.0"),
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
	require.NoError(t, err, "create initial deployment")
	require.Equal(t, "completed", initial.Deployment.Status, "initial deployment status is not completed")
	require.Len(t, initial.Deployment.Openapiv3Assets, 1, "expected 1 asset in initial deployment")
	require.Len(t, initial.Deployment.Packages, 1, "expected 1 package in initial deployment")

	// Redeploy the deployment
	redeployed, err := ti.service.Redeploy(ctx, &gen.RedeployPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     initial.Deployment.ID,
	})
	require.NoError(t, err, "redeploy deployment")

	// Verify both assets and packages are cloned
	require.NotEqual(t, initial.Deployment.ID, redeployed.Deployment.ID, "redeployed deployment should have different ID")
	require.Equal(t, "completed", redeployed.Deployment.Status, "redeployed deployment status is not completed")
	require.Len(t, redeployed.Deployment.Openapiv3Assets, 1, "expected 1 openapi asset")
	require.Len(t, redeployed.Deployment.Packages, 1, "expected 1 package")

	// Verify asset details
	require.Equal(t, "main-doc", redeployed.Deployment.Openapiv3Assets[0].Name, "unexpected asset name")

	// Verify package details
	require.Equal(t, "test-package", redeployed.Deployment.Packages[0].Name, "unexpected package name")
	require.Equal(t, ver.Version.Semver, redeployed.Deployment.Packages[0].Version, "unexpected package version")
}

func TestDeploymentsService_Redeploy_Validation(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	t.Run("missing deployment ID", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.Redeploy(ctx, &gen.RedeployPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			DeploymentID:     "",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "deployment id is required")
	})

	t.Run("invalid deployment ID", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.Redeploy(ctx, &gen.RedeployPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			DeploymentID:     "invalid-uuid",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid deployment id")
	})

	t.Run("nonexistent deployment ID", func(t *testing.T) {
		t.Parallel()

		nonexistentID := uuid.New().String()
		_, err := ti.service.Redeploy(ctx, &gen.RedeployPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			DeploymentID:     nonexistentID,
		})

		require.Error(t, err)
		// The exact error message depends on the cloneDeployment implementation
		// but it should fail when trying to clone a nonexistent deployment
	})
}

func TestDeploymentsService_Redeploy_ComplexDeployment(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Create external package
	otherCtx := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	packageAsset, err := ti.assets.UploadOpenAPIv3(otherCtx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload package asset")

	packageDep, err := ti.service.CreateDeployment(otherCtx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-package-deployment",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: packageAsset.Asset.ID,
				Name:    "package-doc",
				Slug:    "package-doc",
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
	require.NoError(t, err, "create package deployment")

	_, err = ti.packages.CreatePackage(otherCtx, &pkggen.CreatePackagePayload{
		Name:             "external-package",
		Title:            "External Package",
		Summary:          "External Package Summary",
		Description:      nil,
		URL:              nil,
		Keywords:         nil,
		ImageAssetID:     nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "create external package")

	_, err = ti.packages.Publish(otherCtx, &pkggen.PublishPayload{
		ProjectSlugInput: nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		Name:             "external-package",
		Version:          "1.0.0",
		DeploymentID:     packageDep.Deployment.ID,
		Visibility:       "public",
	})
	require.NoError(t, err, "publish external package")

	// Upload multiple assets for main project
	bs1 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares1, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs1.Len()),
	}, io.NopCloser(bs1))
	require.NoError(t, err, "upload first asset")

	bs2 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares2, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs2.Len()),
	}, io.NopCloser(bs2))
	require.NoError(t, err, "upload second asset")

	// Create complex deployment with multiple assets and packages
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-complex-deployment",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "todo-api",
				Slug:    "todo-api",
			},
			{
				AssetID: ares2.Asset.ID,
				Name:    "petstore-api",
				Slug:    "petstore-api",
			},
		},
		Functions: []*gen.AddFunctionsForm{},
		Packages: []*gen.AddDeploymentPackageForm{
			{
				Name:    "external-package",
				Version: new("1.0.0"),
			},
		},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       new("org/complex-repo"),
		GithubPr:         new("456"),
		GithubSha:        new("def456"),
		ExternalID:       new("complex-ext-789"),
		ExternalURL:      new("https://complex.example.com"),
	})
	require.NoError(t, err, "create complex deployment")
	require.Equal(t, "completed", initial.Deployment.Status, "initial deployment status is not completed")
	require.Len(t, initial.Deployment.Openapiv3Assets, 2, "expected 2 assets in initial deployment")
	require.Len(t, initial.Deployment.Packages, 1, "expected 1 package in initial deployment")

	// Redeploy the complex deployment
	redeployed, err := ti.service.Redeploy(ctx, &gen.RedeployPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     initial.Deployment.ID,
	})
	require.NoError(t, err, "redeploy complex deployment")

	// Verify everything is cloned correctly
	require.NotEqual(t, initial.Deployment.ID, redeployed.Deployment.ID, "redeployed deployment should have different ID")
	require.Equal(t, "completed", redeployed.Deployment.Status, "redeployed deployment status is not completed")
	require.Equal(t, initial.Deployment.ID, *redeployed.Deployment.ClonedFrom, "redeployed deployment should reference original")

	// Verify assets
	require.Len(t, redeployed.Deployment.Openapiv3Assets, 2, "expected 2 openapi assets")
	assetNames := lo.Map(redeployed.Deployment.Openapiv3Assets, func(a *types.OpenAPIv3DeploymentAsset, _ int) string {
		return a.Name
	})
	require.ElementsMatch(t, assetNames, []string{"todo-api", "petstore-api"}, "unexpected asset names")

	// Verify packages
	require.Len(t, redeployed.Deployment.Packages, 1, "expected 1 package")
	require.Equal(t, "external-package", redeployed.Deployment.Packages[0].Name, "unexpected package name")

	// Verify metadata is cloned
	require.Equal(t, initial.Deployment.GithubRepo, redeployed.Deployment.GithubRepo, "github repo should be cloned")
	require.Equal(t, initial.Deployment.GithubPr, redeployed.Deployment.GithubPr, "github pr should be cloned")
	require.Equal(t, initial.Deployment.GithubSha, redeployed.Deployment.GithubSha, "github sha should be cloned")
	require.Equal(t, initial.Deployment.ExternalID, redeployed.Deployment.ExternalID, "external id should be cloned")
	require.Equal(t, initial.Deployment.ExternalURL, redeployed.Deployment.ExternalURL, "external url should be cloned")
}

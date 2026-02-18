package deployments_test

import (
	"bytes"
	"io"
	"testing"
	"time"

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

func TestDeploymentsService_Evolve_InitialDeployment(t *testing.T) {
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

	// Test evolving when no previous deployments exist (should create initial deployment)
	result, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "initial-doc",
				Slug:    "initial-doc",
			},
		},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve initial deployment")

	require.NotEqual(t, uuid.Nil.String(), result.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", result.Deployment.Status, "deployment status is not completed")
	require.Len(t, result.Deployment.Openapiv3Assets, 1, "expected 1 openapi asset")
	require.Equal(t, "initial-doc", result.Deployment.Openapiv3Assets[0].Name, "unexpected asset name")
	require.Equal(t, "initial-doc", string(result.Deployment.Openapiv3Assets[0].Slug), "unexpected asset slug")

	// Verify tools were generated
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(result.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.Len(t, tools, 5, "expected 5 tools")
}

func TestDeploymentsService_Evolve_NonBlocking(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	result, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		NonBlocking:      new(true),
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "initial-doc",
				Slug:    "initial-doc",
			},
		},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment in non-blocking mode")
	require.NotEqual(t, uuid.Nil.String(), result.Deployment.ID, "deployment ID is nil")
	// Non-blocking returns immediately but the workflow runs concurrently, so
	// the status may be "created", "pending", or "completed" depending on timing.
	require.Contains(t, []string{"created", "pending", "completed"}, result.Deployment.Status,
		"deployment status should be a valid early state")
	require.Len(t, result.Deployment.Openapiv3Assets, 1, "expected 1 openapi asset")
	require.Equal(t, "initial-doc", result.Deployment.Openapiv3Assets[0].Name, "unexpected asset name")

	// Poll until the non-blocking deployment completes
	var finalStatus string
	require.Eventually(t, func() bool {
		dep, err := ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{
			ID:               result.Deployment.ID,
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		if err != nil {
			return false
		}
		finalStatus = dep.Status
		return finalStatus == "completed" || finalStatus == "failed"
	}, 10*time.Second, 100*time.Millisecond, "non-blocking deployment should eventually complete")

	require.Equal(t, "completed", finalStatus, "non-blocking deployment should complete successfully")
}

func TestDeploymentsService_Evolve_UpsertOpenAPIv3(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Create initial deployment
	bs1 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares1, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs1.Len()),
	}, io.NopCloser(bs1))
	require.NoError(t, err, "upload first openapi v3 asset")

	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-initial-deployment-upsert-assets",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "initial-doc",
				Slug:    "initial-doc",
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

	// Upload second OpenAPI asset
	bs2 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares2, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs2.Len()),
	}, io.NopCloser(bs2))
	require.NoError(t, err, "upload second openapi v3 asset")

	// Evolve deployment to add second asset
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares2.Asset.ID,
				Name:    "second-doc",
				Slug:    "second-doc",
			},
		},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "evolved deployment status is not completed")
	require.Len(t, evolved.Deployment.Openapiv3Assets, 2, "expected 2 openapi assets")

	// Verify both assets are present
	assetNames := lo.Map(evolved.Deployment.Openapiv3Assets, func(a *types.OpenAPIv3DeploymentAsset, _ int) string {
		return a.Name
	})
	require.ElementsMatch(t, assetNames, []string{"initial-doc", "second-doc"}, "unexpected asset names")
}

func TestDeploymentsService_Evolve_UpsertBadAssets(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Create initial deployment
	bs1 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares1, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs1.Len()),
	}, io.NopCloser(bs1))
	require.NoError(t, err, "upload first openapi v3 asset")

	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-initial-deployment-upsert-bad-assets",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "initial-doc",
				Slug:    "initial-doc",
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

	expectedToolCount := initial.Deployment.Openapiv3ToolCount
	require.NotZero(t, expectedToolCount, "initial deployment has incorrect tool count")

	// Upload second OpenAPI asset
	bs2 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/invalid.yaml"))
	ares2, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs2.Len()),
	}, io.NopCloser(bs2))
	require.NoError(t, err, "upload second openapi v3 asset")

	// Evolve deployment to add second asset
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares2.Asset.ID,
				Name:    "second-doc",
				Slug:    "second-doc",
			},
		},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "failed", evolved.Deployment.Status, "evolved deployment status is not completed")
	require.Equal(t, expectedToolCount, evolved.Deployment.Openapiv3ToolCount, "evolved deployment has incorrect openapi tool count")
	require.Len(t, evolved.Deployment.Openapiv3Assets, 2, "expected 2 openapi assets")

	// Verify both assets are present
	assetNames := lo.Map(evolved.Deployment.Openapiv3Assets, func(a *types.OpenAPIv3DeploymentAsset, _ int) string {
		return a.Name
	})
	require.ElementsMatch(t, assetNames, []string{"initial-doc", "second-doc"}, "unexpected asset names")
}

func TestDeploymentsService_Evolve_ExcludeOpenAPIv3(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload two OpenAPI assets
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

	// Create initial deployment with both assets
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-initial-deployment-exclude-assets",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "doc-1",
				Slug:    "doc-1",
			},
			{
				AssetID: ares2.Asset.ID,
				Name:    "doc-2",
				Slug:    "doc-2",
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
	require.Len(t, initial.Deployment.Openapiv3Assets, 2, "expected 2 assets in initial deployment")

	// Evolve deployment to exclude first asset
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{ares1.Asset.ID},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Len(t, evolved.Deployment.Openapiv3Assets, 1, "expected 1 openapi asset after exclusion")
	require.Equal(t, "doc-2", evolved.Deployment.Openapiv3Assets[0].Name, "wrong asset remained")
}

func TestDeploymentsService_Evolve_ExcludeAllOpenAPIv3(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload two OpenAPI assets
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

	// Create initial deployment with both assets
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-initial-deployment-all-excluded",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "doc-1",
				Slug:    "doc-1",
			},
			{
				AssetID: ares2.Asset.ID,
				Name:    "doc-2",
				Slug:    "doc-2",
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
	require.Len(t, initial.Deployment.Openapiv3Assets, 2, "expected 2 assets in initial deployment")

	// Verify initial deployment has tools
	repo := testrepo.New(ti.conn)
	initialTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(initial.Deployment.ID))
	require.NoError(t, err, "list initial deployment tools")
	require.NotEmpty(t, initialTools, "expected tools in initial deployment")

	// Evolve deployment to exclude all assets
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{ares1.Asset.ID, ares2.Asset.ID},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Empty(t, evolved.Deployment.Openapiv3Assets, "expected 0 openapi assets after excluding all")

	// Verify no tools remain in the deployment
	evolvedTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list evolved deployment tools")
	require.Empty(t, evolvedTools, "expected no tools after excluding all assets")
}

func TestDeploymentsService_Evolve_UpsertPackages(t *testing.T) {
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
		IdempotencyKey: "test-package-deployment-upsert-package",
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

	// Create package
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

	// Publish package
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

	// Now test evolving to add this package to our main project
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:       []*gen.AddFunctionsForm{},
		UpsertPackages: []*gen.AddPackageForm{
			{
				Name:    "test-package",
				Version: new("1.0.0"),
			},
		},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment with package")

	require.NotEqual(t, uuid.Nil.String(), evolved.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Len(t, evolved.Deployment.Packages, 1, "expected 1 package")
	require.Equal(t, "test-package", evolved.Deployment.Packages[0].Name, "unexpected package name")
	require.Equal(t, ver.Version.Semver, evolved.Deployment.Packages[0].Version, "unexpected package version")
}

func TestDeploymentsService_Evolve_ExcludePackages(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Create packages in different projects (since each project can only have one package)
	otherCtx1 := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)
	otherCtx2 := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)

	// Upload assets for package creation
	bs1 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	packageAsset1, err := ti.assets.UploadOpenAPIv3(otherCtx1, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs1.Len()),
	}, io.NopCloser(bs1))
	require.NoError(t, err, "upload package asset 1")

	bs2 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	packageAsset2, err := ti.assets.UploadOpenAPIv3(otherCtx2, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs2.Len()),
	}, io.NopCloser(bs2))
	require.NoError(t, err, "upload package asset 2")

	// Create deployments for packages
	packageDep1, err := ti.service.CreateDeployment(otherCtx1, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-package-deployment-1",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: packageAsset1.Asset.ID,
				Name:    "package-doc-1",
				Slug:    "package-doc-1",
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
	require.NoError(t, err, "create package deployment 1")

	packageDep2, err := ti.service.CreateDeployment(otherCtx2, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-package-deployment-2",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: packageAsset2.Asset.ID,
				Name:    "package-doc-2",
				Slug:    "package-doc-2",
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
	require.NoError(t, err, "create package deployment 2")

	// Create packages in separate projects
	_, err = ti.packages.CreatePackage(otherCtx1, &pkggen.CreatePackagePayload{
		Name:             "test-package-1",
		Title:            "Test Package 1",
		Summary:          "Test Package 1 Summary",
		Description:      nil,
		URL:              nil,
		Keywords:         nil,
		ImageAssetID:     nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "create package 1")

	_, err = ti.packages.CreatePackage(otherCtx2, &pkggen.CreatePackagePayload{
		Name:             "test-package-2",
		Title:            "Test Package 2",
		Summary:          "Test Package 2 Summary",
		Description:      nil,
		URL:              nil,
		Keywords:         nil,
		ImageAssetID:     nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "create package 2")

	// Publish both packages
	_, err = ti.packages.Publish(otherCtx1, &pkggen.PublishPayload{
		ProjectSlugInput: nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		Name:             "test-package-1",
		Version:          "1.0.0",
		DeploymentID:     packageDep1.Deployment.ID,
		Visibility:       "public",
	})
	require.NoError(t, err, "publish package 1")

	_, err = ti.packages.Publish(otherCtx2, &pkggen.PublishPayload{
		ProjectSlugInput: nil,
		ApikeyToken:      nil,
		SessionToken:     nil,
		Name:             "test-package-2",
		Version:          "1.0.0",
		DeploymentID:     packageDep2.Deployment.ID,
		Visibility:       "public",
	})
	require.NoError(t, err, "publish package 2")

	// Create initial deployment with both packages
	initial, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:       []*gen.AddFunctionsForm{},
		UpsertPackages: []*gen.AddPackageForm{
			{
				Name:    "test-package-1",
				Version: new("1.0.0"),
			},
			{
				Name:    "test-package-2",
				Version: new("1.0.0"),
			},
		},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "create initial deployment with packages")
	require.Len(t, initial.Deployment.Packages, 2, "expected 2 packages in initial deployment")

	// Find the package ID to exclude
	var excludePackageID string
	for _, pkg := range initial.Deployment.Packages {
		if pkg.Name == "test-package-1" {
			excludePackageID = pkg.ID
			break
		}
	}
	require.NotEmpty(t, excludePackageID, "could not find package 1 ID")

	// Evolve deployment to exclude first package
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{excludePackageID},
	})
	require.NoError(t, err, "evolve deployment")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Len(t, evolved.Deployment.Packages, 1, "expected 1 package after exclusion")
	require.Equal(t, "test-package-2", evolved.Deployment.Packages[0].Name, "wrong package remained")
}

func TestDeploymentsService_Evolve_Validation(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	t.Run("no operations specified", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
			ApikeyToken:            nil,
			SessionToken:           nil,
			ProjectSlugInput:       nil,
			DeploymentID:           nil,
			UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
			UpsertFunctions:        []*gen.AddFunctionsForm{},
			UpsertPackages:         []*gen.AddPackageForm{},
			ExcludeOpenapiv3Assets: []string{},
			ExcludeFunctions:       []string{},
			ExcludePackages:        []string{},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "at least one asset, package, or external mcp to upsert or exclude is required")
	})

	t.Run("invalid asset ID to upsert", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			DeploymentID:     nil,
			UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
				{
					AssetID: "invalid-uuid",
					Name:    "test-doc",
					Slug:    "test-doc",
				},
			},
			UpsertFunctions:        []*gen.AddFunctionsForm{},
			UpsertPackages:         []*gen.AddPackageForm{},
			ExcludeOpenapiv3Assets: []string{},
			ExcludeFunctions:       []string{},
			ExcludePackages:        []string{},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "error parsing openapiv3 asset id to upsert")
	})

	t.Run("invalid asset ID to exclude", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
			ApikeyToken:            nil,
			SessionToken:           nil,
			ProjectSlugInput:       nil,
			DeploymentID:           nil,
			UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
			UpsertFunctions:        []*gen.AddFunctionsForm{},
			UpsertPackages:         []*gen.AddPackageForm{},
			ExcludeOpenapiv3Assets: []string{"invalid-uuid"},
			ExcludeFunctions:       []string{},
			ExcludePackages:        []string{},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "error parsing openapiv3 asset id to exclude")
	})

	t.Run("invalid package ID to exclude", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
			ApikeyToken:            nil,
			SessionToken:           nil,
			ProjectSlugInput:       nil,
			DeploymentID:           nil,
			UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
			UpsertFunctions:        []*gen.AddFunctionsForm{},
			UpsertPackages:         []*gen.AddPackageForm{},
			ExcludeOpenapiv3Assets: []string{},
			ExcludeFunctions:       []string{},
			ExcludePackages:        []string{"invalid-uuid"},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "error parsing package id to exclude")
	})

	t.Run("invalid package version", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
			ApikeyToken:           nil,
			SessionToken:          nil,
			ProjectSlugInput:      nil,
			DeploymentID:          nil,
			UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
			UpsertFunctions:       []*gen.AddFunctionsForm{},
			UpsertPackages: []*gen.AddPackageForm{
				{
					Name:    "test-package",
					Version: new("invalid-version"),
				},
			},
			ExcludeOpenapiv3Assets: []string{},
			ExcludeFunctions:       []string{},
			ExcludePackages:        []string{},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "error parsing semver")
	})

	t.Run("circular package dependency", func(t *testing.T) {
		t.Parallel()

		// Upload asset for initial deployment
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
		dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
			IdempotencyKey: "test-deployment-for-package-circular",
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
		require.NoError(t, err, "create initial deployment")

		// Create package in same project
		_, err = ti.packages.CreatePackage(ctx, &pkggen.CreatePackagePayload{
			Name:             "self-package",
			Title:            "Self Package",
			Summary:          "Self Package Summary",
			Description:      nil,
			URL:              nil,
			Keywords:         nil,
			ImageAssetID:     nil,
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err, "create package")

		// Publish package
		_, err = ti.packages.Publish(ctx, &pkggen.PublishPayload{
			ProjectSlugInput: nil,
			ApikeyToken:      nil,
			SessionToken:     nil,
			Name:             "self-package",
			Version:          "1.0.0",
			DeploymentID:     dep.Deployment.ID,
			Visibility:       "public",
		})
		require.NoError(t, err, "publish package")

		// Try to evolve to add the same project's package (should fail)
		_, err = ti.service.Evolve(ctx, &gen.EvolvePayload{
			ApikeyToken:           nil,
			SessionToken:          nil,
			ProjectSlugInput:      nil,
			DeploymentID:          nil,
			UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
			UpsertFunctions:       []*gen.AddFunctionsForm{},
			UpsertPackages: []*gen.AddPackageForm{
				{
					Name:    "self-package",
					Version: new("1.0.0"),
				},
			},
			ExcludeOpenapiv3Assets: []string{},
			ExcludeFunctions:       []string{},
			ExcludePackages:        []string{},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot add package to its own project: self-package")
	})
}

func TestDeploymentsService_Evolve_ComplexScenario(t *testing.T) {
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
		IdempotencyKey: "test-package-deployment-complex",
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

	// Upload assets for main project using different fixtures
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

	// Create initial deployment with first asset and external package
	initial, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "doc-1",
				Slug:    "doc-1",
			},
		},
		UpsertPackages: []*gen.AddPackageForm{
			{
				Name:    "external-package",
				Version: new("1.0.0"),
			},
		},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "create initial deployment")
	require.Len(t, initial.Deployment.Openapiv3Assets, 1, "expected 1 asset in initial")
	require.Len(t, initial.Deployment.Packages, 1, "expected 1 package in initial")

	// Complex evolution: add new asset, remove existing package, add new asset
	var excludePackageID string
	for _, pkg := range initial.Deployment.Packages {
		if pkg.Name == "external-package" {
			excludePackageID = pkg.ID
			break
		}
	}
	require.NotEmpty(t, excludePackageID, "could not find external package ID")

	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares2.Asset.ID,
				Name:    "doc-2",
				Slug:    "doc-2",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{excludePackageID},
	})
	require.NoError(t, err, "evolve deployment with complex changes")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Len(t, evolved.Deployment.Openapiv3Assets, 2, "expected 2 assets after evolution")
	require.Empty(t, evolved.Deployment.Packages, "expected 0 packages after evolution")

	// Verify asset names
	assetNames := lo.Map(evolved.Deployment.Openapiv3Assets, func(a *types.OpenAPIv3DeploymentAsset, _ int) string {
		return a.Name
	})
	require.ElementsMatch(t, assetNames, []string{"doc-1", "doc-2"}, "unexpected asset names")
}

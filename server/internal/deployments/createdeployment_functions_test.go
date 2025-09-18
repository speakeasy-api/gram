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
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestCreateDeployment_OnlyFunctions(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-only-functions",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{}, // No OpenAPI
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "only-functions",
				Slug:    "only-functions",
				Runtime: "nodejs:22",
			},
		},
		Packages:         []*gen.AddDeploymentPackageForm{}, // No packages
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment with only functions")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")
	require.Empty(t, dep.Deployment.Openapiv3Assets, "should have no OpenAPI assets")
	require.Len(t, dep.Deployment.FunctionsAssets, 1, "expected 1 functions file")
	require.Empty(t, dep.Deployment.Packages, "should have no packages")

	// Verify only function tools were created, no HTTP tools
	repo := testrepo.New(ti.conn)
	httpTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment HTTP tools")
	require.Empty(t, httpTools, "should have no HTTP tools")

	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "should have function tools")
}

func TestCreateDeployment_FunctionsWithManifestValidation(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Create a functions zip that contains a manifest matching one of our fixture manifests
	// In practice, this would involve creating a zip file with the manifest from manifest-todo.json
	// For this test, we'll use existing valid functions and verify tools are created

	// Upload functions file that should create tools based on its internal manifest
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-functions-manifest-validation",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "manifest-functions",
				Slug:    "manifest-functions",
				Runtime: "nodejs:22",
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
	require.NoError(t, err, "create deployment with functions that have manifest")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")
	require.Len(t, dep.Deployment.FunctionsAssets, 1, "expected 1 functions file")

	// Verify function tools were created from the manifest
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created from manifest")

	// Verify tool attributes match expected values from manifest parsing
	for _, tool := range functionTools {
		require.NotEmpty(t, tool.Name, "tool should have a name")
		require.NotEmpty(t, tool.Description, "tool should have a description")
		require.Equal(t, "nodejs:22", tool.Runtime, "tool should have correct runtime")
	}
}

func TestCreateDeployment_WithFunctions_ValidJS(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload JS functions file with todo manifest
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-js-functions",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "js-functions",
				Slug:    "js-functions",
				Runtime: "nodejs:22",
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
	require.NoError(t, err, "create deployment with JS functions")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")
	require.Len(t, dep.Deployment.FunctionsAssets, 1, "expected 1 functions file")
	require.Equal(t, "js-functions", dep.Deployment.FunctionsAssets[0].Name, "unexpected functions name")
	require.Equal(t, "nodejs:22", dep.Deployment.FunctionsAssets[0].Runtime, "unexpected runtime")

	// Verify function tools were created in database
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")

	// Verify all tools have correct runtime
	for _, tool := range functionTools {
		require.Equal(t, "nodejs:22", tool.Runtime, "tool runtime should match deployment runtime")
	}
}

func TestCreateDeployment_WithFunctions_ValidPython(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload Python functions file with petstore manifest
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-python-functions",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "python-functions",
				Slug:    "python-functions",
				Runtime: "python:3.12",
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
	require.NoError(t, err, "create deployment with Python functions")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")
	require.Len(t, dep.Deployment.FunctionsAssets, 1, "expected 1 functions file")
	require.Equal(t, "python-functions", dep.Deployment.FunctionsAssets[0].Name, "unexpected functions name")
	require.Equal(t, "python:3.12", dep.Deployment.FunctionsAssets[0].Runtime, "unexpected runtime")

	// Verify function tools were created in database
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")

	// Verify all tools have correct runtime
	for _, tool := range functionTools {
		require.Equal(t, "python:3.12", tool.Runtime, "tool runtime should match deployment runtime")
	}
}

func TestCreateDeployment_WithFunctions_ValidTypeScript(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload TypeScript functions file
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-typescript-functions",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "typescript-functions",
				Slug:    "typescript-functions",
				Runtime: "nodejs:22",
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
	require.NoError(t, err, "create deployment with TypeScript functions")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")
	require.Len(t, dep.Deployment.FunctionsAssets, 1, "expected 1 functions file")
	require.Equal(t, "typescript-functions", dep.Deployment.FunctionsAssets[0].Name, "unexpected functions name")
	require.Equal(t, "nodejs:22", dep.Deployment.FunctionsAssets[0].Runtime, "unexpected runtime")

	// Verify function tools were created in database
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")
}

func TestCreateDeployment_WithFunctions_MultipleFiles(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload multiple function files
	jsRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")
	pyRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-multiple-functions",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: jsRes.Asset.ID,
				Name:    "js-functions",
				Slug:    "js-functions",
				Runtime: "nodejs:22",
			},
			{
				AssetID: pyRes.Asset.ID,
				Name:    "py-functions",
				Slug:    "py-functions",
				Runtime: "python:3.12",
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
	require.NoError(t, err, "create deployment with multiple functions")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")
	require.Len(t, dep.Deployment.FunctionsAssets, 2, "expected 2 functions files")

	// Verify function names
	names := lo.Map(dep.Deployment.FunctionsAssets, func(f *types.DeploymentFunctions, _ int) string {
		return f.Name
	})
	require.ElementsMatch(t, names, []string{"js-functions", "py-functions"}, "unexpected function names")

	// Verify function tools were created in database
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")

	// Check that we have tools for both runtimes
	runtimes := lo.Map(functionTools, func(tool testrepo.FunctionToolDefinition, _ int) string {
		return tool.Runtime
	})
	require.Contains(t, runtimes, "nodejs:22", "expected nodejs:22 runtime tools")
	require.Contains(t, runtimes, "python:3.12", "expected python:3.12 runtime tools")
}

func TestCreateDeployment_WithFunctions_AndOpenAPI(t *testing.T) {
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

	// Upload functions file
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-functions-and-openapi",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "todo-api",
				Slug:    "todo-api",
			},
		},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "todo-functions",
				Slug:    "todo-functions",
				Runtime: "nodejs:22",
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
	require.NoError(t, err, "create deployment with functions and OpenAPI")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")
	require.Len(t, dep.Deployment.Openapiv3Assets, 1, "expected 1 OpenAPI asset")
	require.Len(t, dep.Deployment.FunctionsAssets, 1, "expected 1 functions file")

	// Verify both HTTP and function tools were created
	repo := testrepo.New(ti.conn)
	httpTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment HTTP tools")
	require.NotEmpty(t, httpTools, "expected HTTP tools to be created")

	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")
}

func TestCreateDeployment_WithFunctions_Idempotency(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	// Create same deployment multiple times with same idempotency key
	var deploymentIDs []string
	for i := 0; i < 3; i++ {
		dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
			IdempotencyKey:  "test-functions-idempotency",
			Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
			Functions: []*gen.AddFunctionsForm{
				{
					AssetID: fres.Asset.ID,
					Name:    "idempotent-functions",
					Slug:    "idempotent-functions",
					Runtime: "nodejs:22",
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
		require.NoError(t, err, "create deployment iteration %d", i+1)
		deploymentIDs = append(deploymentIDs, dep.Deployment.ID)
	}

	// All should have same deployment ID
	require.Len(t, lo.Uniq(deploymentIDs), 1, "expected all deployments to have same ID")

	// Verify function tools exist
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(deploymentIDs[0]))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")
}

func TestCreateDeployment_WithFunctions_NodeJS22Runtime(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-nodejs:22-runtime",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "nodejs:22-functions",
				Slug:    "nodejs-22-functions",
				Runtime: "nodejs:22",
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
	require.NoError(t, err, "create deployment with nodejs:22 runtime")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	// Verify runtime is set correctly
	require.Equal(t, "nodejs:22", dep.Deployment.FunctionsAssets[0].Runtime, "unexpected runtime")

	// Verify all tools have nodejs:22 runtime
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")

	for _, tool := range functionTools {
		require.Equal(t, "nodejs:22", tool.Runtime, "all tools should have nodejs:22 runtime")
	}
}

func TestCreateDeployment_WithFunctions_Python312Runtime(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-python312-runtime",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "python312-functions",
				Slug:    "python312-functions",
				Runtime: "python:3.12",
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
	require.NoError(t, err, "create deployment with python:3.12 runtime")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	// Verify runtime is set correctly
	require.Equal(t, "python:3.12", dep.Deployment.FunctionsAssets[0].Runtime, "unexpected runtime")

	// Verify all tools have python:3.12 runtime
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")

	for _, tool := range functionTools {
		require.Equal(t, "python:3.12", tool.Runtime, "all tools should have python:3.12 runtime")
	}
}

func TestCreateDeployment_WithFunctions_InvalidAssetID(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-invalid-asset-id",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: "invalid-uuid",
				Name:    "test-functions",
				Slug:    "test-functions",
				Runtime: "nodejs:22",
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

	require.Error(t, err)
	require.Contains(t, err.Error(), "error parsing functions asset id", "should contain asset ID parsing error")
}

func TestCreateDeployment_WithFunctions_InvalidRuntime(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-invalid-runtime",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "test-functions",
				Slug:    "test-functions",
				Runtime: "java17", // Invalid runtime
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

	require.ErrorIs(t, err, deployments.ErrUnsupported)
	require.Nil(t, dep, "evolve result must be nil")
}

func TestCreateDeployment_WithFunctions_NonExistentAsset(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-nonexistent-asset",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: uuid.New().String(), // Valid UUID but non-existent asset
				Name:    "test-functions",
				Slug:    "test-functions",
				Runtime: "nodejs:22",
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

	require.Error(t, err)
	require.Contains(t, err.Error(), "error adding deployment functions asset", "should contain functions asset error")
}

func TestCreateDeployment_WithFunctions_EmptyName(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-empty-name",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "", // Empty name
				Slug:    "test-functions",
				Runtime: "nodejs:22",
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

	require.Error(t, err)
	require.Contains(t, err.Error(), "error adding deployment functions asset", "should contain functions asset error")
}

func TestCreateDeployment_WithFunctions_EmptySlug(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-empty-slug",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "test-functions",
				Slug:    "", // Empty slug
				Runtime: "nodejs:22",
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

	require.Error(t, err)
	require.Contains(t, err.Error(), "error adding deployment functions asset", "should contain functions asset error")
}

func TestCreateDeployment_WithFunctions_BadToolsManifest(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file with bad tools manifest that contains malformed tools
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-bad-tools.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-bad-tools-manifest",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "bad-tools-functions",
				Slug:    "bad-tools-functions",
				Runtime: "nodejs:22",
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
	require.NoError(t, err, "create deployment should succeed even with bad tools")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status should be completed")
	require.Len(t, dep.Deployment.FunctionsAssets, 1, "expected 1 functions file")

	// Verify that NO function tools were created due to bad manifest
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.Empty(t, functionTools, "expected zero function tools due to bad manifest - all tools should be invalid")
}

func TestCreateDeployment_WithFunctions_InvalidManifest(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file with invalid JSON manifest
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-invalid.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-invalid-manifest",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "invalid-manifest-functions",
				Slug:    "invalid-manifest-functions",
				Runtime: "nodejs:22",
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
	require.NoError(t, err, "create deployment should succeed but processing should fail")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "failed", dep.Deployment.Status, "deployment status should be failed due to invalid manifest JSON")
	require.Len(t, dep.Deployment.FunctionsAssets, 1, "expected 1 functions file")

	// Verify that NO function tools were created due to invalid manifest
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.Empty(t, functionTools, "expected zero function tools due to invalid manifest JSON")
}

func TestCreateDeployment_WithFunctions_ManifestValidation(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Test todo manifest (should create 4 tools)
	todoRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	todoDep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-todo-manifest-validation",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: todoRes.Asset.ID,
				Name:    "todo-functions",
				Slug:    "todo-functions",
				Runtime: "nodejs:22",
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
	require.NoError(t, err, "create deployment with todo manifest")
	require.Equal(t, "completed", todoDep.Deployment.Status, "todo deployment should be completed")

	// Verify todo manifest created 4 tools
	repo := testrepo.New(ti.conn)
	todoTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(todoDep.Deployment.ID))
	require.NoError(t, err, "list todo deployment function tools")
	require.Len(t, todoTools, 4, "expected 4 function tools from todo manifest")

	// Verify tool names match manifest
	toolNames := lo.Map(todoTools, func(tool testrepo.FunctionToolDefinition, _ int) string {
		return tool.Name
	})
	require.ElementsMatch(t, toolNames, []string{
		"list_all_todos",
		"get_todo",
		"create_todo",
		"share_todos",
	}, "tool names should match manifest definitions")

	// Test petstore manifest (should create 7 tools)
	petstoreRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	petstoreDep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-petstore-manifest-validation",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: petstoreRes.Asset.ID,
				Name:    "petstore-functions",
				Slug:    "petstore-functions",
				Runtime: "python:3.12",
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
	require.NoError(t, err, "create deployment with petstore manifest")
	require.Equal(t, "completed", petstoreDep.Deployment.Status, "petstore deployment should be completed")

	// Verify petstore manifest created 7 tools
	petstoreTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(petstoreDep.Deployment.ID))
	require.NoError(t, err, "list petstore deployment function tools")
	require.Len(t, petstoreTools, 7, "expected 7 function tools from petstore manifest")

	// Verify tool names match manifest
	petstoreToolNames := lo.Map(petstoreTools, func(tool testrepo.FunctionToolDefinition, _ int) string {
		return tool.Name
	})
	require.ElementsMatch(t, petstoreToolNames, []string{
		"list_all_pets",
		"get_pet",
		"create_pet",
		"update_pet",
		"delete_pet",
		"find_pets_by_status",
		"upload_pet_image",
	}, "petstore tool names should match manifest definitions")

	// Verify all tools have correct runtimes
	for _, tool := range todoTools {
		require.Equal(t, "nodejs:22", tool.Runtime, "todo tools should have nodejs:22 runtime")
	}
	for _, tool := range petstoreTools {
		require.Equal(t, "python:3.12", tool.Runtime, "petstore tools should have python:3.12 runtime")
	}
}

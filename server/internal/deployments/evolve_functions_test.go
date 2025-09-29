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

func TestEvolve_ReplaceFunctions(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions files
	jsRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")
	pyRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	// Create initial deployment with only JS functions
	initial, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{}, // No OpenAPI
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: jsRes.Asset.ID,
				Name:    "js-functions",
				Slug:    "js-functions",
				Runtime: "nodejs:22",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{}, // No packages
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "create initial functions-only deployment")

	require.NotEqual(t, uuid.Nil.String(), initial.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", initial.Deployment.Status, "deployment status is not completed")
	require.Empty(t, initial.Deployment.Openapiv3Assets, "should have no OpenAPI assets")
	require.Len(t, initial.Deployment.FunctionsAssets, 1, "expected 1 functions file")
	require.Empty(t, initial.Deployment.Packages, "should have no packages")

	repo := testrepo.New(ti.conn)
	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(initial.Deployment.ID),
		ProjectID:    uuid.MustParse(initial.Deployment.ProjectID),
	})
	require.NoError(t, err, "first evolve: get functions without access")
	require.Equal(t, int64(1), accessCount, "first evolve: all functions should have access credentials")

	// Evolve to add Python functions and remove JS functions (still functions-only)
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{}, // Still no OpenAPI
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: pyRes.Asset.ID,
				Name:    "py-functions",
				Slug:    "py-functions",
				Runtime: "python:3.12",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{}, // Still no packages
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{jsRes.Asset.ID}, // Remove JS functions
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve functions-only deployment")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Empty(t, evolved.Deployment.Openapiv3Assets, "should still have no OpenAPI assets")
	require.Len(t, evolved.Deployment.FunctionsAssets, 1, "expected 1 functions file after evolution")
	require.Empty(t, evolved.Deployment.Packages, "should still have no packages")
	require.Equal(t, "py-functions", evolved.Deployment.FunctionsAssets[0].Name, "should have Python functions")

	// Verify only function tools exist, no HTTP tools
	httpTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment HTTP tools")
	require.Empty(t, httpTools, "should have no HTTP tools")

	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "should have function tools")

	// Verify all tools have Python runtime
	for _, tool := range functionTools {
		require.Equal(t, "python:3.12", tool.Runtime, "all tools should have python:3.12 runtime")
	}

	accessCount, err = repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(initial.Deployment.ID),
		ProjectID:    uuid.MustParse(initial.Deployment.ProjectID),
	})
	require.NoError(t, err, "second evolve: get functions without access")
	require.Equal(t, int64(1), accessCount, "second evolve: all functions should have access credentials")
}

func TestEvolve_FunctionsFirst(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	// Create initial deployment with only functions
	initial, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "initial-functions",
				Slug:    "initial-functions",
				Runtime: "nodejs:22",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "create initial functions-only deployment")

	require.Empty(t, initial.Deployment.Openapiv3Assets, "initial deployment should have no OpenAPI")
	require.Len(t, initial.Deployment.FunctionsAssets, 1, "initial deployment should have 1 functions file")

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

	// Evolve to add OpenAPI while keeping functions
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "todo-api",
				Slug:    "todo-api",
			},
		},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve to add OpenAPI to functions deployment")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Len(t, evolved.Deployment.Openapiv3Assets, 1, "should have OpenAPI asset")
	require.Len(t, evolved.Deployment.FunctionsAssets, 1, "should keep functions file")
	require.Equal(t, "todo-api", evolved.Deployment.Openapiv3Assets[0].Name, "should have correct OpenAPI asset")
	require.Equal(t, "initial-functions", evolved.Deployment.FunctionsAssets[0].Name, "should keep original functions")

	// Verify both HTTP and function tools exist
	repo := testrepo.New(ti.conn)
	httpTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment HTTP tools")
	require.NotEmpty(t, httpTools, "should have HTTP tools from OpenAPI")

	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "should have function tools")

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(initial.Deployment.ID),
		ProjectID:    uuid.MustParse(initial.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all functions should have access credentials")
}

func TestEvolve_UpsertFunctions_InitialDeployment(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	// Test evolving when no previous deployments exist (should create initial deployment)
	result, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "initial-functions",
				Slug:    "initial-functions",
				Runtime: "nodejs:22",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve initial deployment with functions")

	require.NotEqual(t, uuid.Nil.String(), result.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", result.Deployment.Status, "deployment status is not completed")
	require.Len(t, result.Deployment.FunctionsAssets, 1, "expected 1 functions file")
	require.Equal(t, "initial-functions", result.Deployment.FunctionsAssets[0].Name, "unexpected functions name")
	require.Equal(t, "initial-functions", string(result.Deployment.FunctionsAssets[0].Slug), "unexpected functions slug")

	// Verify function tools were generated
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(result.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(result.Deployment.ID),
		ProjectID:    uuid.MustParse(result.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all functions should have access credentials")
}

func TestEvolve_UpsertFunctions_AddToExisting(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Create initial deployment with OpenAPI
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-initial-deployment-for-functions",
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
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create initial deployment")
	require.Equal(t, "completed", initial.Deployment.Status, "initial deployment status is not completed")
	require.Empty(t, initial.Deployment.FunctionsAssets, "initial deployment should have no functions")

	// Upload functions file
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	// Evolve deployment to add functions
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "added-functions",
				Slug:    "added-functions",
				Runtime: "nodejs:22",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment to add functions")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "evolved deployment status is not completed")
	require.Len(t, evolved.Deployment.Openapiv3Assets, 1, "should keep existing OpenAPI asset")
	require.Len(t, evolved.Deployment.FunctionsAssets, 1, "should have added functions")
	require.Equal(t, "added-functions", evolved.Deployment.FunctionsAssets[0].Name, "unexpected functions name")

	// Verify both HTTP and function tools exist
	repo := testrepo.New(ti.conn)
	httpTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment HTTP tools")
	require.NotEmpty(t, httpTools, "should have HTTP tools from OpenAPI")

	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "should have function tools")
}

func TestEvolve_UpsertFunctions_UpdateExisting(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload initial functions file
	fres1 := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	// Create initial deployment with functions
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-initial-functions-deployment",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres1.Asset.ID,
				Name:    "my-functions",
				Slug:    "my-functions",
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
	require.NoError(t, err, "create initial deployment with functions")
	require.Len(t, initial.Deployment.FunctionsAssets, 1, "expected 1 functions file")

	// Upload updated functions file
	fres2 := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	// Evolve deployment to update functions (same name/slug, different asset and runtime)
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: fres2.Asset.ID,
				Name:    "my-functions", // Same name
				Slug:    "my-functions", // Same slug
				Runtime: "python:3.12",  // Different runtime
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment to update functions")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "evolved deployment status is not completed")
	require.Len(t, evolved.Deployment.FunctionsAssets, 1, "should still have 1 functions file")
	require.Equal(t, "my-functions", evolved.Deployment.FunctionsAssets[0].Name, "functions name should remain same")
	require.Equal(t, "python:3.12", evolved.Deployment.FunctionsAssets[0].Runtime, "runtime should be updated")

	// Verify function tools were updated with new runtime
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "should have function tools")

	for _, tool := range functionTools {
		require.Equal(t, "python:3.12", tool.Runtime, "all tools should have updated runtime")
	}

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(evolved.Deployment.ID),
		ProjectID:    uuid.MustParse(evolved.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all functions should have access credentials")
}

func TestEvolve_UpsertFunctions_Multiple(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload multiple functions files with different manifests to avoid tool name conflicts
	jsRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")
	pyRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	// Create initial deployment
	result, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
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
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment with multiple functions")

	require.NotEqual(t, uuid.Nil.String(), result.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", result.Deployment.Status, "deployment status is not completed")
	require.Len(t, result.Deployment.FunctionsAssets, 2, "expected 2 functions files")

	// Verify function names
	names := lo.Map(result.Deployment.FunctionsAssets, func(f *types.DeploymentFunctions, _ int) string {
		return f.Name
	})
	require.ElementsMatch(t, names, []string{"js-functions", "py-functions"}, "unexpected function names")

	// Verify function tools were created for all files
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(result.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")

	// Check that we have tools for both runtimes
	runtimes := lo.Map(functionTools, func(tool testrepo.FunctionToolDefinition, _ int) string {
		return tool.Runtime
	})
	require.Contains(t, runtimes, "nodejs:22", "expected nodejs:22 runtime tools")
	require.Contains(t, runtimes, "python:3.12", "expected python:3.12 runtime tools")

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(result.Deployment.ID),
		ProjectID:    uuid.MustParse(result.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(2), accessCount, "all functions should have access credentials")
}

func TestEvolve_UpsertFunctions_MixedWithOpenAPI(t *testing.T) {
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

	// Create deployment with both OpenAPI and functions
	result, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "todo-api",
				Slug:    "todo-api",
			},
		},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "todo-functions",
				Slug:    "todo-functions",
				Runtime: "nodejs:22",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment with mixed assets")

	require.NotEqual(t, uuid.Nil.String(), result.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", result.Deployment.Status, "deployment status is not completed")
	require.Len(t, result.Deployment.Openapiv3Assets, 1, "expected 1 OpenAPI asset")
	require.Len(t, result.Deployment.FunctionsAssets, 1, "expected 1 functions file")

	// Verify both HTTP and function tools were created
	repo := testrepo.New(ti.conn)
	httpTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(result.Deployment.ID))
	require.NoError(t, err, "list deployment HTTP tools")
	require.NotEmpty(t, httpTools, "expected HTTP tools to be created")

	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(result.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected function tools to be created")
}

func TestEvolve_ExcludeFunctions_Single(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload two functions files
	jsRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")
	pyRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	// Create initial deployment with both functions
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-initial-multiple-functions",
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
	require.NoError(t, err, "create initial deployment with multiple functions")
	require.Len(t, initial.Deployment.FunctionsAssets, 2, "expected 2 functions files in initial deployment")

	// Evolve deployment to exclude first functions file
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{jsRes.Asset.ID},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment to exclude functions")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Len(t, evolved.Deployment.FunctionsAssets, 1, "expected 1 functions file after exclusion")
	require.Equal(t, "py-functions", evolved.Deployment.FunctionsAssets[0].Name, "wrong functions file remained")

	// Verify only Python function tools remain
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "expected some function tools to remain")

	for _, tool := range functionTools {
		require.Equal(t, "python:3.12", tool.Runtime, "all remaining tools should be python:3.12")
	}

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(evolved.Deployment.ID),
		ProjectID:    uuid.MustParse(evolved.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all remaining functions should have access credentials")
}

func TestEvolve_ExcludeFunctions_Multiple(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload two functions files
	jsRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")
	pyRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	// Create initial deployment with all functions
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-initial-three-functions",
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
	require.NoError(t, err, "create initial deployment with two functions")
	require.Len(t, initial.Deployment.FunctionsAssets, 2, "expected 2 functions files in initial deployment")

	// Evolve deployment to exclude one functions file
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{jsRes.Asset.ID},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment to exclude one function")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Len(t, evolved.Deployment.FunctionsAssets, 1, "expected 1 functions file after exclusions")
	require.Equal(t, "py-functions", evolved.Deployment.FunctionsAssets[0].Name, "wrong functions file remained")

	repo := testrepo.New(ti.conn)
	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(evolved.Deployment.ID),
		ProjectID:    uuid.MustParse(evolved.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all remaining functions should have access credentials")
}

func TestEvolve_ExcludeFunctions_All(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file
	jsRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	// Create initial deployment with functions
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-initial-for-all-exclusion",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: jsRes.Asset.ID,
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
	require.NoError(t, err, "create initial deployment with functions")
	require.Len(t, initial.Deployment.FunctionsAssets, 1, "expected 1 functions file in initial deployment")

	// Verify initial deployment has function tools
	repo := testrepo.New(ti.conn)
	initialFunctionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(initial.Deployment.ID))
	require.NoError(t, err, "list initial deployment function tools")
	require.NotEmpty(t, initialFunctionTools, "expected function tools in initial deployment")

	// Evolve deployment to exclude all functions
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{jsRes.Asset.ID},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment to exclude all functions")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Empty(t, evolved.Deployment.FunctionsAssets, "expected 0 functions files after excluding all")

	// Verify no function tools remain in the deployment
	evolvedFunctionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list evolved deployment function tools")
	require.Empty(t, evolvedFunctionTools, "expected no function tools after excluding all functions")

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(evolved.Deployment.ID),
		ProjectID:    uuid.MustParse(evolved.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(0), accessCount, "no access credentials should exist after excluding all functions")
}

func TestEvolve_ExcludeFunctions_KeepOpenAPI(t *testing.T) {
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
	jsRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	// Create initial deployment with both OpenAPI and functions
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-initial-mixed-for-exclusion",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "todo-api",
				Slug:    "todo-api",
			},
		},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: jsRes.Asset.ID,
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
	require.NoError(t, err, "create initial mixed deployment")
	require.Len(t, initial.Deployment.Openapiv3Assets, 1, "expected 1 OpenAPI asset in initial")
	require.Len(t, initial.Deployment.FunctionsAssets, 1, "expected 1 functions file in initial")

	// Evolve deployment to exclude functions but keep OpenAPI
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{jsRes.Asset.ID},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment to exclude functions")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Len(t, evolved.Deployment.Openapiv3Assets, 1, "should keep OpenAPI asset")
	require.Empty(t, evolved.Deployment.FunctionsAssets, "should exclude functions")

	// Verify HTTP tools remain but function tools are gone
	repo := testrepo.New(ti.conn)
	httpTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment HTTP tools")
	require.NotEmpty(t, httpTools, "HTTP tools should remain")

	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.Empty(t, functionTools, "function tools should be gone")

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(evolved.Deployment.ID),
		ProjectID:    uuid.MustParse(evolved.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(0), accessCount, "no access credentials should exist after excluding all functions")
}

func TestEvolve_Functions_ComplexScenario(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload multiple assets for complex scenario
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	jsRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")
	pyRes := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-petstore.json", "python:3.12")

	// Create initial deployment with OpenAPI and one functions file
	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-complex-initial",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "todo-api",
				Slug:    "todo-api",
			},
		},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: jsRes.Asset.ID,
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
	require.NoError(t, err, "create initial complex deployment")
	require.Len(t, initial.Deployment.Openapiv3Assets, 1, "expected 1 OpenAPI asset in initial")
	require.Len(t, initial.Deployment.FunctionsAssets, 1, "expected 1 functions file in initial")

	// Complex evolution: add new functions (Python), remove existing JS functions, keep OpenAPI
	evolved, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: pyRes.Asset.ID,
				Name:    "py-functions",
				Slug:    "py-functions",
				Runtime: "python:3.12",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{jsRes.Asset.ID},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment with complex changes")

	require.NotEqual(t, initial.Deployment.ID, evolved.Deployment.ID, "evolved deployment should have different ID")
	require.Equal(t, "completed", evolved.Deployment.Status, "deployment status is not completed")
	require.Len(t, evolved.Deployment.Openapiv3Assets, 1, "should keep OpenAPI asset")
	require.Len(t, evolved.Deployment.FunctionsAssets, 1, "should have 1 functions file after evolution")

	// Verify function names
	functionNames := lo.Map(evolved.Deployment.FunctionsAssets, func(f *types.DeploymentFunctions, _ int) string {
		return f.Name
	})
	require.ElementsMatch(t, functionNames, []string{"py-functions"}, "unexpected function names")

	// Verify both HTTP and function tools exist with correct runtimes
	repo := testrepo.New(ti.conn)
	httpTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment HTTP tools")
	require.NotEmpty(t, httpTools, "should have HTTP tools from OpenAPI")

	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(evolved.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.NotEmpty(t, functionTools, "should have function tools")

	// Check that we only have Python tools now (JS tools should be gone)
	runtimes := lo.Map(functionTools, func(tool testrepo.FunctionToolDefinition, _ int) string {
		return tool.Runtime
	})
	require.Contains(t, runtimes, "python:3.12", "expected python:3.12 runtime tools")
	// All tools should be Python now since we excluded JS functions and only added Python
	for _, tool := range functionTools {
		require.Equal(t, "python:3.12", tool.Runtime, "all function tools should be python:3.12 after evolution")
	}

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(evolved.Deployment.ID),
		ProjectID:    uuid.MustParse(evolved.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all functions should have access credentials")
}

func TestEvolve_UpsertFunctions_InvalidAssetID(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	_, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: "invalid-uuid",
				Name:    "test-functions",
				Slug:    "test-functions",
				Runtime: "nodejs:22",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "error parsing functions asset id to upsert", "should contain functions asset ID parsing error")
}

func TestEvolve_ExcludeFunctions_InvalidAssetID(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	_, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{"invalid-uuid"},
		ExcludePackages:        []string{},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "error parsing functions asset id to exclude", "should contain functions asset ID parsing error")
}

func TestEvolve_UpsertFunctions_InvalidRuntime(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "nodejs:22")

	result, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "test-functions",
				Slug:    "test-functions",
				Runtime: "java17", // Invalid runtime
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})

	require.ErrorIs(t, err, deployments.ErrUnsupported)
	require.Nil(t, result, "evolve result must be nil")
}

func TestEvolve_UpsertFunctions_BadToolsManifest(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file with bad tools manifest that contains malformed tools
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-bad-tools.json", "nodejs:22")

	result, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "bad-tools-functions",
				Slug:    "bad-tools-functions",
				Runtime: "nodejs:22",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve should succeed even with bad tools")

	require.NotEqual(t, uuid.Nil.String(), result.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", result.Deployment.Status, "deployment status should be completed")
	require.Len(t, result.Deployment.FunctionsAssets, 1, "expected 1 functions file")

	// Verify that NO function tools were created due to bad manifest
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(result.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.Empty(t, functionTools, "expected zero function tools due to bad manifest - all tools should be invalid")
}

func TestEvolve_UpsertFunctions_InvalidManifest(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file with invalid JSON manifest
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-invalid.json", "nodejs:22")

	result, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		UpsertOpenapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		UpsertFunctions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "invalid-manifest-functions",
				Slug:    "invalid-manifest-functions",
				Runtime: "nodejs:22",
			},
		},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: []string{},
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve should succeed but processing should fail")

	require.NotEqual(t, uuid.Nil.String(), result.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "failed", result.Deployment.Status, "deployment status should be failed due to invalid manifest JSON")
	require.Len(t, result.Deployment.FunctionsAssets, 1, "expected 1 functions file")

	// Verify that NO function tools were created due to invalid manifest
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(result.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.Empty(t, functionTools, "expected zero function tools due to invalid manifest JSON")
}

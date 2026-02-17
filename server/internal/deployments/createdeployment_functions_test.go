package deployments_test

import (
	"bytes"
	"encoding/json"
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

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(dep.Deployment.ID),
		ProjectID:    uuid.MustParse(dep.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all functions should have access credentials")
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

	// Verify meta tags on tools
	createTodoTool, ok := lo.Find(functionTools, func(t testrepo.FunctionToolDefinition) bool {
		return t.Name == "create_todo"
	})
	require.True(t, ok, "create_todo tool not found")
	require.NotNil(t, createTodoTool.Meta, "create_todo should have meta")

	var toolMeta map[string]string
	err = json.Unmarshal(createTodoTool.Meta, &toolMeta)
	require.NoError(t, err, "tool meta should unmarshal to map[string]string")
	require.Equal(t, "productivity", toolMeta["category"])
	require.Equal(t, "1.0", toolMeta["version"])

	// Verify tools without meta tags have nil meta
	listAllTodosTool, ok := lo.Find(functionTools, func(t testrepo.FunctionToolDefinition) bool {
		return t.Name == "list_all_todos"
	})
	require.True(t, ok, "list_all_todos tool not found")
	require.Nil(t, listAllTodosTool.Meta, "list_all_todos should have no meta tags")

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(dep.Deployment.ID),
		ProjectID:    uuid.MustParse(dep.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all functions should have access credentials")
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

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(dep.Deployment.ID),
		ProjectID:    uuid.MustParse(dep.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all functions should have access credentials")
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

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(dep.Deployment.ID),
		ProjectID:    uuid.MustParse(dep.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all functions should have access credentials")
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

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(dep.Deployment.ID),
		ProjectID:    uuid.MustParse(dep.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(1), accessCount, "all functions should have access credentials")
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

	accessCount, err := repo.CountFunctionsAccess(ctx, testrepo.CountFunctionsAccessParams{
		DeploymentID: uuid.MustParse(dep.Deployment.ID),
		ProjectID:    uuid.MustParse(dep.Deployment.ProjectID),
	})
	require.NoError(t, err, "get functions without access")
	require.Equal(t, int64(2), accessCount, "all functions should have access credentials")
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
	for i := range 3 {
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
	require.Equal(t, "failed", dep.Deployment.Status, "deployment status should be failed")
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

func TestDeploymentsService_CreateDeployment_WithResources(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)

	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file with resources
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-with-resources.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-functions-with-resources",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "functions-with-resources",
				Slug:    "functions-with-resources",
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
	require.NoError(t, err, "create deployment with functions and resources")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	repo := testrepo.New(ti.conn)

	// Verify function tools were created
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.Len(t, functionTools, 2, "expected 2 function tools")

	toolNames := lo.Map(functionTools, func(tool testrepo.FunctionToolDefinition, _ int) string {
		return tool.Name
	})
	require.ElementsMatch(t, toolNames, []string{"search_documentation", "analyze_data"}, "unexpected tool names")

	// Verify function resources were created
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function resources")
	require.Len(t, resources, 3, "expected 3 function resources")

	t.Run("resource names", func(t *testing.T) {
		t.Parallel()

		names := lo.Map(resources, func(r testrepo.FunctionResourceDefinition, _ int) string {
			return r.Name
		})
		require.ElementsMatch(t, names, []string{
			"user_guide",
			"api_reference",
			"data_source",
		}, "mismatched resource names")
	})

	t.Run("verify user_guide resource", func(t *testing.T) {
		t.Parallel()

		resource, ok := lo.Find(resources, func(r testrepo.FunctionResourceDefinition) bool {
			return r.Name == "user_guide"
		})

		require.True(t, ok, "resource user_guide not found")
		require.Equal(t, "file:///docs/user-guide.pdf", resource.Uri)
		require.Equal(t, "Comprehensive user guide for the system", resource.Description)
		require.Equal(t, "User Guide", resource.Title.String)
		require.True(t, resource.Title.Valid, "title should be set")
		require.Equal(t, "application/pdf", resource.MimeType.String)
		require.True(t, resource.MimeType.Valid, "mime type should be set")
		require.JSONEq(t, `{}`, string(resource.Variables), "should have no variables")
	})

	t.Run("verify api_reference resource with variables", func(t *testing.T) {
		t.Parallel()

		resource, ok := lo.Find(resources, func(r testrepo.FunctionResourceDefinition) bool {
			return r.Name == "api_reference"
		})

		require.True(t, ok, "resource api_reference not found")
		require.Equal(t, "file:///docs/api-reference.md", resource.Uri)
		require.Equal(t, "API reference documentation", resource.Description)
		require.Equal(t, "API Reference", resource.Title.String)
		require.Equal(t, "text/markdown", resource.MimeType.String)
		require.NotNil(t, resource.Variables, "should have variables")
		require.JSONEq(t, `{"API_VERSION": {"description": "The API version to use"}}`, string(resource.Variables))
	})

	t.Run("verify data_source resource with multiple variables", func(t *testing.T) {
		t.Parallel()

		resource, ok := lo.Find(resources, func(r testrepo.FunctionResourceDefinition) bool {
			return r.Name == "data_source"
		})

		require.True(t, ok, "resource data_source not found")
		require.Equal(t, "https://api.example.com/data", resource.Uri)
		require.Equal(t, "External data source endpoint", resource.Description)
		require.False(t, resource.Title.Valid, "should have no title")
		require.False(t, resource.MimeType.Valid, "should have no mime type")
		require.NotNil(t, resource.Variables, "should have variables")
		require.JSONEq(t, `{"DATA_API_KEY": {"description": "API key for data source"}, "DATA_REGION": {"description": "Region for data access"}}`, string(resource.Variables))
		require.NotNil(t, resource.Meta, "should have meta")

		var meta map[string]any
		err = json.Unmarshal(resource.Meta, &meta)
		require.NoError(t, err, "meta should unmarshal to map[string]string")
	})

	t.Run("verify user_guide has no meta tags", func(t *testing.T) {
		t.Parallel()

		resource, ok := lo.Find(resources, func(r testrepo.FunctionResourceDefinition) bool {
			return r.Name == "user_guide"
		})

		require.True(t, ok, "resource user_guide not found")
		require.Nil(t, resource.Meta, "user_guide should have no meta tags")
	})
}

func TestDeploymentsService_CreateDeployment_ResourcesMultipleFiles(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)

	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload multiple function files with resources
	res1 := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-with-resources.json", "nodejs:22")
	res2 := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-todo.json", "python:3.12")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-resources-multiple-files",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: res1.Asset.ID,
				Name:    "functions-with-resources",
				Slug:    "functions-with-resources",
				Runtime: "nodejs:22",
			},
			{
				AssetID: res2.Asset.ID,
				Name:    "todo-functions",
				Slug:    "todo-functions",
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
	require.NoError(t, err, "create deployment with multiple function files")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	repo := testrepo.New(ti.conn)

	// Verify function tools from both files were created
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.Len(t, functionTools, 6, "expected 6 function tools (2 from first + 4 from second)")

	// Verify resources from first file were created (second file has no resources)
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function resources")
	require.Len(t, resources, 3, "expected 3 function resources from first file")

	// Verify all resources are from the nodejs runtime function
	for _, resource := range resources {
		require.Equal(t, "nodejs:22", resource.Runtime, "all resources should be from nodejs function")
	}
}

func TestDeploymentsService_CreateDeployment_ResourcesWithOpenAPI(t *testing.T) {
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

	// Upload functions file with resources
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-with-resources.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-resources-with-openapi",
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
				Name:    "functions-with-resources",
				Slug:    "functions-with-resources",
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
	require.NoError(t, err, "create deployment with resources and OpenAPI")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	repo := testrepo.New(ti.conn)

	// Verify HTTP tools from OpenAPI were created
	httpTools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment HTTP tools")
	require.Len(t, httpTools, 5, "expected 5 HTTP tools from OpenAPI")

	// Verify function tools were created
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.Len(t, functionTools, 2, "expected 2 function tools")

	// Verify function resources were created
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function resources")
	require.Len(t, resources, 3, "expected 3 function resources")
}

func TestCreateDeployment_WithFunctions_AuthInputSaved(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	// Upload functions file with authInput in manifest
	fres := uploadFunctionsWithManifest(t, ctx, ti.assets, "fixtures/manifest-with-authinput.json", "nodejs:22")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:  "test-functions-authinput",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
		Functions: []*gen.AddFunctionsForm{
			{
				AssetID: fres.Asset.ID,
				Name:    "authinput-functions",
				Slug:    "authinput-functions",
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
	require.NoError(t, err, "create deployment with functions that have authInput")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status should be completed")

	// Verify function tools were created
	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.Len(t, functionTools, 3, "expected 3 function tools")

	// Verify tool with oauth authInput
	oauthTool, ok := lo.Find(functionTools, func(tool testrepo.FunctionToolDefinition) bool {
		return tool.Name == "authenticated_api_call"
	})
	require.True(t, ok, "authenticated_api_call tool not found")
	require.NotNil(t, oauthTool.AuthInput, "authenticated_api_call should have authInput")
	require.NotEmpty(t, oauthTool.AuthInput, "authInput should not be empty")

	// Verify authInput content matches expected structure
	var authInput map[string]any
	err = json.Unmarshal(oauthTool.AuthInput, &authInput)
	require.NoError(t, err, "authInput should unmarshal to map")
	require.Equal(t, "oauth2", authInput["type"], "authInput type should be oauth2")
	require.Equal(t, "OAUTH_TOKEN", authInput["variable"], "authInput variable should be OAUTH_TOKEN")

	// Verify tool with bearer authInput
	bearerTool, ok := lo.Find(functionTools, func(tool testrepo.FunctionToolDefinition) bool {
		return tool.Name == "bearer_authenticated_call"
	})
	require.True(t, ok, "bearer_authenticated_call tool not found")
	require.NotNil(t, bearerTool.AuthInput, "bearer_authenticated_call should have authInput")
	require.NotEmpty(t, bearerTool.AuthInput, "authInput should not be empty")

	// Verify bearer authInput content
	var bearerAuthInput map[string]any
	err = json.Unmarshal(bearerTool.AuthInput, &bearerAuthInput)
	require.NoError(t, err, "bearer authInput should unmarshal to map")
	require.Equal(t, "bearer", bearerAuthInput["type"], "authInput type should be bearer")
	require.Equal(t, "BEARER_TOKEN", bearerAuthInput["variable"], "authInput variable should be BEARER_TOKEN")

	// Verify tool without authInput has nil authInput
	simpleTool, ok := lo.Find(functionTools, func(tool testrepo.FunctionToolDefinition) bool {
		return tool.Name == "simple_api_call"
	})
	require.True(t, ok, "simple_api_call tool not found")
	require.Nil(t, simpleTool.AuthInput, "simple_api_call should have no authInput")
}

package functions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/functions"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestService_GetSignedAssetURL_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestFunctionsService(t)

	// Create a real deployment with functions
	dep := createFunctionsDeployment(t, ctx, ti)

	require.NotEmpty(t, dep.Deployment.FunctionsAssets, "expected functions assets in deployment")
	funcAsset := dep.Deployment.FunctionsAssets[0]

	projectID := uuid.MustParse(dep.Deployment.ProjectID)
	deploymentID := uuid.MustParse(dep.Deployment.ID)
	functionID := uuid.MustParse(funcAsset.ID)
	assetID := funcAsset.AssetID

	// Set up context with runner auth
	ctx = functions.PushRunnerAuthContext(ctx, &functions.RunnerAuthContext{
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		FunctionID:   functionID,
	})

	// Call GetSignedAssetURL
	result, err := ti.service.GetSignedAssetURL(ctx, &gen.GetSignedAssetURLPayload{
		AssetID: assetID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.URL)
}

func TestService_GetSignedAssetURL_Unauthorized_NoAuthContext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestFunctionsService(t)

	// Call without any auth context
	_, err := ti.service.GetSignedAssetURL(ctx, &gen.GetSignedAssetURLPayload{
		AssetID: uuid.New().String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_GetSignedAssetURL_Unauthorized_InvalidAuthContext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestFunctionsService(t)

	// Set up context with invalid runner auth (nil UUID for projectID)
	ctx = functions.PushRunnerAuthContext(ctx, &functions.RunnerAuthContext{
		ProjectID:    uuid.Nil,
		DeploymentID: uuid.New(),
		FunctionID:   uuid.New(),
	})

	_, err := ti.service.GetSignedAssetURL(ctx, &gen.GetSignedAssetURLPayload{
		AssetID: uuid.New().String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_GetSignedAssetURL_InvalidAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestFunctionsService(t)

	ctx = functions.PushRunnerAuthContext(ctx, &functions.RunnerAuthContext{
		ProjectID:    uuid.New(),
		DeploymentID: uuid.New(),
		FunctionID:   uuid.New(),
	})

	_, err := ti.service.GetSignedAssetURL(ctx, &gen.GetSignedAssetURLPayload{
		AssetID: "invalid-uuid",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "invalid asset id")
}

func TestService_GetSignedAssetURL_NilAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestFunctionsService(t)

	ctx = functions.PushRunnerAuthContext(ctx, &functions.RunnerAuthContext{
		ProjectID:    uuid.New(),
		DeploymentID: uuid.New(),
		FunctionID:   uuid.New(),
	})

	_, err := ti.service.GetSignedAssetURL(ctx, &gen.GetSignedAssetURLPayload{
		AssetID: uuid.Nil.String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "asset id cannot be nil")
}

func TestService_GetSignedAssetURL_AssetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestFunctionsService(t)

	// Create a real deployment to get valid project/deployment/function IDs
	dep := createFunctionsDeployment(t, ctx, ti)

	require.NotEmpty(t, dep.Deployment.FunctionsAssets, "expected functions assets in deployment")
	funcAsset := dep.Deployment.FunctionsAssets[0]

	projectID := uuid.MustParse(dep.Deployment.ProjectID)
	deploymentID := uuid.MustParse(dep.Deployment.ID)
	functionID := uuid.MustParse(funcAsset.ID)

	ctx = functions.PushRunnerAuthContext(ctx, &functions.RunnerAuthContext{
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		FunctionID:   functionID,
	})

	// Use a non-existent asset ID
	nonExistentAssetID := uuid.New()

	_, err := ti.service.GetSignedAssetURL(ctx, &gen.GetSignedAssetURLPayload{
		AssetID: nonExistentAssetID.String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_GetSignedAssetURL_WrongDeployment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestFunctionsService(t)

	// Create first deployment
	dep1 := createFunctionsDeployment(t, ctx, ti)
	require.NotEmpty(t, dep1.Deployment.FunctionsAssets, "expected functions assets in deployment 1")
	funcAsset1 := dep1.Deployment.FunctionsAssets[0]

	// Create second deployment (need different idempotency key)
	dep2 := createFunctionsDeploymentWithKey(t, ctx, ti, "test-functions-wrong-deployment-2")
	require.NotEmpty(t, dep2.Deployment.FunctionsAssets, "expected functions assets in deployment 2")

	projectID := uuid.MustParse(dep1.Deployment.ProjectID)
	deploymentID2 := uuid.MustParse(dep2.Deployment.ID)
	functionID1 := uuid.MustParse(funcAsset1.ID)
	assetID1 := funcAsset1.AssetID

	// Try to access asset from deployment 1 using deployment 2's context
	ctx = functions.PushRunnerAuthContext(ctx, &functions.RunnerAuthContext{
		ProjectID:    projectID,
		DeploymentID: deploymentID2,
		FunctionID:   functionID1,
	})

	_, err := ti.service.GetSignedAssetURL(ctx, &gen.GetSignedAssetURLPayload{
		AssetID: assetID1,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_GetSignedAssetURL_WrongFunction(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestFunctionsService(t)

	// Create a deployment
	dep := createFunctionsDeployment(t, ctx, ti)
	require.NotEmpty(t, dep.Deployment.FunctionsAssets, "expected functions assets in deployment")
	funcAsset := dep.Deployment.FunctionsAssets[0]

	projectID := uuid.MustParse(dep.Deployment.ProjectID)
	deploymentID := uuid.MustParse(dep.Deployment.ID)
	assetID := funcAsset.AssetID

	// Use a different (non-existent) function ID
	wrongFunctionID := uuid.New()

	ctx = functions.PushRunnerAuthContext(ctx, &functions.RunnerAuthContext{
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		FunctionID:   wrongFunctionID,
	})

	_, err := ti.service.GetSignedAssetURL(ctx, &gen.GetSignedAssetURLPayload{
		AssetID: assetID,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_GetSignedAssetURL_CrossProjectAccess(t *testing.T) {
	t.Parallel()

	// Create first test service and deployment in project 1
	ctx1, ti := newTestFunctionsService(t)

	dep1 := createFunctionsDeployment(t, ctx1, ti)
	require.NotEmpty(t, dep1.Deployment.FunctionsAssets, "expected functions assets in deployment 1")
	funcAsset1 := dep1.Deployment.FunctionsAssets[0]

	project1ID := uuid.MustParse(dep1.Deployment.ProjectID)
	deployment1ID := uuid.MustParse(dep1.Deployment.ID)
	function1ID := uuid.MustParse(funcAsset1.ID)
	asset1ID := funcAsset1.AssetID

	// Verify we can access the asset from project 1's context
	ctx1 = functions.PushRunnerAuthContext(ctx1, &functions.RunnerAuthContext{
		ProjectID:    project1ID,
		DeploymentID: deployment1ID,
		FunctionID:   function1ID,
	})

	result, err := ti.service.GetSignedAssetURL(ctx1, &gen.GetSignedAssetURLPayload{
		AssetID: asset1ID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.URL)

	// Create second auth context with different project using the same database connection
	ctx2 := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)

	// Create deployment in project 2
	dep2 := createFunctionsDeploymentWithKey(t, ctx2, ti, "test-functions-cross-project-2")
	require.NotEmpty(t, dep2.Deployment.FunctionsAssets, "expected functions assets in deployment 2")

	project2ID := uuid.MustParse(dep2.Deployment.ProjectID)
	deployment2ID := uuid.MustParse(dep2.Deployment.ID)
	function2ID := uuid.MustParse(dep2.Deployment.FunctionsAssets[0].ID)

	// Ensure we have different projects
	require.NotEqual(t, project1ID, project2ID)

	// Try to access asset from project 1 using project 2's runner auth context
	ctx2 = functions.PushRunnerAuthContext(ctx2, &functions.RunnerAuthContext{
		ProjectID:    project2ID,
		DeploymentID: deployment2ID,
		FunctionID:   function2ID,
	})

	_, err = ti.service.GetSignedAssetURL(ctx2, &gen.GetSignedAssetURLPayload{
		AssetID: asset1ID, // Asset from project 1
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

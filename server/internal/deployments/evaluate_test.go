package deployments_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func uploadOpenAPIv3Fixture(t *testing.T, ctx context.Context, ti *testInstance, fixture string) string {
	t.Helper()

	bs := bytes.NewBuffer(testenv.ReadFixture(t, fixture))
	res, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	return res.Asset.ID
}

func evolveOpenAPIv3(t *testing.T, ctx context.Context, ti *testInstance, upsert []*gen.AddOpenAPIv3DeploymentAssetForm, exclude []string) *gen.EvolveResult {
	t.Helper()

	res, err := ti.service.Evolve(ctx, &gen.EvolvePayload{
		ApikeyToken:            nil,
		SessionToken:           nil,
		ProjectSlugInput:       nil,
		DeploymentID:           nil,
		UpsertOpenapiv3Assets:  upsert,
		UpsertFunctions:        []*gen.AddFunctionsForm{},
		UpsertPackages:         []*gen.AddPackageForm{},
		ExcludeOpenapiv3Assets: exclude,
		ExcludeFunctions:       []string{},
		ExcludePackages:        []string{},
	})
	require.NoError(t, err, "evolve deployment")
	require.Equal(t, "completed", res.Deployment.Status)

	return res
}

func TestDeploymentsService_EvaluateOpenAPIv3_NewSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	assetID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/todo-valid.yaml")

	res, err := ti.service.EvaluateOpenAPIv3(ctx, &gen.EvaluateOpenAPIv3Payload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		AssetID:          assetID,
		Slug:             "todo",
		Name:             nil,
	})
	require.NoError(t, err)
	require.Equal(t, "new", res.Status)
	require.Equal(t, "todo", res.Name)
	require.Nil(t, res.Base)
	require.Nil(t, res.BaseAssetID)
	require.NotNil(t, res.Next)
	require.Positive(t, res.Next.ToolCount)
	require.Zero(t, res.BreakingCount)
	require.Empty(t, res.AffectedToolsets)
}

func TestDeploymentsService_EvaluateOpenAPIv3_UnchangedSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	assetID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/todo-valid.yaml")
	evolveOpenAPIv3(t, ctx, ti, []*gen.AddOpenAPIv3DeploymentAssetForm{{AssetID: assetID, Name: "Todo API", Slug: "todo"}}, []string{})

	res, err := ti.service.EvaluateOpenAPIv3(ctx, &gen.EvaluateOpenAPIv3Payload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		AssetID:          assetID,
		Slug:             "todo",
		Name:             nil,
	})
	require.NoError(t, err)
	require.Equal(t, "unchanged", res.Status)
	require.Equal(t, "Todo API", res.Name, "name defaults to the deployed name")
	require.NotNil(t, res.Base)
	require.Equal(t, res.Base.ToolCount, res.Next.ToolCount)
	require.Empty(t, res.Changes)
}

func TestDeploymentsService_EvaluateOpenAPIv3_ReplacedSourceIsBreaking(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	todoID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/todo-valid.yaml")
	petstoreID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/petstore-valid.yaml")
	evolveOpenAPIv3(t, ctx, ti, []*gen.AddOpenAPIv3DeploymentAssetForm{{AssetID: todoID, Name: "todo", Slug: "todo"}}, []string{})

	res, err := ti.service.EvaluateOpenAPIv3(ctx, &gen.EvaluateOpenAPIv3Payload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		AssetID:          petstoreID,
		Slug:             "todo",
		Name:             nil,
	})
	require.NoError(t, err)
	require.Equal(t, "changed", res.Status)
	require.Equal(t, todoID, *res.BaseAssetID)
	require.Equal(t, petstoreID, *res.AssetID)
	require.Positive(t, res.BreakingCount, "removing every todo tool is breaking")
	require.Equal(t, gen.OpenAPIv3EvaluationSeverity("breaking"), res.Changes[0].Severity, "changes are sorted most severe first")
}

func TestDeploymentsService_EvaluateOpenAPIv3_InvalidAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))

	_, err := ti.service.EvaluateOpenAPIv3(ctx, &gen.EvaluateOpenAPIv3Payload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		AssetID:          "not-a-uuid",
		Slug:             "todo",
		Name:             nil,
	})
	require.Error(t, err)
}

func TestDeploymentsService_GetDeploymentEvaluation_FirstDeployment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	assetID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/todo-valid.yaml")
	dep := evolveOpenAPIv3(t, ctx, ti, []*gen.AddOpenAPIv3DeploymentAssetForm{{AssetID: assetID, Name: "todo", Slug: "todo"}}, []string{})

	res, err := ti.service.GetDeploymentEvaluation(ctx, &gen.GetDeploymentEvaluationPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     dep.Deployment.ID,
	})
	require.NoError(t, err)
	require.Equal(t, dep.Deployment.ID, res.DeploymentID)
	require.Nil(t, res.BaseDeploymentID)
	require.Len(t, res.Documents, 1)
	require.Equal(t, "new", res.Documents[0].Status)
}

func TestDeploymentsService_GetDeploymentEvaluation_ChangedAndRemovedSources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	todoID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/todo-valid.yaml")
	taggedID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/todo-with-tags.yaml")
	petstoreID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/petstore-valid.yaml")

	first := evolveOpenAPIv3(t, ctx, ti, []*gen.AddOpenAPIv3DeploymentAssetForm{
		{AssetID: todoID, Name: "todo", Slug: "todo"},
		{AssetID: petstoreID, Name: "petstore", Slug: "petstore"},
	}, []string{})
	second := evolveOpenAPIv3(t, ctx, ti, []*gen.AddOpenAPIv3DeploymentAssetForm{
		{AssetID: taggedID, Name: "todo", Slug: "todo"},
	}, []string{petstoreID})

	res, err := ti.service.GetDeploymentEvaluation(ctx, &gen.GetDeploymentEvaluationPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     second.Deployment.ID,
	})
	require.NoError(t, err)
	require.Equal(t, first.Deployment.ID, *res.BaseDeploymentID)
	require.Len(t, res.Documents, 2)

	petstore, todo := res.Documents[0], res.Documents[1]
	require.Equal(t, "petstore", petstore.Slug)
	require.Equal(t, "removed", petstore.Status)
	require.Nil(t, petstore.Next)
	require.NotNil(t, petstore.Base)
	require.Equal(t, 1, petstore.BreakingCount)
	require.Equal(t, "source_removed", petstore.Changes[0].Code)

	require.Equal(t, "todo", todo.Slug)
	require.Equal(t, todoID, *todo.BaseAssetID)
	require.Equal(t, taggedID, *todo.AssetID)
}

func TestDeploymentsService_GetDeploymentEvaluation_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))

	_, err := ti.service.GetDeploymentEvaluation(ctx, &gen.GetDeploymentEvaluationPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     "00000000-0000-0000-0000-000000000042",
	})
	require.Error(t, err)
}

func createOpenAPIv3Deployment(t *testing.T, ctx context.Context, ti *testInstance, key string, assets []*gen.AddOpenAPIv3DeploymentAssetForm) *gen.CreateDeploymentResult {
	t.Helper()

	res, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey:   key,
		Openapiv3Assets:  assets,
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
	require.Equal(t, "completed", res.Deployment.Status)

	return res
}

func TestDeploymentsService_GetDeploymentEvaluation_UnclonedDeploymentUsesPreviousCompleted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	todoID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/todo-valid.yaml")
	taggedID := uploadOpenAPIv3Fixture(t, ctx, ti, "fixtures/todo-with-tags.yaml")

	first := createOpenAPIv3Deployment(t, ctx, ti, "evaluation-base-first", []*gen.AddOpenAPIv3DeploymentAssetForm{{AssetID: todoID, Name: "todo", Slug: "todo"}})
	second := createOpenAPIv3Deployment(t, ctx, ti, "evaluation-base-second", []*gen.AddOpenAPIv3DeploymentAssetForm{{AssetID: taggedID, Name: "todo", Slug: "todo"}})

	res, err := ti.service.GetDeploymentEvaluation(ctx, &gen.GetDeploymentEvaluationPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     second.Deployment.ID,
	})
	require.NoError(t, err)
	require.Equal(t, first.Deployment.ID, *res.BaseDeploymentID)
	require.Len(t, res.Documents, 1)
	require.Equal(t, todoID, *res.Documents[0].BaseAssetID)
}

package deployments_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	deprepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDeploymentsService_DispatchRedeployCompletes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))

	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	asset, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ContentType:   "application/x-yaml",
		ContentLength: int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err)

	initial, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-dispatch-redeploy-initial",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{{
			AssetID: asset.Asset.ID,
			Name:    "initial-doc",
			Slug:    "initial-doc",
		}},
		Functions: []*gen.AddFunctionsForm{},
		Packages:  []*gen.AddDeploymentPackageForm{},
	})
	require.NoError(t, err)

	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction is the behavior under test.
	require.NoError(t, err)
	clone, err := ti.service.PrepareRedeploy(ctx, tx, &gen.RedeployPayload{DeploymentID: initial.Deployment.ID})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.NotNil(t, clone.IdempotencyKey)

	dispatched, err := ti.service.DispatchRedeploy(ctx, clone.ID, uuid.MustParse(initial.Deployment.ID))
	require.NoError(t, err)
	require.Equal(t, "created", dispatched.Deployment.Status)

	workflowID := fmt.Sprintf("v1:process-deployment:%s", *clone.IdempotencyKey)
	var workflowResult background.ProcessDeploymentWorkflowResult
	err = ti.temporalEnv.Client().GetWorkflow(ctx, workflowID, "").Get(ctx, &workflowResult)
	require.NoError(t, err)
	require.Equal(t, clone.ID, workflowResult.DeploymentID.String())
	require.Equal(t, "completed", workflowResult.Status)
	execution, err := ti.temporalEnv.Client().DescribeWorkflowExecution(ctx, workflowID, "")
	require.NoError(t, err)
	require.Equal(t, "ProcessGuardedDeploymentWorkflow", execution.WorkflowExecutionInfo.Type.Name)

	completed, err := ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{ID: clone.ID})
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Status)
}

func TestDeploymentsService_DispatchRedeployRejectsStaleActiveDeployment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))

	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	asset, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ContentType:   "application/x-yaml",
		ContentLength: int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err)

	deploymentPayload := func(idempotencyKey string) *gen.CreateDeploymentPayload {
		return &gen.CreateDeploymentPayload{
			IdempotencyKey: idempotencyKey,
			Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{{
				AssetID: asset.Asset.ID,
				Name:    "initial-doc",
				Slug:    "initial-doc",
			}},
			Functions: []*gen.AddFunctionsForm{},
			Packages:  []*gen.AddDeploymentPackageForm{},
		}
	}

	initial, err := ti.service.CreateDeployment(ctx, deploymentPayload("test-dispatch-redeploy-stale-initial"))
	require.NoError(t, err)

	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction is the behavior under test.
	require.NoError(t, err)
	clone, err := ti.service.PrepareRedeploy(ctx, tx, &gen.RedeployPayload{DeploymentID: initial.Deployment.ID})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.NotNil(t, clone.IdempotencyKey)

	newActive, err := ti.service.CreateDeployment(ctx, deploymentPayload("test-dispatch-redeploy-stale-new-active"))
	require.NoError(t, err)
	require.Equal(t, "completed", newActive.Deployment.Status)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	repo := deprepo.New(ti.conn)
	activeID, err := repo.GetActiveDeploymentID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Equal(t, newActive.Deployment.ID, activeID.String())

	before, err := repo.ListDeployments(ctx, deprepo.ListDeploymentsParams{
		ProjectID: *authCtx.ProjectID,
		Cursor:    uuid.NullUUID{},
	})
	require.NoError(t, err)

	dispatched, err := ti.service.DispatchRedeploy(ctx, clone.ID, uuid.MustParse(initial.Deployment.ID))
	require.NoError(t, err)
	require.Equal(t, "created", dispatched.Deployment.Status)

	workflowID := fmt.Sprintf("v1:process-deployment:%s", *clone.IdempotencyKey)
	var workflowResult background.ProcessDeploymentWorkflowResult
	err = ti.temporalEnv.Client().GetWorkflow(ctx, workflowID, "").Get(ctx, &workflowResult)
	require.NoError(t, err)
	require.Equal(t, clone.ID, workflowResult.DeploymentID.String())
	require.Equal(t, "failed", workflowResult.Status)

	failed, err := ti.service.GetDeployment(ctx, &gen.GetDeploymentPayload{ID: clone.ID})
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)

	activeID, err = repo.GetActiveDeploymentID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Equal(t, newActive.Deployment.ID, activeID.String())

	replayed, err := ti.service.DispatchRedeploy(ctx, clone.ID, uuid.MustParse(initial.Deployment.ID))
	require.NoError(t, err)
	require.Equal(t, clone.ID, replayed.Deployment.ID)
	require.Equal(t, "failed", replayed.Deployment.Status)

	after, err := repo.ListDeployments(ctx, deprepo.ListDeploymentsParams{
		ProjectID: *authCtx.ProjectID,
		Cursor:    uuid.NullUUID{},
	})
	require.NoError(t, err)
	require.Len(t, after, len(before))

}

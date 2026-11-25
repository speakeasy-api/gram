package deployments_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedCompletedDeployment inserts a completed deployment with no sources,
// without running the processing workflow.
func seedCompletedDeployment(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	q := repo.New(ti.conn)
	depID, err := q.InsertDeployment(ctx, repo.InsertDeploymentParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	require.NoError(t, q.CreateDeploymentStatus(ctx, repo.CreateDeploymentStatusParams{DeploymentID: depID, Status: "completed"}))

	return depID
}

func TestStaffRedeployer_CloneBecomesActiveAfterProcessing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	source := seedCompletedDeployment(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	redeployer := deployments.NewStaffRedeployer(testenv.NewLogger(t), testenv.NewTracerProvider(t), ti.conn, ti.temporalEnv, audit.NewLogger())
	staff := urn.NewPrincipal(urn.PrincipalTypeUser, "staff-subject")

	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction is the behavior under test.
	require.NoError(t, err)
	clone, err := redeployer.Prepare(ctx, tx, deployments.StaffRedeploy{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          projectID,
		SourceDeploymentID: source,
		Actor:              staff,
		ActorDisplayName:   new("Staff Member"),
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	activeBeforeProcessing, err := repo.New(ti.conn).GetActiveDeploymentID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, source, activeBeforeProcessing, "the source stays active until the clone succeeds")

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDeploymentsRedeploy)
	require.NoError(t, err)
	require.Equal(t, staff.ID, record.ActorID)

	cloneID := uuid.MustParse(clone.ID)
	// The clone is committed, so a client that has gone away must not strand it.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.NoError(t, redeployer.Dispatch(cancelled, projectID, cloneID, source))

	require.NotNil(t, clone.IdempotencyKey)
	workflowID := fmt.Sprintf("v1:process-deployment:%s", *clone.IdempotencyKey)
	var result background.ProcessDeploymentWorkflowResult
	require.NoError(t, ti.temporalEnv.Client().GetWorkflow(ctx, workflowID, "").Get(ctx, &result))
	require.Equal(t, "completed", result.Status)

	active, err := repo.New(ti.conn).GetActiveDeploymentID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, cloneID, active)
}

func TestStaffRedeployer_PrepareRefusesDeploymentFromAnotherProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	source := seedCompletedDeployment(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	redeployer := deployments.NewStaffRedeployer(testenv.NewLogger(t), testenv.NewTracerProvider(t), ti.conn, ti.temporalEnv, audit.NewLogger())

	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction is the behavior under test.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = redeployer.Prepare(ctx, tx, deployments.StaffRedeploy{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          uuid.New(),
		SourceDeploymentID: source,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, "staff-subject"),
		ActorDisplayName:   nil,
	})
	require.Error(t, err)
}

func TestStaffRedeployer_PrepareRequiresATransaction(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	redeployer := deployments.NewStaffRedeployer(testenv.NewLogger(t), testenv.NewTracerProvider(t), ti.conn, ti.temporalEnv, audit.NewLogger())

	_, err := redeployer.Prepare(ctx, nil, deployments.StaffRedeploy{
		OrganizationID:     "",
		ProjectID:          uuid.New(),
		SourceDeploymentID: uuid.New(),
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, "staff-subject"),
		ActorDisplayName:   nil,
	})
	require.ErrorContains(t, err, "transaction is required")
}

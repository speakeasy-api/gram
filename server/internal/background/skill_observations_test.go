package background

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestReconcileSkillObservationsWorkflowID(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	require.Equal(t, "v1:reconcile-skill-observations:"+projectID.String(), reconcileSkillObservationsWorkflowID(ReconcileSkillObservationsParams{ProjectID: projectID}))
}

func TestReconcileSkillObservationsWorkflowDrainsFullBatches(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	projectID := uuid.New()
	calls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ReconcileSkillObservationsParams) (*activities.ReconcileSkillObservationsResult, error) {
			calls++
			require.Equal(t, projectID, params.ProjectID)
			require.Equal(t, skillObservationBatchSize, params.BatchSize)
			return &activities.ReconcileSkillObservationsResult{Processed: int(params.BatchSize), HasMore: calls == 1}, nil
		},
		activity.RegisterOptions{Name: "ReconcileSkillObservations"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.SyncSkillSessionVersionsParams) (*activities.SyncSkillSessionVersionsResult, error) {
			require.Equal(t, projectID, params.ProjectID)
			require.Equal(t, skillObservationBatchSize, params.BatchSize)
			return &activities.SyncSkillSessionVersionsResult{}, nil
		},
		activity.RegisterOptions{Name: "SyncSkillSessionVersions"},
	)

	env.ExecuteWorkflow(ReconcileSkillObservationsWorkflow, ReconcileSkillObservationsParams{ProjectID: projectID})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, calls)
}

func TestReconcileSkillObservationsWorkflowBoundsBacklogDrain(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReconcileSkillObservationsParams) (*activities.ReconcileSkillObservationsResult, error) {
			calls++
			return &activities.ReconcileSkillObservationsResult{Processed: int(skillObservationBatchSize), HasMore: true}, nil
		},
		activity.RegisterOptions{Name: "ReconcileSkillObservations"},
	)

	env.ExecuteWorkflow(ReconcileSkillObservationsWorkflow, ReconcileSkillObservationsParams{ProjectID: uuid.New()})
	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.Equal(t, skillObservationMaxBatches, calls)
}

func TestReconcileSkillObservationsWorkflowDrainsSessionVersionMappingsAfterReconciliation(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	projectID := uuid.New()
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReconcileSkillObservationsParams) (*activities.ReconcileSkillObservationsResult, error) {
			return &activities.ReconcileSkillObservationsResult{}, nil
		},
		activity.RegisterOptions{Name: "ReconcileSkillObservations"},
	)
	syncCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.SyncSkillSessionVersionsParams) (*activities.SyncSkillSessionVersionsResult, error) {
			syncCalls++
			require.Equal(t, projectID, params.ProjectID)
			require.Equal(t, skillObservationBatchSize, params.BatchSize)
			return &activities.SyncSkillSessionVersionsResult{Processed: int(params.BatchSize), HasMore: syncCalls == 1}, nil
		},
		activity.RegisterOptions{Name: "SyncSkillSessionVersions"},
	)

	env.ExecuteWorkflow(ReconcileSkillObservationsWorkflow, ReconcileSkillObservationsParams{ProjectID: projectID})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, syncCalls)
}

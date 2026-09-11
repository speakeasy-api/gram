package background

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestIndexToolsetSweepWorkflowBoundsFanout(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
	targets := make([]activities.ToolsetIndexTarget, indexToolsetSweepStartLimit+2)
	for i := range targets {
		targets[i] = activities.ToolsetIndexTarget{
			ProjectID:      uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27"),
			ToolsetID:      uuid.MustParse(fmt.Sprintf("019c1a55-c04e-7b95-a840-b5054b45%04x", i)),
			ToolsetSlug:    types.Slug(fmt.Sprintf("toolset-%d", i)),
			ToolsetVersion: 1,
			DeploymentID:   uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e28"),
		}
	}

	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ListToolsetsForIndexingInput) ([]activities.ToolsetIndexTarget, error) {
			require.Equal(t, int32(indexToolsetSweepScanLimit), input.ScanLimit)
			require.NotZero(t, input.RotationSeed)
			require.Equal(t, []uuid.UUID{uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")}, input.ProjectIDs)
			return targets, nil
		},
		activity.RegisterOptions{Name: "ListToolsetsForIndexing"},
	)
	env.RegisterWorkflow(IndexToolsetWorkflow)
	env.OnWorkflow(IndexToolsetWorkflow, mock.Anything, mock.Anything).Return(nil).Times(indexToolsetSweepStartLimit)

	env.ExecuteWorkflow(IndexToolsetSweepWorkflow, IndexToolsetSweepParams{
		ProjectIDs: []uuid.UUID{uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestIndexToolsetSweepWorkflowDiscoversBoundedProjectPage(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	projectID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ListProjectsForToolsetIndexingInput) ([]uuid.UUID, error) {
			require.NotZero(t, input.RotationSeed)
			require.Equal(t, int32(indexToolsetSweepProjectLimit), input.ProjectLimit)
			return []uuid.UUID{projectID}, nil
		},
		activity.RegisterOptions{Name: "ListProjectsForToolsetIndexing"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ListToolsetsForIndexingInput) ([]activities.ToolsetIndexTarget, error) {
			require.Equal(t, []uuid.UUID{projectID}, input.ProjectIDs)
			return nil, nil
		},
		activity.RegisterOptions{Name: "ListToolsetsForIndexing"},
	)

	env.ExecuteWorkflow(IndexToolsetSweepWorkflow, IndexToolsetSweepParams{ProjectIDs: nil})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestUnexpectedIndexToolsetStartError_IgnoresAlreadyStarted(t *testing.T) {
	t.Parallel()

	err := serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "request-id", "run-id")
	require.NoError(t, unexpectedIndexToolsetStartError(err))
}

func TestUnexpectedIndexToolsetStartError_ReturnsUnexpectedError(t *testing.T) {
	t.Parallel()

	cause := errors.New("temporal unavailable")
	err := unexpectedIndexToolsetStartError(cause)
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "start toolset indexing workflow")
}

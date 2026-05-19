package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestOutboxGCWorkflow_PartialBatch(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	gcCallCount := 0
	var gotCutoff time.Time
	var gotBatchSize int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, cutoff time.Time, batchSize int32) (int64, error) {
			gcCallCount++
			if gcCallCount == 1 {
				gotCutoff = cutoff
				gotBatchSize = batchSize
				// Partial batch — fewer rows than limit, workflow should sleep then loop again.
				return int64(outboxGCBatchSize) - 1, nil
			}
			return 0, errStop
		},
		activity.RegisterOptions{Name: "GCOutboxProcessedRows"},
	)

	env.ExecuteWorkflow(OutboxGCWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError()) // stopped by sentinel
	require.Equal(t, 2, gcCallCount)         // first partial batch, second errStop after sleep
	require.Equal(t, outboxGCBatchSize, gotBatchSize)
	require.WithinDuration(t, time.Now().Add(-outboxGCRetentionPeriod), gotCutoff, time.Second)
}

func TestOutboxGCWorkflow_FullBatchSkipsSleep(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	gcCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ time.Time, _ int32) (int64, error) {
			gcCallCount++
			switch gcCallCount {
			case 1:
				// Full batch — workflow must NOT sleep before next poll.
				return int64(outboxGCBatchSize), nil
			case 2:
				// Partial batch — workflow sleeps, then loops.
				return 0, nil
			default:
				return 0, errStop
			}
		},
		activity.RegisterOptions{Name: "GCOutboxProcessedRows"},
	)

	env.ExecuteWorkflow(OutboxGCWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError()) // stopped by sentinel
	require.Equal(t, 3, gcCallCount)         // full batch → immediate re-poll → partial batch → sleep → errStop
}

func TestOutboxGCWorkflow_ActivityError(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ time.Time, _ int32) (int64, error) {
			return 0, errStop
		},
		activity.RegisterOptions{Name: "GCOutboxProcessedRows"},
	)

	env.ExecuteWorkflow(OutboxGCWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

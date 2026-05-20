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
			gotCutoff = cutoff
			gotBatchSize = batchSize
			// Partial batch — fewer rows than limit, workflow should return nil immediately.
			return int64(outboxGCBatchSize) - 1, nil
		},
		activity.RegisterOptions{Name: "GCOutboxProcessedRows"},
	)

	env.ExecuteWorkflow(OutboxGCWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, gcCallCount)
	require.Equal(t, outboxGCBatchSize, gotBatchSize)
	require.WithinDuration(t, time.Now().Add(-outboxGCRetentionPeriod), gotCutoff, time.Second)
}

func TestOutboxGCWorkflow_FullBatchContinues(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	gcCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ time.Time, _ int32) (int64, error) {
			gcCallCount++
			switch gcCallCount {
			case 1:
				// Full batch — workflow must immediately re-poll without sleeping.
				return int64(outboxGCBatchSize), nil
			default:
				// Partial batch — workflow returns nil.
				return 0, nil
			}
		},
		activity.RegisterOptions{Name: "GCOutboxProcessedRows"},
	)

	env.ExecuteWorkflow(OutboxGCWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, gcCallCount) // full batch → immediate re-poll → partial batch → return nil
}

func TestOutboxGCWorkflow_MultipleFullBatches(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	gcCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ time.Time, _ int32) (int64, error) {
			gcCallCount++
			if gcCallCount < 4 {
				return int64(outboxGCBatchSize), nil
			}
			return 0, nil
		},
		activity.RegisterOptions{Name: "GCOutboxProcessedRows"},
	)

	env.ExecuteWorkflow(OutboxGCWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 4, gcCallCount) // 3 full batches → immediate re-poll each time → partial → return nil
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

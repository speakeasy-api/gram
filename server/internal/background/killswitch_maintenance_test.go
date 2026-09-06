package background

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

func registerKillswitchMaintenanceActivities(env *testsuite.TestWorkflowEnvironment, expiry func(int32) (killswitches.ExpiryBatchResult, error), cleanup func(int32) (int64, error)) {
	env.RegisterActivityWithOptions(
		func(_ context.Context, batchSize int32) (killswitches.ExpiryBatchResult, error) {
			return expiry(batchSize)
		},
		activity.RegisterOptions{Name: "RecordDueKillswitchExpiries"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, batchSize int32) (int64, error) { return cleanup(batchSize) },
		activity.RegisterOptions{Name: "CleanupExpiredKillswitchOperations"},
	)
}

func TestKillswitchMaintenanceWorkflow_PartialBatches(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	expiryCalls, cleanupCalls := 0, 0
	var gotExpiryBatch, gotCleanupBatch int32
	registerKillswitchMaintenanceActivities(env,
		func(batchSize int32) (killswitches.ExpiryBatchResult, error) {
			expiryCalls++
			gotExpiryBatch = batchSize
			partial := int64(killswitchExpiryBatchSize) - 1
			return killswitches.ExpiryBatchResult{Candidates: partial, Recorded: partial}, nil
		},
		func(batchSize int32) (int64, error) {
			cleanupCalls++
			gotCleanupBatch = batchSize
			return 0, nil
		},
	)

	env.ExecuteWorkflow(KillswitchMaintenanceWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, expiryCalls)
	require.Equal(t, 1, cleanupCalls)
	require.Equal(t, killswitchExpiryBatchSize, gotExpiryBatch)
	require.Equal(t, killswitchCleanupBatchSize, gotCleanupBatch)
}

func TestKillswitchMaintenanceWorkflow_FullBatchesContinue(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	expiryCalls, cleanupCalls := 0, 0
	registerKillswitchMaintenanceActivities(env,
		func(int32) (killswitches.ExpiryBatchResult, error) {
			expiryCalls++
			if expiryCalls == 1 {
				// A full candidate batch that recorded fewer rows (raced
				// candidates) must still continue draining.
				return killswitches.ExpiryBatchResult{Candidates: int64(killswitchExpiryBatchSize), Recorded: int64(killswitchExpiryBatchSize) - 3}, nil
			}
			return killswitches.ExpiryBatchResult{Candidates: 0, Recorded: 0}, nil
		},
		func(int32) (int64, error) {
			cleanupCalls++
			if cleanupCalls == 1 {
				return int64(killswitchCleanupBatchSize), nil
			}
			return 0, nil
		},
	)

	env.ExecuteWorkflow(KillswitchMaintenanceWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, expiryCalls, "a full candidate batch continues draining")
	require.Equal(t, 2, cleanupCalls, "a full cleanup batch continues draining")
}

func TestKillswitchMaintenanceWorkflow_CleanupFailureDoesNotAffectExpiry(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	expiryCalls := 0
	registerKillswitchMaintenanceActivities(env,
		func(int32) (killswitches.ExpiryBatchResult, error) {
			expiryCalls++
			return killswitches.ExpiryBatchResult{Candidates: 0, Recorded: 0}, nil
		},
		func(int32) (int64, error) {
			return 0, errors.New("cleanup unavailable")
		},
	)

	env.ExecuteWorkflow(KillswitchMaintenanceWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError(), "cleanup failure surfaces for the schedule to retry")
	require.Equal(t, 1, expiryCalls, "expiry recording ran in its own activity and transaction before cleanup failed")
}

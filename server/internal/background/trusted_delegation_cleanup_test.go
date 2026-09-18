package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

func TestTrustedDelegationCleanupWorkflow(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			// Match other workflow tests: race instrumentation can delay the first yield.
			env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if fail {
					return errors.New("database unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: "CleanupTrustedDelegationCredentials"})
			env.ExecuteWorkflow(TrustedDelegationCleanupWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			if fail {
				require.Error(t, env.GetWorkflowError())
				require.Equal(t, 3, calls)
			} else {
				require.NoError(t, env.GetWorkflowError())
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestCleanupTrustedDelegationBatches(t *testing.T) {
	t.Parallel()
	t.Run("drains partial batch", func(t *testing.T) {
		calls := 0
		err := cleanupTrustedDelegationBatches(t.Context(), func(_ context.Context, limit int32) (int64, error) {
			require.Equal(t, int32(500), limit)
			calls++
			if calls == 1 {
				return int64(limit), nil
			}
			return 1, nil
		})
		require.NoError(t, err)
		require.Equal(t, 2, calls)
	})
	t.Run("per-attempt saturation", func(t *testing.T) {
		calls := 0
		err := cleanupTrustedDelegationBatches(t.Context(), func(_ context.Context, limit int32) (int64, error) {
			calls++
			return int64(limit), nil
		})
		require.ErrorContains(t, err, "budget exhausted")
		require.Equal(t, 100, calls)
	})
	t.Run("database failure", func(t *testing.T) {
		failure := errors.New("database unavailable")
		calls := 0
		err := cleanupTrustedDelegationBatches(t.Context(), func(context.Context, int32) (int64, error) {
			calls++
			return 0, failure
		})
		require.ErrorIs(t, err, failure)
		require.Equal(t, 1, calls)
	})
}

func TestTrustedDelegationCleanupRunTimeout(t *testing.T) {
	t.Parallel()
	// Three ten-minute attempts, one- and two-minute backoffs, seven minutes
	// of headroom for queueing and workflow tasks.
	require.Equal(t, 40*time.Minute, trustedDelegationCleanupRunTimeout())
}

// Exercise the actual workflow retry policy and batch loop together: saturation
// is retryable, and every activity attempt gets all 100 batches again.
func TestTrustedDelegationCleanupBudgetResetsPerAttempt(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
	var rowsPerAttempt []int64
	var attempts []int32
	env.RegisterActivityWithOptions(func(ctx context.Context) error {
		attempts = append(attempts, activity.GetInfo(ctx).Attempt)
		var rows int64
		err := cleanupTrustedDelegationBatches(ctx, func(_ context.Context, limit int32) (int64, error) {
			rows += int64(limit)
			return int64(limit), nil
		})
		rowsPerAttempt = append(rowsPerAttempt, rows)
		return err
	}, activity.RegisterOptions{Name: "CleanupTrustedDelegationCredentials"})
	env.ExecuteWorkflow(TrustedDelegationCleanupWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "per-attempt batch budget exhausted")
	require.Equal(t, []int32{1, 2, 3}, attempts)
	require.Equal(t, []int64{50_000, 50_000, 50_000}, rowsPerAttempt)
}

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

func TestPluginAPIKeyReapWorkflow(t *testing.T) {
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
			env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if fail {
					return errors.New("database unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: "ReapStalePluginAPIKeys"})
			env.ExecuteWorkflow(PluginAPIKeyReapWorkflow)
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

func TestReapPluginAPIKeyBatches(t *testing.T) {
	t.Parallel()

	t.Run("stops after a partial batch", func(t *testing.T) {
		t.Parallel()
		results := []int{200, 200, 7}
		calls := 0
		total, err := reapPluginAPIKeyBatches(t.Context(), func(_ context.Context, size int32) (int, error) {
			require.Equal(t, pluginAPIKeyReapBatchSize, size)
			n := results[calls]
			calls++
			return n, nil
		})
		require.NoError(t, err)
		require.Equal(t, 3, calls)
		require.Equal(t, 407, total)
	})

	t.Run("stops at the per-attempt budget", func(t *testing.T) {
		t.Parallel()
		calls := 0
		total, err := reapPluginAPIKeyBatches(t.Context(), func(context.Context, int32) (int, error) {
			calls++
			return int(pluginAPIKeyReapBatchSize), nil
		})
		require.NoError(t, err)
		require.Equal(t, pluginAPIKeyReapMaxBatchesPerAttempt, calls)
		require.Equal(t, pluginAPIKeyReapMaxBatchesPerAttempt*int(pluginAPIKeyReapBatchSize), total)
	})

	t.Run("returns progress with the error", func(t *testing.T) {
		t.Parallel()
		calls := 0
		total, err := reapPluginAPIKeyBatches(t.Context(), func(context.Context, int32) (int, error) {
			calls++
			if calls == 2 {
				return 0, errors.New("database unavailable")
			}
			return int(pluginAPIKeyReapBatchSize), nil
		})
		require.Error(t, err)
		require.Equal(t, int(pluginAPIKeyReapBatchSize), total)
	})
}

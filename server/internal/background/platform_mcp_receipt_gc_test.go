package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func runPlatformMCPReceiptGC(t *testing.T, deleted func(call int) int64) (int, []int32) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	calls := 0
	var sizes []int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, batchSize int32) (int64, error) {
			calls++
			sizes = append(sizes, batchSize)
			return deleted(calls), nil
		},
		activity.RegisterOptions{Name: "GCExpiredPlatformMCPReceipts"},
	)

	env.ExecuteWorkflow(PlatformMCPReceiptGCWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	return calls, sizes
}

func TestPlatformMCPReceiptGCWorkflow_PartialBatchStops(t *testing.T) {
	t.Parallel()
	calls, sizes := runPlatformMCPReceiptGC(t, func(int) int64 { return int64(platformMCPReceiptGCBatchSize) - 1 })
	require.Equal(t, 1, calls)
	require.Equal(t, []int32{platformMCPReceiptGCBatchSize}, sizes)
}

func TestPlatformMCPReceiptGCWorkflow_FullBatchContinues(t *testing.T) {
	t.Parallel()
	calls, _ := runPlatformMCPReceiptGC(t, func(call int) int64 {
		if call == 1 {
			return int64(platformMCPReceiptGCBatchSize)
		}
		return 0
	})
	require.Equal(t, 2, calls)
}

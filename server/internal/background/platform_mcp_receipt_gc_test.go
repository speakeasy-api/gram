package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func runPlatformMCPReceiptGC(t *testing.T, deleted func(call int) int64) (int, []int32) {
	t.Helper()
	calls, sizes, err := executePlatformMCPReceiptGC(t, deleted)
	require.NoError(t, err)
	return calls, sizes
}

func executePlatformMCPReceiptGC(t *testing.T, deleted func(call int) int64) (int, []int32, error) {
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
	return calls, sizes, env.GetWorkflowError()
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

// A backlog that keeps every batch full stops at the per-run budget and
// continues as new, rather than looping until the run timeout cuts it off.
func TestPlatformMCPReceiptGCWorkflow_BacklogContinuesAsNew(t *testing.T) {
	t.Parallel()
	calls, _, err := executePlatformMCPReceiptGC(t, func(int) int64 { return int64(platformMCPReceiptGCBatchSize) })
	require.Equal(t, platformMCPReceiptGCMaxBatchesPerRun, calls)
	var continued *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continued)
}

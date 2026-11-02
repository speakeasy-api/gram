package background

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

// runMemoryDigestsWorkflow runs the workflow against a fake activity that
// answers each call from results[level] in order, and returns the levels it
// was called with. Invalidation must run once, before any level, so it is
// checked here and left out of the returned calls.
func runMemoryDigestsWorkflow(t *testing.T, results map[string][]activities.BuildMemoryDigestsResult) []string {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var mu sync.Mutex
	var calls []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.BuildMemoryDigestsArgs) (activities.BuildMemoryDigestsResult, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, args.Level)
			queue := results[args.Level]
			if len(queue) == 0 {
				return activities.BuildMemoryDigestsResult{}, nil
			}
			results[args.Level] = queue[1:]
			return queue[0], nil
		},
		activity.RegisterOptions{Name: "BuildMemoryDigests"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "invalidate")
			return nil
		},
		activity.RegisterOptions{Name: "InvalidateMemoryDigests"},
	)

	env.ExecuteWorkflow(MemoryDigestsWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.NotEmpty(t, calls)
	require.Equal(t, "invalidate", calls[0])
	require.NotContains(t, calls[1:], "invalidate")
	return calls[1:]
}

func TestMemoryDigestsWorkflowRunsLevelsInOrderAndDrainsBatches(t *testing.T) {
	t.Parallel()

	calls := runMemoryDigestsWorkflow(t, map[string][]activities.BuildMemoryDigestsResult{
		"day": {{Built: 40, More: true}, {Built: 3}},
	})
	require.Equal(t, []string{"day", "day", "week", "month"}, calls)
}

func TestMemoryDigestsWorkflowStopsAboveAFailedLevel(t *testing.T) {
	t.Parallel()

	calls := runMemoryDigestsWorkflow(t, map[string][]activities.BuildMemoryDigestsResult{
		"day": {{Built: 2, Failed: 1}},
	})
	require.Equal(t, []string{"day"}, calls)
}

func TestMemoryDigestsWorkflowStopsWhenABatchMakesNoProgress(t *testing.T) {
	t.Parallel()

	calls := runMemoryDigestsWorkflow(t, map[string][]activities.BuildMemoryDigestsResult{
		"day": {{Skipped: 40, More: true}, {Built: 1}},
	})
	require.Equal(t, []string{"day", "week", "month"}, calls)
}

func TestMemoryDigestsWorkflowStopsAboveALevelLeftPending(t *testing.T) {
	t.Parallel()

	full := make([]activities.BuildMemoryDigestsResult, maxMemoryDigestBatchesPerLevel+1)
	for i := range full {
		full[i] = activities.BuildMemoryDigestsResult{Built: 40, More: true}
	}
	calls := runMemoryDigestsWorkflow(t, map[string][]activities.BuildMemoryDigestsResult{"day": full})
	require.Len(t, calls, maxMemoryDigestBatchesPerLevel)
	require.NotContains(t, calls, "week")
}

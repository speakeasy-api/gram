package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/dailyoutcomes"
)

func TestMCPConnectionOutcomesSweepBuildsEveryPlannedDay(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(MCPConnectionOutcomesDayWorkflow)

	first := time.Date(2026, time.November, 4, 0, 0, 0, 0, time.UTC)
	planned := make([]time.Time, 0, 6)
	for i := range 6 {
		planned = append(planned, first.AddDate(0, 0, i))
	}
	failing := planned[2]

	env.RegisterActivityWithOptions(func(context.Context, activities.PlanMCPConnectionOutcomeDaysArgs) ([]time.Time, error) {
		return planned, nil
	}, activity.RegisterOptions{Name: "PlanMCPConnectionOutcomeDays"})

	var mu sync.Mutex
	built := map[time.Time]int{}
	env.RegisterActivityWithOptions(func(_ context.Context, args activities.BuildMCPConnectionOutcomesDayArgs) (*dailyoutcomes.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		built[args.Day.UTC()]++
		if args.Day.Equal(failing) {
			return nil, errors.New("clickhouse unavailable")
		}
		return &dailyoutcomes.Result{Rows: 1, Attempts: 1, Swapped: true}, nil
	}, activity.RegisterOptions{Name: "BuildMCPConnectionOutcomesDay"})

	env.ExecuteWorkflow(MCPConnectionOutcomesSweepWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err, "a failed day fails the sweep so it shows up")
	require.Contains(t, err.Error(), failing.Format(time.DateOnly))
	for _, day := range planned {
		require.Positive(t, built[day], "every planned day is built, even after another fails: %s", day.Format(time.DateOnly))
	}
	require.Equal(t, mcpConnectionOutcomesBuildMaxAttempts, built[failing], "a failed build is retried")
}

func TestMCPConnectionOutcomesSweepSkipsADayStillBuilding(t *testing.T) {
	t.Parallel()
	require.True(t, dayStillBuilding(fmt.Errorf("start child: %w", &temporal.ChildWorkflowExecutionAlreadyStartedError{})))
	require.False(t, dayStillBuilding(errors.New("clickhouse unavailable")))
	require.False(t, dayStillBuilding(nil))
}

func TestMCPConnectionOutcomesDayWorkflowIDIsOnePerDay(t *testing.T) {
	t.Parallel()

	morning := time.Date(2026, time.November, 4, 1, 0, 0, 0, time.UTC)
	evening := time.Date(2026, time.November, 4, 23, 0, 0, 0, time.UTC)
	require.Equal(t, "v1:mcp-connection-outcomes-daily:2026-11-04", mcpConnectionOutcomesDayWorkflowID(morning))
	require.Equal(t, mcpConnectionOutcomesDayWorkflowID(morning), mcpConnectionOutcomesDayWorkflowID(evening))
}

func TestMCPConnectionOutcomesTimeoutsCoverEveryRead(t *testing.T) {
	t.Parallel()
	reads := dailyoutcomes.ReadsPerBuild * dailyoutcomes.ReadTimeout
	require.Greater(t, mcpConnectionOutcomesBuildTimeout, reads)
	require.Greater(t, mcpConnectionOutcomesPlanTimeout, reads)
	require.GreaterOrEqual(t, mcpConnectionOutcomesMaxWaves*mcpConnectionOutcomesConcurrency, dailyoutcomes.RawSafeDays+1, "every planned day fits in the waves")
	require.Greater(t, mcpConnectionOutcomesSweepRunTimeout, mcpConnectionOutcomesMaxWaves*mcpConnectionOutcomesDayRunTimeout)
	require.Less(t, mcpConnectionOutcomesSweepRunTimeout, mcpConnectionOutcomesCatchupWindow, "a worst-case sweep ends before the next run")
}

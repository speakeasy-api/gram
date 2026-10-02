package background

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestWorkers_Run_RegistersSchedules(t *testing.T) {
	t.Parallel()

	env := newSharedQueueTemporalEnv(t)
	workers := newSchedulingWorkers(t, env)

	interrupt := make(chan any)
	runErr := make(chan error, 1)
	go func() { runErr <- workers.Run(interrupt) }()
	t.Cleanup(func() {
		close(interrupt)
		require.NoError(t, <-runErr, "run workers")
	})

	ctx := t.Context()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ids, err := scheduleIDs(ctx, env.Client())
		if !assert.NoError(c, err, "list temporal schedules") {
			return
		}

		assert.Subset(c, ids, []string{
			killswitchMaintenanceScheduleID,
			assistantReaperScheduleID,
			chatAnalysisSweepScheduleID,
			aiUsagePollerCoordinatorScheduleID,
			deviceIntegrationSyncCoordinatorScheduleID,
			indexToolsetSweepScheduleID(string(env.Queue())),
		}, "the long-running worker owns the recurring sweeps")
	}, 60*time.Second, 250*time.Millisecond, "Run should register the recurring schedules")
}

func TestWorkers_Start_DoesNotRegisterSchedules(t *testing.T) {
	t.Parallel()

	env, _ := infra.NewTemporalEnv(t)
	workers := newSchedulingWorkers(t, env)

	require.NoError(t, workers.Start(), "start workers")
	t.Cleanup(workers.Stop)

	ids, err := scheduleIDs(t.Context(), env.Client())
	require.NoError(t, err, "list temporal schedules")
	require.Empty(t, ids, "Start is the entrypoint test suites use, and it must stay schedule-free: they build a namespace per test case, and ~20 scheduler workflows in each one swamps the shared dev server")
}

// newSharedQueueTemporalEnv returns a fresh test namespace whose worker polls
// the shared queue, which is the only queue that registers the fleet-wide
// schedules. The namespace is per test, so sharing the queue name is safe.
func newSharedQueueTemporalEnv(t *testing.T) *tenv.Environment {
	t.Helper()
	env, _ := infra.NewTemporalEnv(t)
	return tenv.NewEnvironment(env.Client(), env.Namespace(), sharedTaskQueue)
}

// newSchedulingWorkers builds a worker set whose only exercised behaviour is
// schedule registration. Every dependency is nil because the sweeps those
// schedules fire are not under test; ForDeploymentProcessing is here solely for
// the noop publishers it supplies.
func newSchedulingWorkers(t *testing.T, env *tenv.Environment) *Workers {
	t.Helper()

	return NewTemporalWorker(
		env,
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ForDeploymentProcessing(nil, nil, nil, nil, nil, nil, nil, nil),
	)
}

func scheduleIDs(ctx context.Context, c client.Client) ([]string, error) {
	iter, err := c.ScheduleClient().List(ctx, client.ScheduleListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}

	ids := make([]string, 0)
	for iter.HasNext() {
		entry, err := iter.Next()
		if err != nil {
			return nil, fmt.Errorf("read schedule list entry: %w", err)
		}

		ids = append(ids, entry.ID)
	}

	return ids, nil
}

// PR previews poll their own queue in the dev namespace. They must register
// only the schedules whose ID names their queue, never the fixed-ID ones that
// dev owns, or they re-point dev's schedules at the preview queue.
func TestWorkers_RegisterSchedulesSkipsFleetWideOnPreviewQueue(t *testing.T) {
	t.Parallel()
	env, _ := infra.NewTemporalEnv(t)
	ctx := t.Context()
	newSchedulingWorkers(t, env).registerSchedules(ctx)

	queue := string(env.Queue())
	want := []string{
		slackDirectorySweepScheduleID(env.Queue()),
		oktaApplicationSyncCoordinatorScheduleID(queue),
		fmt.Sprintf("v1:trusted-delegation-cleanup:%s", queue),
		fmt.Sprintf("v1:tenant-dimensions-sync:%s", queue),
		indexToolsetSweepScheduleID(queue),
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ids, err := scheduleIDs(ctx, env.Client())
		assert.NoError(c, err)
		assert.ElementsMatch(c, want, ids)
	}, 30*time.Second, 250*time.Millisecond)
}

func TestWorkers_RegisterSchedulesPreservesManualPauses(t *testing.T) {
	t.Parallel()
	env := newSharedQueueTemporalEnv(t)
	workers := newSchedulingWorkers(t, env)
	ctx := t.Context()
	workers.registerSchedules(ctx)
	var ids []string
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		ids, err = scheduleIDs(ctx, env.Client())
		assert.NoError(c, err)
		assert.Len(c, ids, 27, "all unconditional schedules should be registered")
		assert.Contains(c, ids, fmt.Sprintf("v1:trusted-delegation-cleanup:%s", env.Queue()), "delegation cleanup is an unconditional schedule")
		assert.Contains(c, ids, fmt.Sprintf("v1:mcp-finding-evidence-cleanup:%s", env.Queue()), "MCP finding evidence cleanup is an unconditional schedule")
	}, 30*time.Second, 250*time.Millisecond)
	sc := env.Client().ScheduleClient()
	windows := make(map[string]time.Duration, len(ids))
	for _, id := range ids {
		handle := sc.GetHandle(ctx, id)
		description, err := handle.Describe(ctx)
		require.NoError(t, err)
		window := description.Schedule.Policy.CatchupWindow
		require.GreaterOrEqual(t, window, 10*time.Second, id)
		for _, interval := range description.Schedule.Spec.Intervals {
			require.Less(t, window, interval.Every, id)
		}
		windows[id] = window
		// Simulate an existing schedule with the server-default catchup window and
		// an operator pause. Both create-only and reconciled registrations must
		// migrate the policy without dropping the pause or its note.
		require.NoError(t, handle.Pause(ctx, client.SchedulePauseOptions{Note: "operator maintenance"}))
		require.NoError(t, handle.Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Policy.CatchupWindow = 365 * 24 * time.Hour
				return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
			},
		}))
	}
	workers.registerSchedules(ctx)
	for _, id := range ids {
		description, err := sc.GetHandle(ctx, id).Describe(ctx)
		require.NoError(t, err)
		require.Equal(t, windows[id], description.Schedule.Policy.CatchupWindow, id)
		require.True(t, description.Schedule.State.Paused, id)
		require.Equal(t, "operator maintenance", description.Schedule.State.Note, id)
	}
}

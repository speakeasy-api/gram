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

	env, _ := infra.NewTemporalEnv(t)
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
			outboxGCScheduleID,
			assistantReaperScheduleID,
			chatAnalysisSweepScheduleID,
			aiUsagePollerCoordinatorScheduleID,
			deviceIntegrationSyncCoordinatorScheduleID,
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

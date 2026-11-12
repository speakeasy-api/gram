package background

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"

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
			memoryDigestsScheduleID,
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
		ForDeploymentProcessing(nil, nil, nil, nil, nil, nil, nil),
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

// scheduleCreateRecorder is a Temporal client interceptor that records every
// schedule the client creates. Registration tests read schedule IDs from it
// instead of from the schedule list: listing is served by eventually
// consistent visibility, which can briefly omit a schedule that was just
// created, so an exact-set assertion polled against it can pass while the
// expected set is stale and then fail on later, unrelated runs.
type scheduleCreateRecorder struct {
	interceptor.ClientInterceptorBase

	mu sync.Mutex

	// created holds the IDs of schedules the server created, in order.
	created []string

	// failed holds creation errors by schedule ID, excluding
	// temporal.ErrScheduleAlreadyRunning, which re-registration expects.
	failed map[string]error
}

func (r *scheduleCreateRecorder) InterceptClient(next interceptor.ClientOutboundInterceptor) interceptor.ClientOutboundInterceptor {
	return &scheduleCreateRecorderOutbound{
		ClientOutboundInterceptorBase: interceptor.ClientOutboundInterceptorBase{Next: next},
		recorder:                      r,
	}
}

func (r *scheduleCreateRecorder) record(id string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch {
	case err == nil:
		r.created = append(r.created, id)
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
	default:
		r.failed[id] = err
	}
}

// requireCreated returns the IDs of the schedules created so far and fails the
// test if any creation was rejected.
func (r *scheduleCreateRecorder) requireCreated(t *testing.T) []string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()

	require.Empty(t, r.failed, "every schedule registration should succeed")
	return slices.Clone(r.created)
}

type scheduleCreateRecorderOutbound struct {
	interceptor.ClientOutboundInterceptorBase

	recorder *scheduleCreateRecorder
}

func (o *scheduleCreateRecorderOutbound) CreateSchedule(ctx context.Context, in *interceptor.ScheduleClientCreateInput) (client.ScheduleHandle, error) {
	handle, err := o.Next.CreateSchedule(ctx, in)
	o.recorder.record(in.Options.ID, err)
	if err != nil {
		return nil, fmt.Errorf("create schedule %s: %w", in.Options.ID, err)
	}
	return handle, nil
}

// withScheduleCreateRecorder returns a copy of env whose client records the
// schedules it creates. The copy shares env's connection and namespace.
func withScheduleCreateRecorder(t *testing.T, env *tenv.Environment) (*tenv.Environment, *scheduleCreateRecorder) {
	t.Helper()

	recorder := &scheduleCreateRecorder{
		ClientInterceptorBase: interceptor.ClientInterceptorBase{},
		mu:                    sync.Mutex{},
		created:               nil,
		failed:                map[string]error{},
	}
	c, err := client.NewClientFromExisting(env.Client(), client.Options{
		Namespace:    string(env.Namespace()),
		Logger:       testenv.NewLogger(t),
		Interceptors: []interceptor.ClientInterceptor{recorder},
	})
	require.NoError(t, err, "create recording temporal client")
	t.Cleanup(c.Close)

	return tenv.NewEnvironment(c, env.Namespace(), env.Queue()), recorder
}

// PR previews poll their own queue in the dev namespace. They must register
// only the schedules whose ID names their queue, never the fixed-ID ones that
// dev owns, or they re-point dev's schedules at the preview queue.
func TestWorkers_RegisterSchedulesSkipsFleetWideOnPreviewQueue(t *testing.T) {
	t.Parallel()
	previewEnv, _ := infra.NewTemporalEnv(t)
	env, recorder := withScheduleCreateRecorder(t, previewEnv)
	newSchedulingWorkers(t, env).registerSchedules(t.Context())

	queue := string(env.Queue())
	require.ElementsMatch(t, []string{
		slackDirectorySweepScheduleID(env.Queue()),
		oktaApplicationSyncCoordinatorScheduleID(queue),
		fmt.Sprintf("v1:trusted-delegation-cleanup:%s", queue),
		fmt.Sprintf("v1:channels-retention:%s", queue),
		fmt.Sprintf("v1:tenant-dimensions-sync:%s", queue),
		indexToolsetSweepScheduleID(queue),
	}, recorder.requireCreated(t))
}

func TestWorkers_RegisterSchedulesPreservesManualPauses(t *testing.T) {
	t.Parallel()
	env, recorder := withScheduleCreateRecorder(t, newSharedQueueTemporalEnv(t))
	workers := newSchedulingWorkers(t, env)
	ctx := t.Context()
	workers.registerSchedules(ctx)

	ids := recorder.requireCreated(t)
	queue := string(env.Queue())
	require.Subset(t, ids, []string{
		killswitchMaintenanceScheduleID,
		assistantReaperScheduleID,
		chatAnalysisSweepScheduleID,
		organizationEncryptionSweepScheduleID(queue),
		internalSessionIssuerKeySweepScheduleID(queue),
		fmt.Sprintf("v1:directory-review-emails:%s", queue),
		fmt.Sprintf("v1:trusted-delegation-cleanup:%s", queue),
		fmt.Sprintf("v1:mcp-finding-evidence-cleanup:%s", queue),
		fmt.Sprintf("v1:channels-retention:%s", queue),
		indexToolsetSweepScheduleID(queue),
	}, "the shared queue registers both the fleet-wide and the per-queue schedules")

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
	require.Equal(t, ids, recorder.requireCreated(t), "re-registration should find every schedule already present")
	for _, id := range ids {
		description, err := sc.GetHandle(ctx, id).Describe(ctx)
		require.NoError(t, err)
		require.Equal(t, windows[id], description.Schedule.Policy.CatchupWindow, id)
		require.True(t, description.Schedule.State.Paused, id)
		require.Equal(t, "operator maintenance", description.Schedule.State.Note, id)
	}
}

package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestAssistantThreadWorkflowBacksOffBeforeRetryAdmission(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	env.SetStartTime(start)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessAssistantThreadInput) (*activities.ProcessAssistantThreadResult, error) {
			return &activities.ProcessAssistantThreadResult{
				AssistantID:       "11111111-1111-1111-1111-111111111111",
				WarmUntil:         "",
				RuntimeActive:     true,
				RetryAdmission:    true,
				ProcessedAnyEvent: false,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessAssistantThread"},
	)

	// A kick during the durable timer cannot bypass the backoff.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAssistantThreadKick, struct{}{})
	}, time.Second)
	var signalTime time.Time
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalAssistantCoordinatorInput) error {
			signalTime = env.Now()
			return nil
		},
		activity.RegisterOptions{Name: "SignalAssistantCoordinator"},
	)

	env.ExecuteWorkflow(AssistantThreadWorkflow, AssistantThreadWorkflowInput{
		ThreadID:  "22222222-2222-2222-2222-222222222222",
		ProjectID: "33333333-3333-3333-3333-333333333333",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.GreaterOrEqual(t, signalTime.Sub(start), assistantRetryAdmissionBackoff)
}

// warmThreadWorkflowEnv runs AssistantThreadWorkflow against one processed
// turn with a 60s warm window. expire answers each ExpireAssistantThreadRuntime
// call in order; the returned counters record activity calls.
func warmThreadWorkflowEnv(t *testing.T, expire []activities.ExpireAssistantThreadRuntimeResult) (env *testsuite.TestWorkflowEnvironment, start time.Time, expireTimes *[]time.Time, signalCalls *atomic.Int32) {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env = suite.NewTestWorkflowEnvironment()
	start = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	env.SetStartTime(start)

	warmUntil := start.Add(60 * time.Second).Format(time.RFC3339Nano)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessAssistantThreadInput) (*activities.ProcessAssistantThreadResult, error) {
			return &activities.ProcessAssistantThreadResult{
				AssistantID:       "11111111-1111-1111-1111-111111111111",
				WarmUntil:         warmUntil,
				WarmTTLSeconds:    60,
				RuntimeActive:     true,
				RetryAdmission:    false,
				ProcessedAnyEvent: true,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessAssistantThread"},
	)

	expireTimes = &[]time.Time{}
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ExpireAssistantThreadRuntimeInput) (*activities.ExpireAssistantThreadRuntimeResult, error) {
			require.Equal(t, 60, input.WarmTTLSeconds)
			call := len(*expireTimes)
			*expireTimes = append(*expireTimes, env.Now())
			require.Less(t, call, len(expire), "unexpected ExpireAssistantThreadRuntime call")
			return &expire[call], nil
		},
		activity.RegisterOptions{Name: "ExpireAssistantThreadRuntime"},
	)

	signalCalls = &atomic.Int32{}
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalAssistantCoordinatorInput) error {
			signalCalls.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "SignalAssistantCoordinator"},
	)

	return env, start, expireTimes, signalCalls
}

func TestAssistantThreadWorkflowExpiresRuntimeOnWarmTimer(t *testing.T) {
	t.Parallel()

	env, start, expireTimes, signalCalls := warmThreadWorkflowEnv(t, []activities.ExpireAssistantThreadRuntimeResult{
		{Stopped: true, RemainingSeconds: 0},
	})

	env.ExecuteWorkflow(AssistantThreadWorkflow, AssistantThreadWorkflowInput{
		ThreadID:  "22222222-2222-2222-2222-222222222222",
		ProjectID: "33333333-3333-3333-3333-333333333333",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Len(t, *expireTimes, 1)
	require.GreaterOrEqual(t, (*expireTimes)[0].Sub(start), 60*time.Second)
	require.Equal(t, int32(2), signalCalls.Load(), "the coordinator is kicked after the turn and again after the runtime stops")
}

func TestAssistantThreadWorkflowRearmsExpiryWhenTurnSlipsIn(t *testing.T) {
	t.Parallel()

	env, start, expireTimes, _ := warmThreadWorkflowEnv(t, []activities.ExpireAssistantThreadRuntimeResult{
		{Stopped: false, RemainingSeconds: 30},
		{Stopped: true, RemainingSeconds: 0},
	})

	env.ExecuteWorkflow(AssistantThreadWorkflow, AssistantThreadWorkflowInput{
		ThreadID:  "22222222-2222-2222-2222-222222222222",
		ProjectID: "33333333-3333-3333-3333-333333333333",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Len(t, *expireTimes, 2)
	require.GreaterOrEqual(t, (*expireTimes)[1].Sub(start), 90*time.Second, "the second expire waits out the remaining window")
}

func TestAssistantThreadWorkflowVersion2ExitsOnWarmTimerWithoutExpire(t *testing.T) {
	t.Parallel()

	env, _, expireTimes, signalCalls := warmThreadWorkflowEnv(t, nil)
	env.OnGetVersion("expire-toctou-revert", workflow.DefaultVersion, 3).Return(workflow.Version(2))

	env.ExecuteWorkflow(AssistantThreadWorkflow, AssistantThreadWorkflowInput{
		ThreadID:  "22222222-2222-2222-2222-222222222222",
		ProjectID: "33333333-3333-3333-3333-333333333333",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Empty(t, *expireTimes, "version 2 histories must not call ExpireAssistantThreadRuntime")
	require.Equal(t, int32(1), signalCalls.Load(), "ProcessedAnyEvent must kick the coordinator so held-back pending siblings get re-evaluated")
}

func TestAssistantThreadWorkflowRetryAdmissionCancellation(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var attempts atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessAssistantThreadInput) (*activities.ProcessAssistantThreadResult, error) {
			if attempts.Add(1) == 1 {
				return nil, errors.New("transient worker failure")
			}
			return &activities.ProcessAssistantThreadResult{
				AssistantID:    "11111111-1111-1111-1111-111111111111",
				RuntimeActive:  true,
				RetryAdmission: true,
			}, nil
		}, activity.RegisterOptions{Name: "ProcessAssistantThread"},
	)
	var signals atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalAssistantCoordinatorInput) error {
			signals.Add(1)
			return nil
		}, activity.RegisterOptions{Name: "SignalAssistantCoordinator"},
	)
	// Activity retry is 5s; cancel inside the subsequent 30s admission timer.
	env.RegisterDelayedCallback(env.CancelWorkflow, 20*time.Second)
	env.ExecuteWorkflow(AssistantThreadWorkflow, AssistantThreadWorkflowInput{
		ThreadID:  "22222222-2222-2222-2222-222222222222",
		ProjectID: "33333333-3333-3333-3333-333333333333",
	})
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, int32(2), attempts.Load())
	require.Zero(t, signals.Load(), "cancellation must not enqueue a new coordinator kick")
}

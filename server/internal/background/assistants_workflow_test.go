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

func TestAssistantThreadWorkflowExitsOnWarmTimerWithoutExpire(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
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

	var expireCalls atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ExpireAssistantThreadRuntimeInput) (*activities.ExpireAssistantThreadRuntimeResult, error) {
			expireCalls.Add(1)
			return &activities.ExpireAssistantThreadRuntimeResult{Stopped: true}, nil
		},
		activity.RegisterOptions{Name: "ExpireAssistantThreadRuntime"},
	)

	var signalCalls atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalAssistantCoordinatorInput) error {
			signalCalls.Add(1)
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
	require.Equal(t, int32(0), expireCalls.Load(), "warm-timer exit must not call ExpireAssistantThreadRuntime")
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

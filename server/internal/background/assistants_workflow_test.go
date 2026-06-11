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
				RuntimeActive:     false,
				RetryAdmission:    true,
				ProcessedAnyEvent: false,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessAssistantThread"},
	)

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

func TestAssistantRuntimeWarmupWorkflowExpiresBootedRuntime(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	env.SetStartTime(start)

	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.WarmAssistantRuntimeInput) (*activities.WarmAssistantRuntimeResult, error) {
			require.Equal(t, "11111111-1111-1111-1111-111111111111", input.AssistantID)
			return &activities.WarmAssistantRuntimeResult{
				Booted:         true,
				ProjectID:      "33333333-3333-3333-3333-333333333333",
				WarmTTLSeconds: 60,
			}, nil
		},
		activity.RegisterOptions{Name: "WarmAssistantRuntime"},
	)

	var expireTime time.Time
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ExpireWarmupAssistantRuntimeInput) (*activities.ExpireAssistantThreadRuntimeResult, error) {
			expireTime = env.Now()
			require.Equal(t, 60, input.WarmTTLSeconds)
			return &activities.ExpireAssistantThreadRuntimeResult{Stopped: true, RemainingSeconds: 0}, nil
		},
		activity.RegisterOptions{Name: "ExpireWarmupAssistantRuntime"},
	)

	var signalCalls atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalAssistantCoordinatorInput) error {
			signalCalls.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "SignalAssistantCoordinator"},
	)

	env.ExecuteWorkflow(AssistantRuntimeWarmupWorkflow, AssistantRuntimeWarmupWorkflowInput{
		AssistantID: "11111111-1111-1111-1111-111111111111",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.GreaterOrEqual(t, expireTime.Sub(start), 60*time.Second, "expire must wait out the warm TTL")
	require.Equal(t, int32(2), signalCalls.Load(), "coordinator must be kicked after warmup and again after the stop")
}

func TestAssistantRuntimeWarmupWorkflowHandsOffExpiryWhenTurnArrived(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.WarmAssistantRuntimeInput) (*activities.WarmAssistantRuntimeResult, error) {
			return &activities.WarmAssistantRuntimeResult{
				Booted:         true,
				ProjectID:      "33333333-3333-3333-3333-333333333333",
				WarmTTLSeconds: 60,
			}, nil
		},
		activity.RegisterOptions{Name: "WarmAssistantRuntime"},
	)

	var expireCalls atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ExpireWarmupAssistantRuntimeInput) (*activities.ExpireAssistantThreadRuntimeResult, error) {
			expireCalls.Add(1)
			return &activities.ExpireAssistantThreadRuntimeResult{Stopped: false, RemainingSeconds: 45}, nil
		},
		activity.RegisterOptions{Name: "ExpireWarmupAssistantRuntime"},
	)

	var signalCalls atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalAssistantCoordinatorInput) error {
			signalCalls.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "SignalAssistantCoordinator"},
	)

	env.ExecuteWorkflow(AssistantRuntimeWarmupWorkflow, AssistantRuntimeWarmupWorkflowInput{
		AssistantID: "11111111-1111-1111-1111-111111111111",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, int32(1), expireCalls.Load(), "a revert means a turn arrived and its thread workflow owns expiry — no re-arm")
	require.Equal(t, int32(2), signalCalls.Load(), "a revert must still kick the coordinator for threads enqueued during the expiring window")
}

func TestAssistantRuntimeWarmupWorkflowExpiresEvenWhenCoordinatorSignalFails(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.WarmAssistantRuntimeInput) (*activities.WarmAssistantRuntimeResult, error) {
			return &activities.WarmAssistantRuntimeResult{
				Booted:         true,
				ProjectID:      "33333333-3333-3333-3333-333333333333",
				WarmTTLSeconds: 60,
			}, nil
		},
		activity.RegisterOptions{Name: "WarmAssistantRuntime"},
	)

	var expireCalls atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ExpireWarmupAssistantRuntimeInput) (*activities.ExpireAssistantThreadRuntimeResult, error) {
			expireCalls.Add(1)
			return &activities.ExpireAssistantThreadRuntimeResult{Stopped: true, RemainingSeconds: 0}, nil
		},
		activity.RegisterOptions{Name: "ExpireWarmupAssistantRuntime"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalAssistantCoordinatorInput) error {
			return errors.New("temporal client outage")
		},
		activity.RegisterOptions{Name: "SignalAssistantCoordinator"},
	)

	env.ExecuteWorkflow(AssistantRuntimeWarmupWorkflow, AssistantRuntimeWarmupWorkflowInput{
		AssistantID: "11111111-1111-1111-1111-111111111111",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, int32(1), expireCalls.Load(), "a failed coordinator kick must not leave the booted VM without an expiry")
}

func TestAssistantRuntimeWarmupWorkflowSignalsCoordinatorOnWarmupFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.WarmAssistantRuntimeInput) (*activities.WarmAssistantRuntimeResult, error) {
			return nil, errors.New("db down")
		},
		activity.RegisterOptions{Name: "WarmAssistantRuntime"},
	)

	var signalCalls atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalAssistantCoordinatorInput) error {
			signalCalls.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "SignalAssistantCoordinator"},
	)

	env.ExecuteWorkflow(AssistantRuntimeWarmupWorkflow, AssistantRuntimeWarmupWorkflowInput{
		AssistantID: "11111111-1111-1111-1111-111111111111",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, int32(1), signalCalls.Load(), "threads held back by the starting row need a kick even when warmup fails")
}

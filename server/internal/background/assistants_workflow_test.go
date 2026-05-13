package background

import (
	"context"
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

// Pins the v1 expire-toctou-revert branch: when ExpireAssistantThreadRuntime
// reports a turn slipped in past the warm timer, the workflow must re-arm and
// retry instead of falling through to SignalAssistantCoordinator.
func TestAssistantThreadWorkflowRearmsOnExpireRevert(t *testing.T) {
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
			n := expireCalls.Add(1)
			if n == 1 {
				return &activities.ExpireAssistantThreadRuntimeResult{Stopped: false, RemainingSeconds: 30}, nil
			}
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
	require.Equal(t, int32(2), expireCalls.Load(), "second expire must run after revert re-arm")
	require.Equal(t, int32(1), signalCalls.Load(), "coordinator signal fires once after the runtime is finally stopped")
}

package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	relay "github.com/speakeasy-api/gram/server/internal/background/activities/publish_outbox"
)

// haltLoop ends the workflow's otherwise endless poll loop. It must be
// non-retryable: a plain error would be retried five times by the activity
// retry policy, inflating call counts and scheduling backoff timers that look
// like the workflow's own idle sleep.
func haltLoop() error {
	return temporal.NewNonRetryableApplicationError("halt", "halt", nil)
}

// registerDrain stubs the single drain activity and returns a pointer to the
// call counter so tests can assert on the poll loop's shape.
func registerDrain(env *testsuite.TestWorkflowEnvironment, fn func(call int) (relay.DrainResult, error)) *int {
	calls := 0
	env.RegisterActivityWithOptions(
		func(context.Context) (relay.DrainResult, error) {
			calls++
			return fn(calls)
		},
		activity.RegisterOptions{Name: "DrainPublishOutbox"},
	)

	return &calls
}

func TestPublishOutboxWorkflow_SleepsWhenDrained(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var slept time.Duration
	env.SetOnTimerScheduledListener(func(_ string, d time.Duration) { slept = d })

	calls := registerDrain(env, func(call int) (relay.DrainResult, error) {
		if call >= 2 {
			// Stop the otherwise endless loop.
			return relay.DrainResult{}, haltLoop()
		}
		return relay.DrainResult{Published: 3, HasMore: false}, nil
	})

	env.ExecuteWorkflow(PublishOutboxWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Equal(t, 2, *calls)
	require.Equal(t, publishOutboxIdleInterval, slept,
		"an empty batch should back off rather than spin on the database")
}

func TestPublishOutboxWorkflow_PollsImmediatelyWhenMoreRemain(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	timerScheduled := false
	env.SetOnTimerScheduledListener(func(string, time.Duration) { timerScheduled = true })

	calls := registerDrain(env, func(call int) (relay.DrainResult, error) {
		if call >= 3 {
			return relay.DrainResult{}, haltLoop()
		}
		return relay.DrainResult{Published: 50, HasMore: true}, nil
	})

	env.ExecuteWorkflow(PublishOutboxWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Equal(t, 3, *calls)
	require.False(t, timerScheduled,
		"a backlog should be drained without waiting out the idle interval")
}

func TestPublishOutboxWorkflow_PropagatesDrainError(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	registerDrain(env, func(int) (relay.DrainResult, error) {
		return relay.DrainResult{}, temporal.NewNonRetryableApplicationError("database is down", "db", nil)
	})

	env.ExecuteWorkflow(PublishOutboxWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.Contains(t, err.Error(), "drain publish outbox")
}

func TestPublishOutboxGCWorkflow_PartialBatchStops(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	calls := 0
	var gotCutoff time.Time
	var gotBatchSize int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, cutoff time.Time, batchSize int32) (int64, error) {
			calls++
			gotCutoff = cutoff
			gotBatchSize = batchSize
			return int64(publishOutboxGCBatchSize) - 1, nil
		},
		activity.RegisterOptions{Name: "GCPublishOutboxDeadLetters"},
	)

	env.ExecuteWorkflow(PublishOutboxGCWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, calls)
	require.Equal(t, publishOutboxGCBatchSize, gotBatchSize)
	require.WithinDuration(t, time.Now().Add(-publishOutboxGCRetentionPeriod), gotCutoff, time.Second)
}

func TestPublishOutboxGCWorkflow_FullBatchContinues(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	calls := 0
	env.RegisterActivityWithOptions(
		func(context.Context, time.Time, int32) (int64, error) {
			calls++
			if calls == 1 {
				return int64(publishOutboxGCBatchSize), nil
			}
			return 0, nil
		},
		activity.RegisterOptions{Name: "GCPublishOutboxDeadLetters"},
	)

	env.ExecuteWorkflow(PublishOutboxGCWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, calls)
}

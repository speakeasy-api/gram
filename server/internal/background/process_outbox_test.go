package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	relay "github.com/speakeasy-api/gram/server/internal/background/activities/outbox_relay"
)

// errStop is a non-retryable sentinel used to break the workflow's infinite loop in tests.
var errStop = temporal.NewNonRetryableApplicationError("test-stop", "test-stop", nil)

func TestProcessOutboxWorkflow_EmptyBatch(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	fetchCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ relay.FetchEventArgs) (relay.FetchEventsResult, error) {
			fetchCallCount++
			if fetchCallCount == 1 {
				return relay.FetchEventsResult{Events: nil, HasMore: false}, nil
			}
			return relay.FetchEventsResult{}, errStop
		},
		activity.RegisterOptions{Name: "FetchPendingOutboxEvents"},
	)

	filterCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []*relay.Event) ([]*relay.Event, error) {
			filterCallCount++
			return nil, nil
		},
		activity.RegisterOptions{Name: "FilterNoopOutboxEvents"},
	)

	relayCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []*relay.Event) error {
			relayCallCount++
			return nil
		},
		activity.RegisterOptions{Name: "RelayOutboxEvents"},
	)

	env.ExecuteWorkflow(ProcessOutboxWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError()) // stopped by sentinel
	require.Equal(t, 2, fetchCallCount)      // first empty, second errStop
	require.Equal(t, 0, filterCallCount)     // never called on empty batch
	require.Equal(t, 0, relayCallCount)
}

func TestProcessOutboxWorkflow_HasMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	events := []*relay.Event{
		{OutboxID: 1, OrganizationID: "org1", SvixAppID: "app1", WebhooksEnabled: true},
	}

	fetchCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ relay.FetchEventArgs) (relay.FetchEventsResult, error) {
			fetchCallCount++
			switch fetchCallCount {
			case 1:
				// First iteration: HasMore=true → workflow must NOT sleep before next poll.
				return relay.FetchEventsResult{Events: events, HasMore: true}, nil
			case 2:
				return relay.FetchEventsResult{Events: events, HasMore: false}, nil
			default:
				return relay.FetchEventsResult{}, errStop
			}
		},
		activity.RegisterOptions{Name: "FetchPendingOutboxEvents"},
	)

	filterCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, evts []*relay.Event) ([]*relay.Event, error) {
			filterCallCount++
			return evts, nil
		},
		activity.RegisterOptions{Name: "FilterNoopOutboxEvents"},
	)

	relayCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []*relay.Event) error {
			relayCallCount++
			return nil
		},
		activity.RegisterOptions{Name: "RelayOutboxEvents"},
	)

	env.ExecuteWorkflow(ProcessOutboxWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError()) // stopped by sentinel
	require.Equal(t, 3, fetchCallCount)      // iteration 1 (HasMore), iteration 2 (no more), iteration 3 (errStop)
	require.Equal(t, 2, filterCallCount)     // called for each iteration that returned events
	require.Equal(t, 2, relayCallCount)      // called for each iteration that returned events
}

func TestProcessOutboxWorkflow_FetchError(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ relay.FetchEventArgs) (relay.FetchEventsResult, error) {
			return relay.FetchEventsResult{}, errStop
		},
		activity.RegisterOptions{Name: "FetchPendingOutboxEvents"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []*relay.Event) ([]*relay.Event, error) {
			t.Fatal("FilterNoopOutboxEvents must not be called when FetchPendingOutboxEvents fails")
			return nil, nil
		},
		activity.RegisterOptions{Name: "FilterNoopOutboxEvents"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []*relay.Event) error {
			t.Fatal("RelayOutboxEvents must not be called when FetchPendingOutboxEvents fails")
			return nil
		},
		activity.RegisterOptions{Name: "RelayOutboxEvents"},
	)

	env.ExecuteWorkflow(ProcessOutboxWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

func TestProcessOutboxWorkflow_FilterError(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	events := []*relay.Event{
		{OutboxID: 1, OrganizationID: "org1", SvixAppID: "app1", WebhooksEnabled: true},
	}

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ relay.FetchEventArgs) (relay.FetchEventsResult, error) {
			return relay.FetchEventsResult{Events: events, HasMore: false}, nil
		},
		activity.RegisterOptions{Name: "FetchPendingOutboxEvents"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []*relay.Event) ([]*relay.Event, error) {
			return nil, errStop
		},
		activity.RegisterOptions{Name: "FilterNoopOutboxEvents"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []*relay.Event) error {
			t.Fatal("RelayOutboxEvents must not be called when FilterNoopOutboxEvents fails")
			return nil
		},
		activity.RegisterOptions{Name: "RelayOutboxEvents"},
	)

	env.ExecuteWorkflow(ProcessOutboxWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

func TestProcessOutboxWorkflow_RelayError(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	events := []*relay.Event{
		{OutboxID: 1, OrganizationID: "org1", SvixAppID: "app1", WebhooksEnabled: true},
	}

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ relay.FetchEventArgs) (relay.FetchEventsResult, error) {
			return relay.FetchEventsResult{Events: events, HasMore: false}, nil
		},
		activity.RegisterOptions{Name: "FetchPendingOutboxEvents"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, evts []*relay.Event) ([]*relay.Event, error) {
			return evts, nil
		},
		activity.RegisterOptions{Name: "FilterNoopOutboxEvents"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []*relay.Event) error {
			return errStop
		},
		activity.RegisterOptions{Name: "RelayOutboxEvents"},
	)

	env.ExecuteWorkflow(ProcessOutboxWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

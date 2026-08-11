package background

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func executePromoteStagedTelemetryWorkflow(t *testing.T, passResults func(call int) (*activities.PromoteStagedTelemetryResult, error)) (int, error) {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	callCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.PromoteStagedTelemetryArgs) (*activities.PromoteStagedTelemetryResult, error) {
			callCount++
			return passResults(callCount)
		},
		activity.RegisterOptions{Name: "PromoteStagedTelemetry"},
	)

	env.ExecuteWorkflow(PromoteStagedTelemetryWorkflow, PromoteStagedTelemetryParams{ProjectID: uuid.New()})

	require.True(t, env.IsWorkflowCompleted())
	if err := env.GetWorkflowError(); err != nil {
		return callCount, fmt.Errorf("workflow error: %w", err)
	}
	return callCount, nil
}

func TestPromoteStagedTelemetryWorkflow_StopsWhenNothingPromoted(t *testing.T) {
	t.Parallel()

	// First pass drains a page, second finds only rows still awaiting
	// tuples: the drain loop must yield to the next sweep tick, not spin.
	calls, err := executePromoteStagedTelemetryWorkflow(t, func(call int) (*activities.PromoteStagedTelemetryResult, error) {
		if call == 1 {
			return &activities.PromoteStagedTelemetryResult{Promoted: 5, Rewritten: 5, Remaining: 0, Deduped: 0}, nil
		}
		return &activities.PromoteStagedTelemetryResult{Promoted: 0, Rewritten: 0, Remaining: 3, Deduped: 0}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestPromoteStagedTelemetryWorkflow_EmptyStagingRunsOnePass(t *testing.T) {
	t.Parallel()

	calls, err := executePromoteStagedTelemetryWorkflow(t, func(int) (*activities.PromoteStagedTelemetryResult, error) {
		return &activities.PromoteStagedTelemetryResult{Promoted: 0, Rewritten: 0, Remaining: 0, Deduped: 0}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestPromoteStagedTelemetryWorkflow_DedupOnlyPageContinuesDraining(t *testing.T) {
	t.Parallel()

	// First page only finishes deletes for rows an earlier crashed pass
	// already inserted (Promoted == 0, Deduped > 0). That is progress: the
	// next page may be promotable, so the loop must continue, then stop on
	// the pass that made no progress at all.
	calls, err := executePromoteStagedTelemetryWorkflow(t, func(call int) (*activities.PromoteStagedTelemetryResult, error) {
		switch call {
		case 1:
			return &activities.PromoteStagedTelemetryResult{Promoted: 0, Rewritten: 0, Remaining: 0, Deduped: 1000}, nil
		case 2:
			return &activities.PromoteStagedTelemetryResult{Promoted: 400, Rewritten: 400, Remaining: 0, Deduped: 0}, nil
		default:
			return &activities.PromoteStagedTelemetryResult{Promoted: 0, Rewritten: 0, Remaining: 0, Deduped: 0}, nil
		}
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls)
}

func TestPromoteStagedTelemetryWorkflow_PassBudgetBoundsBacklogDrain(t *testing.T) {
	t.Parallel()

	// A backlog that keeps promoting full pages drains multiple pages per
	// run but stops at the pass budget; the next sweep tick resumes.
	calls, err := executePromoteStagedTelemetryWorkflow(t, func(int) (*activities.PromoteStagedTelemetryResult, error) {
		return &activities.PromoteStagedTelemetryResult{Promoted: 1000, Rewritten: 900, Remaining: 0, Deduped: 0}, nil
	})
	require.NoError(t, err)
	require.Equal(t, promoteStagedTelemetryMaxPasses, calls)
}

func TestPromoteStagedTelemetryWorkflow_ActivityErrorFailsRun(t *testing.T) {
	t.Parallel()

	calls, err := executePromoteStagedTelemetryWorkflow(t, func(int) (*activities.PromoteStagedTelemetryResult, error) {
		return nil, errors.New("clickhouse unavailable")
	})
	require.Error(t, err)
	require.GreaterOrEqual(t, calls, 1)
}

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

// recheckSweepRun records what one sweep execution asked of its activities.
type recheckSweepRun struct {
	// cursors are the AfterID values the workflow paged with, in order.
	cursors []uuid.UUID

	// rechecked lists every target the workflow handed to the recheck
	// activity, in order.
	rechecked []uuid.UUID
}

// executeRecheckSweep runs the sweep against scripted activities. pages
// returns one page per call; recheckErr decides whether a given target's
// recheck fails.
func executeRecheckSweep(
	t *testing.T,
	pages func(call int, after uuid.UUID) []activities.McpApprovalRecheckTarget,
	recheckErr func(target activities.McpApprovalRecheckTarget) error,
) (*recheckSweepRun, error) {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	run := &recheckSweepRun{cursors: nil, rechecked: nil}
	listCalls := 0

	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.McpApprovalRecheckPageArgs) ([]activities.McpApprovalRecheckTarget, error) {
			listCalls++
			run.cursors = append(run.cursors, args.AfterID)
			return pages(listCalls, args.AfterID), nil
		},
		activity.RegisterOptions{Name: "ListMcpApprovalRecheckPage"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, target activities.McpApprovalRecheckTarget) error {
			run.rechecked = append(run.rechecked, target.ID)
			return recheckErr(target)
		},
		activity.RegisterOptions{Name: "RecheckMcpApprovalRequest"},
	)

	env.ExecuteWorkflow(McpApprovalRecheckWorkflow, McpApprovalRecheckParams{AfterID: uuid.Nil})

	require.True(t, env.IsWorkflowCompleted())
	if err := env.GetWorkflowError(); err != nil {
		return run, fmt.Errorf("workflow error: %w", err)
	}
	return run, nil
}

func recheckTargets(n int) []activities.McpApprovalRecheckTarget {
	targets := make([]activities.McpApprovalRecheckTarget, 0, n)
	for range n {
		targets = append(targets, activities.McpApprovalRecheckTarget{
			ID:             uuid.New(),
			ProjectID:      uuid.New(),
			OrganizationID: "org-test",
		})
	}
	return targets
}

// A short page means the scan is exhausted, so the sweep stops rather than
// paging forever.
func TestMcpApprovalRecheckWorkflow_StopsOnAShortPage(t *testing.T) {
	t.Parallel()

	page := recheckTargets(3)
	run, err := executeRecheckSweep(t,
		func(int, uuid.UUID) []activities.McpApprovalRecheckTarget { return page },
		func(activities.McpApprovalRecheckTarget) error { return nil },
	)
	require.NoError(t, err)
	require.Len(t, run.cursors, 1, "a page shorter than the page size ends the sweep")
	require.Len(t, run.rechecked, 3)
}

// A full page advances the keyset cursor to its last id, so the next page
// resumes after it — no target is skipped and none is rechecked twice.
func TestMcpApprovalRecheckWorkflow_AdvancesTheKeysetCursor(t *testing.T) {
	t.Parallel()

	full := recheckTargets(int(mcpApprovalRecheckPageSize))
	tail := recheckTargets(2)

	run, err := executeRecheckSweep(t,
		func(call int, _ uuid.UUID) []activities.McpApprovalRecheckTarget {
			if call == 1 {
				return full
			}
			return tail
		},
		func(activities.McpApprovalRecheckTarget) error { return nil },
	)
	require.NoError(t, err)

	require.Equal(t, []uuid.UUID{uuid.Nil, full[len(full)-1].ID}, run.cursors)
	require.Len(t, run.rechecked, len(full)+len(tail))
	require.Equal(t, full[0].ID, run.rechecked[0])
	require.Equal(t, tail[len(tail)-1].ID, run.rechecked[len(run.rechecked)-1])
}

// One unreachable server must not end the sweep for every tenant behind it:
// its recheck runs again tomorrow, and the rest of the page still runs.
func TestMcpApprovalRecheckWorkflow_ContinuesPastAFailedTarget(t *testing.T) {
	t.Parallel()

	page := recheckTargets(3)
	run, err := executeRecheckSweep(t,
		func(int, uuid.UUID) []activities.McpApprovalRecheckTarget { return page },
		func(target activities.McpApprovalRecheckTarget) error {
			if target.ID == page[0].ID {
				return errors.New("the server refused every probe")
			}
			return nil
		},
	)
	require.NoError(t, err, "a per-target failure is logged, not fatal")
	for _, target := range page {
		require.Contains(t, run.rechecked, target.ID, "every target on the page is still attempted")
	}
	// The failing target burns its one retry before the sweep gives up on it.
	require.Len(t, run.rechecked, len(page)+1)
}

// A failing scan is fatal: continuing would silently recheck nothing while
// reporting success.
func TestMcpApprovalRecheckWorkflow_FailsWhenTheScanFails(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.McpApprovalRecheckPageArgs) ([]activities.McpApprovalRecheckTarget, error) {
			return nil, errors.New("database unreachable")
		},
		activity.RegisterOptions{Name: "ListMcpApprovalRecheckPage"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.McpApprovalRecheckTarget) error {
			t.Error("no target should be rechecked when the scan fails")
			return nil
		},
		activity.RegisterOptions{Name: "RecheckMcpApprovalRequest"},
	)

	env.ExecuteWorkflow(McpApprovalRecheckWorkflow, McpApprovalRecheckParams{AfterID: uuid.Nil})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

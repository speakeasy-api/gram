package background

import (
	"context"
	"errors"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/mcpevents"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"time"
)

const (
	// eventCleanupAttemptTimeout bounds the work of one retention sweep.
	eventCleanupAttemptTimeout = 10 * time.Minute

	// eventCleanupAttempts accommodates transient database failures.
	eventCleanupAttempts = 3

	// eventCleanupRunTimeout includes three attempts, backoff, and scheduling headroom.
	eventCleanupRunTimeout = 40 * time.Minute
)

// MCPEventsCleanupWorkflow reclaims expired subscriptions and delivery history.
// One hourly workflow and one activity cost about 1,440 actions/month per queue,
// per namespace; three activity attempts raise this to at most 2,880 actions.
// Event production and delivery use Pub/Sub and add no Temporal actions.
func MCPEventsCleanupWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: eventCleanupAttemptTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: eventCleanupAttempts, InitialInterval: time.Minute, BackoffCoefficient: 2},
	})
	var a *Activities
	return workflow.ExecuteActivity(ctx, a.CleanupMCPEvents).Get(ctx, nil)
}

// CleanupMCPEvents bounds each transaction and the total work per attempt.
func (a *Activities) CleanupMCPEvents(ctx context.Context) error {
	if err := mcpevents.Cleanup(ctx, a.db, time.Now()); err != nil {
		return fmt.Errorf("cleanup MCP events: %w", err)
	}
	return nil
}

// AddMCPEventsCleanupSchedule installs one hourly sweep for this task queue.
func AddMCPEventsCleanupSchedule(ctx context.Context, env *tenv.Environment) error {
	id := fmt.Sprintf("v1:mcp-events-cleanup:%s", env.Queue())
	_, err := createScheduleWithCatchup(ctx, env.Client().ScheduleClient(), client.ScheduleOptions{
		ID: id, CatchupWindow: time.Hour - time.Second, Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: time.Hour}}},
		Action: &client.ScheduleWorkflowAction{
			ID: id + "/scheduled", Workflow: MCPEventsCleanupWorkflow, TaskQueue: string(env.Queue()),
			WorkflowRunTimeout: eventCleanupRunTimeout,
		},
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create MCP events cleanup schedule: %w", err)
	}
	return nil
}

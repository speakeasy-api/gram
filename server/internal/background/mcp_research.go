package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// ExecuteMcpResearchWorkflow starts one research-agent run for an MCP
// approval request. The report row already exists in status running; the
// workflow's job is to fill it in. The workflow id is keyed by report, and
// USE_EXISTING makes a duplicate start attach to the in-flight run instead
// of racing it.
func ExecuteMcpResearchWorkflow(ctx context.Context, temporalEnv *tenv.Environment, input activities.McpResearchInput) (client.WorkflowRun, error) {
	run, err := temporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       fmt.Sprintf("v1:mcp-research/%s", input.ReportID),
		TaskQueue:                string(temporalEnv.Queue()),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		// Must exceed the activity's ScheduleToClose so the workflow never
		// expires under a run that is still inside its budget.
		WorkflowRunTimeout: 40 * time.Minute,
	}, McpResearchWorkflow, input)
	if err != nil {
		return nil, fmt.Errorf("execute mcp research workflow: %w", err)
	}

	return run, nil
}

func McpResearchWorkflow(ctx workflow.Context, input activities.McpResearchInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// A research run is a long agent loop: many completions plus web
		// fetches. The heartbeat catches a dead worker long before the
		// deadline does.
		StartToCloseTimeout: 30 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			// One attempt: an agent run is expensive, and the activity
			// records failure on the report row itself — the admin re-runs
			// from the UI deliberately rather than the platform re-spending
			// blindly.
			MaximumAttempts:    1,
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    1 * time.Minute,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.RunMcpResearch, input).Get(ctx, nil); err != nil {
		// The run activity marks its own report row failed when it errors —
		// but a crashed or heartbeat-timed-out worker never gets to. This
		// compensation is what keeps a report from stranding in running,
		// which would pin the page's polling and its disabled Run button
		// forever. Marking a row the activity already failed is a no-op: the
		// update only touches rows still in running.
		markCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts:    5,
				InitialInterval:    5 * time.Second,
				BackoffCoefficient: 2,
				MaximumInterval:    1 * time.Minute,
			},
		})
		if markErr := workflow.ExecuteActivity(markCtx, a.MarkMcpResearchInterrupted, input).Get(markCtx, nil); markErr != nil {
			workflow.GetLogger(ctx).Error("mark interrupted mcp research report failed", "error", markErr)
		}

		return fmt.Errorf("run mcp research: %w", err)
	}

	return nil
}

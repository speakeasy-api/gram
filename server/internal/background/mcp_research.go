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
// The run timeout and the activity budgets below are one invariant, not four
// numbers: the workflow must outlive a full-length run *plus* the
// compensation that resolves the report row, or a timeout kills the
// compensation too and leaves the row in running forever. Asserted in
// TestMcpResearchWorkflow_RunTimeoutOutlivesItsActivities rather than left to
// whoever next raises one of them.
const (
	// What one agent run gets once a worker starts it.
	mcpResearchRunActivityTimeout = 30 * time.Minute

	// Queue time plus run time. Bounded so the activity fails inside the
	// workflow rather than the workflow expiring around it.
	mcpResearchScheduleToCloseTimeout = 35 * time.Minute

	// One attempt at marking the report interrupted.
	mcpResearchCompensationAttemptTimeout = 30 * time.Second

	mcpResearchRunTimeout = 40 * time.Minute
)

// mcpResearchCompensationRetryPolicy retries the report-resolving
// compensation, whose whole job is to run after something else already went
// wrong.
func mcpResearchCompensationRetryPolicy() *temporal.RetryPolicy {
	return &temporal.RetryPolicy{
		MaximumAttempts:        5,
		InitialInterval:        5 * time.Second,
		BackoffCoefficient:     2,
		MaximumInterval:        1 * time.Minute,
		NonRetryableErrorTypes: nil,
	}
}

// mcpResearchCompensationBudget is the worst case wall-clock the compensation
// can take: every attempt running to its timeout, plus every backoff interval
// between them.
func mcpResearchCompensationBudget() time.Duration {
	policy := mcpResearchCompensationRetryPolicy()

	budget := time.Duration(policy.MaximumAttempts) * mcpResearchCompensationAttemptTimeout
	interval := policy.InitialInterval
	for range policy.MaximumAttempts - 1 {
		budget += interval
		interval = min(
			time.Duration(float64(interval)*policy.BackoffCoefficient),
			policy.MaximumInterval,
		)
	}

	return budget
}

// USE_EXISTING makes a duplicate start attach to the in-flight run instead
// of racing it.
func ExecuteMcpResearchWorkflow(ctx context.Context, temporalEnv *tenv.Environment, input activities.McpResearchInput) (client.WorkflowRun, error) {
	run, err := temporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       fmt.Sprintf("v1:mcp-research/%s", input.ReportID),
		TaskQueue:                string(temporalEnv.Queue()),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowRunTimeout:       mcpResearchRunTimeout,
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
		StartToCloseTimeout: mcpResearchRunActivityTimeout,
		// Queue time counts against the workflow's run timeout but not
		// against StartToClose, which only starts when a worker picks the
		// activity up. Without this, a saturated queue expires the workflow
		// while the activity is still waiting — and the run timeout takes
		// the compensation below with it, stranding the report in running
		// with the page polling a run that will never resolve.
		ScheduleToCloseTimeout: mcpResearchScheduleToCloseTimeout,
		HeartbeatTimeout:       2 * time.Minute,
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
			StartToCloseTimeout: mcpResearchCompensationAttemptTimeout,
			RetryPolicy:         mcpResearchCompensationRetryPolicy(),
		})
		if markErr := workflow.ExecuteActivity(markCtx, a.MarkMcpResearchInterrupted, input).Get(markCtx, nil); markErr != nil {
			workflow.GetLogger(ctx).Error("mark interrupted mcp research report failed", "error", markErr)
		}

		return fmt.Errorf("run mcp research: %w", err)
	}

	return nil
}

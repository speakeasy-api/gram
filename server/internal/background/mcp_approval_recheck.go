package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	mcpApprovalRecheckScheduleID       = "v1:mcp-approval-recheck-schedule"
	mcpApprovalRecheckWorkflowID       = mcpApprovalRecheckScheduleID + "/scheduled"
	mcpApprovalRecheckInterval         = 24 * time.Hour
	mcpApprovalRecheckPageSize   int32 = 50

	// mcpApprovalRecheckRunTimeout bounds one run of the sweep.
	mcpApprovalRecheckRunTimeout = 4 * time.Hour

	// mcpApprovalRecheckBudget is how long a run keeps starting new pages.
	// A page of 50 targets that all time out and retry can occupy most of a
	// run on its own, so a large enough scan set would otherwise let the run
	// timeout kill the workflow mid-page — and the next scheduled run starts
	// at the beginning of the id space, re-checking the same prefix forever
	// while the tail is never reached. Continuing as new before the ceiling
	// carries the cursor instead.
	mcpApprovalRecheckBudget = mcpApprovalRecheckRunTimeout - 45*time.Minute
)

// McpApprovalRecheckParams carries the sweep's keyset cursor across
// continue-as-new boundaries.
type McpApprovalRecheckParams struct {
	AfterID uuid.UUID
}

// McpApprovalRecheckWorkflow re-gathers evidence for every approved MCP
// approval request and flags permission-relevant drift from the snapshot the
// approval rested on. Rechecks run sequentially: each one fans out to the
// same third-party sources (registries, OSV, OAuth discovery), so a
// deliberately slow sweep is kinder to them than a parallel burst — and a
// daily cadence leaves it hours of headroom.
func McpApprovalRecheckWorkflow(ctx workflow.Context, params McpApprovalRecheckParams) error {
	listCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	// A recheck's gather budget mirrors the intake path's: every source has
	// its own ~3s deadline inside the assembler, so two minutes is a
	// generous ceiling. Two attempts, not more — a source that failed twice
	// lands in the document's gaps, where the comparison already treats it
	// as not-consulted rather than as changed.
	recheckCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		HeartbeatTimeout:    time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    2,
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var a *Activities
	logger := workflow.GetLogger(ctx)
	afterID := params.AfterID
	deadline := workflow.Now(ctx).Add(mcpApprovalRecheckBudget)

	for {
		// Checked before each page rather than each target: a page is the
		// unit the cursor can resume from, so stopping mid-page would repeat
		// its targets anyway.
		if workflow.Now(ctx).After(deadline) {
			logger.Info("mcp approval recheck sweep continuing as new", "after_id", afterID.String())
			return workflow.NewContinueAsNewError(ctx, McpApprovalRecheckWorkflow, McpApprovalRecheckParams{AfterID: afterID})
		}

		var targets []activities.McpApprovalRecheckTarget
		if err := workflow.ExecuteActivity(listCtx, a.ListMcpApprovalRecheckPage, activities.McpApprovalRecheckPageArgs{
			AfterID:  afterID,
			PageSize: mcpApprovalRecheckPageSize,
		}).Get(listCtx, &targets); err != nil {
			return fmt.Errorf("list approved requests for recheck: %w", err)
		}

		for _, target := range targets {
			// One unreachable server must not end the sweep for every tenant
			// behind it; its recheck simply runs again tomorrow.
			if err := workflow.ExecuteActivity(recheckCtx, a.RecheckMcpApprovalRequest, target).Get(recheckCtx, nil); err != nil {
				logger.Warn("mcp approval recheck failed", "request_id", target.ID.String(), "error", err.Error())
			}
		}

		if len(targets) < int(mcpApprovalRecheckPageSize) {
			return nil
		}
		afterID = targets[len(targets)-1].ID
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, McpApprovalRecheckWorkflow, McpApprovalRecheckParams{AfterID: afterID})
		}
	}
}

// AddMcpApprovalRecheckSchedule registers the daily change-detection sweep.
func AddMcpApprovalRecheckSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: mcpApprovalRecheckInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 mcpApprovalRecheckWorkflowID,
		Workflow:           McpApprovalRecheckWorkflow,
		Args:               []any{McpApprovalRecheckParams{AfterID: uuid.Nil}},
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: mcpApprovalRecheckRunTimeout,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      mcpApprovalRecheckScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := sc.GetHandle(ctx, mcpApprovalRecheckScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing mcp approval recheck schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create mcp approval recheck schedule: %w", err)
	}

	return nil
}

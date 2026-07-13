package background

// Staged-telemetry promotion. Claude api_request rows with redacted (custom)
// MCP attribution park in telemetry_logs_staging (see the fork in
// server/internal/hooks/otel.go) until the transcript-derived attribution
// arrives via the Stop/SubagentStop hooks — or the 30-minute timeout passes —
// then a promotion pass rewrites the names and moves them into
// telemetry_logs. The sweep schedule below is the sole trigger: every two
// minutes it lists the projects with staged rows and fans out one promotion
// pass per project. Passes are serialized per project by the promotion
// workflow ID (the activity's read-check-insert dedup guard assumes a single
// writer), and the schedule's overlap policy keeps sweeps from stacking.

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
	// Comfortably above the activity's worst-case retry window (~195s with
	// three 60s attempts and backoff) so a slow final attempt is not cut off
	// by the workflow deadline.
	promoteStagedTelemetryWorkflowRunTimeout = 4 * time.Minute

	stagedTelemetrySweepScheduleID = "v1:staged-telemetry-sweep-schedule"
	stagedTelemetrySweepWorkflowID = stagedTelemetrySweepScheduleID + "/scheduled"
	stagedTelemetrySweepInterval   = 2 * time.Minute
)

type PromoteStagedTelemetryParams struct {
	ProjectID uuid.UUID
}

func promoteStagedTelemetryWorkflowID(params PromoteStagedTelemetryParams) string {
	return fmt.Sprintf("v1:promote-staged-telemetry:%s", params.ProjectID.String())
}

func PromoteStagedTelemetryWorkflow(ctx workflow.Context, params PromoteStagedTelemetryParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
		},
	})

	var a *Activities
	var result activities.PromoteStagedTelemetryResult
	if err := workflow.ExecuteActivity(
		ctx,
		a.PromoteStagedTelemetry,
		activities.PromoteStagedTelemetryArgs(params),
	).Get(ctx, &result); err != nil {
		return fmt.Errorf("promote staged telemetry: %w", err)
	}
	return nil
}

// StagedTelemetrySweepWorkflow runs one promotion pass per project with rows
// still in staging (projects with the oldest rows first, capped per sweep —
// see stagedTelemetryProjectsLimit — so one sweep's work stays bounded).
// Promotion runs in child workflows keyed per project, so two sweeps can
// never race a pass for the same project even if a pass outlives its sweep.
func StagedTelemetrySweepWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
		},
	})

	var a *Activities
	var projects []activities.PromoteStagedTelemetryArgs
	if err := workflow.ExecuteActivity(ctx, a.ListStagedTelemetryProjects).Get(ctx, &projects); err != nil {
		return fmt.Errorf("list staged telemetry projects: %w", err)
	}

	logger := workflow.GetLogger(ctx)
	for _, project := range projects {
		params := PromoteStagedTelemetryParams(project)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:            promoteStagedTelemetryWorkflowID(params),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowRunTimeout:    promoteStagedTelemetryWorkflowRunTimeout,
			ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
		})
		// One project's failure (or a pass still running from the previous
		// sweep) must not stop this sweep from reaching the others.
		if err := workflow.ExecuteChildWorkflow(childCtx, PromoteStagedTelemetryWorkflow, params).
			GetChildWorkflowExecution().Get(childCtx, nil); err != nil {
			logger.Info("staged telemetry promotion already in flight or failed to start",
				"project_id", params.ProjectID.String(), "error", err.Error())
		}
	}
	return nil
}

func AddStagedTelemetrySweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: stagedTelemetrySweepInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 stagedTelemetrySweepWorkflowID,
		Workflow:           StagedTelemetrySweepWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: 5 * time.Minute,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      stagedTelemetrySweepScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		// The schedule survives deploys; push the current spec and action so
		// interval or workflow changes take effect on running environments.
		if err := sc.GetHandle(ctx, stagedTelemetrySweepScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update staged telemetry sweep schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create staged telemetry sweep schedule: %w", err)
	}
	return nil
}

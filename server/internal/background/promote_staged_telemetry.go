package background

// Staged-telemetry promotion. Claude api_request rows with redacted (custom)
// MCP attribution park in telemetry_logs_staging (see the fork in
// server/internal/hooks/otel.go) until the transcript-derived attribution
// arrives via the Stop/SubagentStop hooks — or the 30-minute timeout passes —
// then a promotion pass rewrites the names and moves them into
// telemetry_logs. Three things trigger a pass, all funnelled through the same
// per-session workflow ID so passes are serialized per session (the activity's
// read-check-insert dedup guard assumes a single writer):
//
//   - the OTEL ingest path, right after staging rows (tuples may already be
//     waiting: Stop can beat the final OTEL export batch);
//   - the hooks ingest path, right after storing new attribution tuples;
//   - the sweep schedule below, which re-triggers every staged session to
//     promote late arrivals and enforce the timeout.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	promoteStagedTelemetryWorkflowRunTimeout = 2 * time.Minute

	stagedTelemetrySweepScheduleID = "v1:staged-telemetry-sweep-schedule"
	stagedTelemetrySweepWorkflowID = stagedTelemetrySweepScheduleID + "/scheduled"
	stagedTelemetrySweepInterval   = 5 * time.Minute
)

type PromoteStagedTelemetryParams struct {
	ProjectID uuid.UUID
	SessionID string
}

func promoteStagedTelemetryWorkflowID(params PromoteStagedTelemetryParams) string {
	return fmt.Sprintf("v1:promote-staged-telemetry:%s:%s", params.ProjectID.String(), params.SessionID)
}

// ExecutePromoteStagedTelemetryWorkflow starts a promotion pass for one
// session. A pass already in flight for the session is fine — the workflow ID
// serializes writers, and a later trigger or the sweep picks up anything the
// in-flight pass misses — so the already-started error is swallowed.
func ExecutePromoteStagedTelemetryWorkflow(ctx context.Context, env *tenv.Environment, params PromoteStagedTelemetryParams) (client.WorkflowRun, error) {
	run, err := env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    promoteStagedTelemetryWorkflowID(params),
		TaskQueue:             string(env.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    promoteStagedTelemetryWorkflowRunTimeout,
	}, PromoteStagedTelemetryWorkflow, params)
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &alreadyStarted) {
		return run, nil
	}
	return run, err
}

func PromoteStagedTelemetryWorkflow(ctx workflow.Context, params PromoteStagedTelemetryParams) error {
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

// StagedTelemetrySweepWorkflow re-triggers promotion for every session with
// rows still in staging. It is the safety net for both race directions
// (staged row after the last hook, tuples after the last OTEL batch) and the
// enforcer of the verbatim-promotion timeout. Promotion itself runs in child
// workflows carrying the same per-session IDs as the hook/OTEL triggers, so
// the sweep can never race a concurrent pass for the same session.
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
	var sessions []activities.PromoteStagedTelemetryArgs
	if err := workflow.ExecuteActivity(ctx, a.ListStagedTelemetrySessions).Get(ctx, &sessions); err != nil {
		return fmt.Errorf("list staged telemetry sessions: %w", err)
	}

	logger := workflow.GetLogger(ctx)
	for _, session := range sessions {
		params := PromoteStagedTelemetryParams(session)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:            promoteStagedTelemetryWorkflowID(params),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowRunTimeout:    promoteStagedTelemetryWorkflowRunTimeout,
			ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
		})
		// One session's failure (or an already-running pass started by a hook
		// trigger) must not stop the sweep from reaching the others.
		if err := workflow.ExecuteChildWorkflow(childCtx, PromoteStagedTelemetryWorkflow, params).
			GetChildWorkflowExecution().Get(childCtx, nil); err != nil {
			logger.Info("staged telemetry promotion already in flight or failed to start",
				"session_id", session.SessionID, "error", err.Error())
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
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create staged telemetry sweep schedule: %w", err)
	}
	return nil
}

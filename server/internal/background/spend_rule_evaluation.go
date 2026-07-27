package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	spend_rules "github.com/speakeasy-api/gram/server/internal/background/activities/spend_rules"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	spendRuleEvaluationWorkflowID = "v1:spend-rule-evaluation"
	spendRuleEvaluationScheduleID = "v1:spend-rule-evaluation-schedule"

	// spendRuleEvaluationRunTimeout budgets one bounded page of the scheduled
	// sweep. Larger sweeps ContinueAsNew with the remaining orgs.
	spendRuleEvaluationRunTimeout = 15 * time.Minute

	// spendRuleEvaluationActivityTimeout budgets one activity: listing orgs
	// or evaluating a single org (directory match + one ClickHouse query per
	// rule).
	spendRuleEvaluationActivityTimeout = 30 * time.Second

	// spendRuleEvaluationOrgPageSize bounds each workflow run. With a 30s
	// activity attempt and 3 attempts, five unhealthy orgs still fit under the
	// run timeout with retry backoff.
	spendRuleEvaluationOrgPageSize = 5
)

type SpendRuleEvaluationParams struct {
	OrganizationIDs []string `json:"organization_ids"`
	EvaluatedCount  int      `json:"evaluated_count"`
	ErrorCount      int      `json:"error_count"`
}

// SpendRuleEvaluationWorkflow is the scheduled coordinator: it lists every
// organization with enabled spend rules and evaluates a bounded page per run.
// Larger sweeps ContinueAsNew with the remaining orgs so slow/failing orgs do
// not prevent later orgs from being evaluated. Runs every
// spendrules.EvaluationInterval with overlap-skip, so a slow sweep delays
// rather than stacks.
func SpendRuleEvaluationWorkflow(ctx workflow.Context, params SpendRuleEvaluationParams) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: spendRuleEvaluationActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities

	orgs := params.OrganizationIDs
	if len(orgs) == 0 && params.EvaluatedCount == 0 {
		if err := workflow.ExecuteActivity(activityCtx, a.ListSpendRuleOrgs).Get(activityCtx, &orgs); err != nil {
			return fmt.Errorf("list spend rule orgs: %w", err)
		}
	}

	pageEnd := min(len(orgs), spendRuleEvaluationOrgPageSize)
	errCount := params.ErrorCount
	evaluatedCount := params.EvaluatedCount
	for _, org := range orgs[:pageEnd] {
		if err := workflow.ExecuteActivity(activityCtx, a.EvaluateOrgSpendRules, spend_rules.EvaluateOrgArgs{
			OrganizationID: org,
		}).Get(activityCtx, nil); err != nil {
			// Evaluate every org in this page even when one fails; surface the
			// failure after the sweep finishes so later pages still run.
			workflow.GetLogger(ctx).Error("evaluate org spend rules", "organization_id", org, "error", err)
			errCount++
		}
		evaluatedCount++
	}

	if pageEnd < len(orgs) {
		return workflow.NewContinueAsNewError(ctx, SpendRuleEvaluationWorkflow, SpendRuleEvaluationParams{
			OrganizationIDs: orgs[pageEnd:],
			EvaluatedCount:  evaluatedCount,
			ErrorCount:      errCount,
		})
	}
	if errCount > 0 {
		return fmt.Errorf("spend rule evaluation failed for %d of %d orgs", errCount, evaluatedCount)
	}
	return nil
}

// SpendRuleOrgEvaluationWorkflow evaluates a single organization's spend
// rules immediately. Runs as the body of the debounced wrapper below so
// circuits open and close without waiting for the next scheduled sweep. It
// must not issue its own ContinueAsNew — continuation is owned by Debounce.
func SpendRuleOrgEvaluationWorkflow(ctx workflow.Context, organizationID string) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: spendRuleEvaluationActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(activityCtx, a.EvaluateOrgSpendRules, spend_rules.EvaluateOrgArgs{
		OrganizationID: organizationID,
	}).Get(activityCtx, nil); err != nil {
		return fmt.Errorf("evaluate org spend rules: %w", err)
	}
	return nil
}

func buildSpendRuleOrgEvaluationWorkflowID(organizationID string) string {
	return "v1:spend-rule-eval:" + organizationID
}

// spendRuleOrgEvaluationDebounceSignal names the debounce signal channel.
// Signals are addressed to a per-org workflow ID, so a single constant name
// is unambiguous across organizations.
func spendRuleOrgEvaluationDebounceSignal(string) string {
	return "v1:spend-rule-eval/signal"
}

// SpendRuleOrgEvaluationResult carries no data; the Debounce wrapper requires
// a result type.
type SpendRuleOrgEvaluationResult struct{}

// SpendRuleOrgEvaluationWorkflowDebounced is the signal-with-start entry
// point for per-org evaluation. Signals that arrive while a run is in flight
// (fresh usage landing mid-evaluation) coalesce into exactly one follow-up
// run via ContinueAsNew instead of being dropped; the workflow completes when
// no work is pending.
func SpendRuleOrgEvaluationWorkflowDebounced(ctx workflow.Context, organizationID string) (SpendRuleOrgEvaluationResult, error) {
	return Debounce(
		func(ctx workflow.Context, organizationID string) (SpendRuleOrgEvaluationResult, error) {
			return SpendRuleOrgEvaluationResult{}, SpendRuleOrgEvaluationWorkflow(ctx, organizationID)
		},
		SpendRuleOrgEvaluationWorkflowDebounced,
		spendRuleOrgEvaluationDebounceSignal,
		func(string, SpendRuleOrgEvaluationResult) bool { return false },
	)(ctx, organizationID)
}

// SpendRuleActorEvaluationWorkflow refreshes one actor's spend cache. Signals
// are keyed by stable Gram user ID; email remains optional metadata the
// activity can use when loading ClickHouse spend.
func SpendRuleActorEvaluationWorkflow(ctx workflow.Context, signal spendrules.ActorEvaluationSignal) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: spendRuleEvaluationActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(activityCtx, a.RefreshSpendRuleActor, spend_rules.EvaluateActorArgs{
		OrganizationID: signal.OrganizationID,
		UserID:         signal.UserID,
		Email:          signal.Email,
	}).Get(activityCtx, nil); err != nil {
		return fmt.Errorf("refresh spend rule actor: %w", err)
	}
	return nil
}

func buildSpendRuleActorEvaluationWorkflowID(signal spendrules.ActorEvaluationSignal) string {
	return "v1:spend-rule-actor-eval:" + signal.OrganizationID + ":" + signal.UserID
}

func spendRuleActorEvaluationDebounceSignal(spendrules.ActorEvaluationSignal) string {
	return "v1:spend-rule-actor-eval/signal"
}

type SpendRuleActorEvaluationResult struct{}

func SpendRuleActorEvaluationWorkflowDebounced(ctx workflow.Context, signal spendrules.ActorEvaluationSignal) (SpendRuleActorEvaluationResult, error) {
	return Debounce(
		func(ctx workflow.Context, signal spendrules.ActorEvaluationSignal) (SpendRuleActorEvaluationResult, error) {
			return SpendRuleActorEvaluationResult{}, SpendRuleActorEvaluationWorkflow(ctx, signal)
		},
		SpendRuleActorEvaluationWorkflowDebounced,
		spendRuleActorEvaluationDebounceSignal,
		func(spendrules.ActorEvaluationSignal, SpendRuleActorEvaluationResult) bool { return false },
	)(ctx, signal)
}

// TemporalSpendRuleEvaluator implements spendrules.EvaluationSignaler by
// signal-with-starting the debounced per-org evaluation workflow. If a run is
// already in flight the signal enqueues exactly one follow-up run, so state
// committed mid-evaluation (a rule mutation or fresh usage) is always picked
// up rather than waiting for the next scheduled sweep.
type TemporalSpendRuleEvaluator struct {
	TemporalEnv *tenv.Environment
}

var _ spendrules.EvaluationSignaler = (*TemporalSpendRuleEvaluator)(nil)
var _ spendrules.ActorEvaluationSignaler = (*TemporalSpendRuleEvaluator)(nil)

func (e *TemporalSpendRuleEvaluator) Signal(ctx context.Context, organizationID string) error {
	id := buildSpendRuleOrgEvaluationWorkflowID(organizationID)
	_, err := e.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		id,
		spendRuleOrgEvaluationDebounceSignal(organizationID),
		"enqueue",
		client.StartWorkflowOptions{
			ID:                       id,
			TaskQueue:                string(e.TemporalEnv.Queue()),
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		},
		SpendRuleOrgEvaluationWorkflowDebounced,
		organizationID,
	)
	if err != nil {
		return fmt.Errorf("signal spend rule org evaluation: %w", err)
	}
	return nil
}

func (e *TemporalSpendRuleEvaluator) SignalActor(ctx context.Context, signal spendrules.ActorEvaluationSignal) error {
	if signal.UserID == "" {
		return nil
	}
	id := buildSpendRuleActorEvaluationWorkflowID(signal)
	_, err := e.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		id,
		spendRuleActorEvaluationDebounceSignal(signal),
		"enqueue",
		client.StartWorkflowOptions{
			ID:                       id,
			TaskQueue:                string(e.TemporalEnv.Queue()),
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		},
		SpendRuleActorEvaluationWorkflowDebounced,
		signal,
	)
	if err != nil {
		return fmt.Errorf("signal spend rule actor evaluation: %w", err)
	}
	return nil
}

func AddSpendRuleEvaluationSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := buildSpendRuleEvaluationScheduleOptions(temporalEnv)

	_, err := scheduleClient.Create(ctx, options)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create spend rule evaluation schedule: %w", err)
	}

	if err := scheduleClient.GetHandle(ctx, spendRuleEvaluationScheduleID).Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			schedule := input.Description.Schedule
			schedule.Spec = &options.Spec
			schedule.Action = options.Action
			if schedule.Policy == nil {
				schedule.Policy = &client.SchedulePolicies{
					Overlap:        enums.SCHEDULE_OVERLAP_POLICY_SKIP,
					CatchupWindow:  0,
					PauseOnFailure: false,
				}
			}
			return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
		},
	}); err != nil {
		return fmt.Errorf("update spend rule evaluation schedule: %w", err)
	}
	return nil
}

func buildSpendRuleEvaluationScheduleOptions(temporalEnv *tenv.Environment) client.ScheduleOptions {
	return client.ScheduleOptions{
		ID:      spendRuleEvaluationScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: spendrules.EvaluationInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 spendRuleEvaluationWorkflowID,
			Workflow:           SpendRuleEvaluationWorkflow,
			Args:               []any{SpendRuleEvaluationParams{OrganizationIDs: nil, EvaluatedCount: 0, ErrorCount: 0}},
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: spendRuleEvaluationRunTimeout,
		},
	}
}

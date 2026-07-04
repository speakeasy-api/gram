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

	// spendRuleEvaluationRunTimeout budgets one full scheduled sweep across
	// every org with enabled rules.
	spendRuleEvaluationRunTimeout = 15 * time.Minute

	// spendRuleEvaluationActivityTimeout budgets one activity: listing orgs
	// or evaluating a single org (directory match + one ClickHouse query per
	// rule).
	spendRuleEvaluationActivityTimeout = 2 * time.Minute
)

// SpendRuleEvaluationWorkflow is the scheduled coordinator: it lists every
// organization with enabled spend rules and evaluates each in turn. Runs
// every spendrules.EvaluationInterval with overlap-skip, so a slow sweep
// delays rather than stacks.
func SpendRuleEvaluationWorkflow(ctx workflow.Context) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: spendRuleEvaluationActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	var a *Activities

	var orgs []string
	if err := workflow.ExecuteActivity(activityCtx, a.ListSpendRuleOrgs).Get(activityCtx, &orgs); err != nil {
		return fmt.Errorf("list spend rule orgs: %w", err)
	}

	var errCount int
	for _, org := range orgs {
		if err := workflow.ExecuteActivity(activityCtx, a.EvaluateOrgSpendRules, spend_rules.EvaluateOrgArgs{
			OrganizationID: org,
		}).Get(activityCtx, nil); err != nil {
			// Evaluate every org even when one fails; surface the failure at
			// the end so the run is visibly unhealthy.
			workflow.GetLogger(ctx).Error("evaluate org spend rules", "organization_id", org, "error", err)
			errCount++
		}
	}

	if errCount > 0 {
		return fmt.Errorf("spend rule evaluation failed for %d of %d orgs", errCount, len(orgs))
	}
	return nil
}

// SpendRuleOrgEvaluationWorkflow evaluates a single organization's spend
// rules immediately. Started by rule mutations so circuits open and close
// without waiting for the next scheduled sweep.
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

// TemporalSpendRuleEvaluator implements spendrules.EvaluationSignaler by
// starting a one-shot per-org evaluation workflow. A concurrent in-flight
// evaluation for the same org is treated as success — the running one will
// pick up the freshly committed rule state or the next sweep will.
type TemporalSpendRuleEvaluator struct {
	TemporalEnv *tenv.Environment
}

var _ spendrules.EvaluationSignaler = (*TemporalSpendRuleEvaluator)(nil)

func (e *TemporalSpendRuleEvaluator) Signal(ctx context.Context, organizationID string) error {
	_, err := e.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    buildSpendRuleOrgEvaluationWorkflowID(organizationID),
		TaskQueue:             string(e.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, SpendRuleOrgEvaluationWorkflow, organizationID)
	if err != nil && !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
		return fmt.Errorf("start spend rule org evaluation: %w", err)
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
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: spendRuleEvaluationRunTimeout,
		},
	}
}

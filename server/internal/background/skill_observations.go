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
	skillObservationBatchSize    int32 = 100
	skillObservationProjectLimit int32 = 100
	skillObservationMaxBatches         = 10
	skillObservationMaxPages           = 10
	skillObservationScheduleID         = "v1:skill-observation-reconciliation-schedule"
	skillObservationWorkflowID         = skillObservationScheduleID + "/scheduled"
	skillObservationInterval           = time.Minute
	skillObservationRunTimeout         = 35 * time.Minute
)

type ReconcileSkillObservationsParams struct {
	ProjectID uuid.UUID `json:"project_id"`
}

type SkillObservationReconciliationSweepParams struct {
	AfterProjectID uuid.UUID `json:"after_project_id"`
}

func reconcileSkillObservationsWorkflowID(params ReconcileSkillObservationsParams) string {
	return fmt.Sprintf("v1:reconcile-skill-observations:%s", params.ProjectID.String())
}

func ReconcileSkillObservationsWorkflow(ctx workflow.Context, params ReconcileSkillObservationsParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	var a *Activities
	for range skillObservationMaxBatches {
		var result activities.ReconcileSkillObservationsResult
		if err := workflow.ExecuteActivity(ctx, a.ReconcileSkillObservations, activities.ReconcileSkillObservationsParams{
			ProjectID: params.ProjectID,
			BatchSize: skillObservationBatchSize,
		}).Get(ctx, &result); err != nil {
			return fmt.Errorf("reconcile skill observations: %w", err)
		}
		if !result.HasMore {
			return nil
		}
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, ReconcileSkillObservationsWorkflow, params)
		}
	}
	return nil
}

func SkillObservationReconciliationSweepWorkflow(ctx workflow.Context, params SkillObservationReconciliationSweepParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	var a *Activities
	logger := workflow.GetLogger(ctx)
	afterProjectID := params.AfterProjectID
	for range skillObservationMaxPages {
		var projects []uuid.UUID
		if err := workflow.ExecuteActivity(ctx, a.ListProjectsWithPendingSkillObservations, activities.ListPendingSkillObservationProjectsParams{
			AfterProjectID: afterProjectID,
			PageLimit:      skillObservationProjectLimit,
		}).Get(ctx, &projects); err != nil {
			return fmt.Errorf("list projects with pending skill observations: %w", err)
		}
		for _, projectID := range projects {
			childParams := ReconcileSkillObservationsParams{ProjectID: projectID}
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:            reconcileSkillObservationsWorkflowID(childParams),
				WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
				WorkflowRunTimeout:    skillObservationRunTimeout,
				ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
			})
			if err := workflow.ExecuteChildWorkflow(childCtx, ReconcileSkillObservationsWorkflow, childParams).
				GetChildWorkflowExecution().Get(childCtx, nil); err != nil {
				logger.Info("skill observation reconciliation already in flight or failed to start", "project_id", projectID.String(), "error", err.Error())
			}
		}
		if len(projects) < int(skillObservationProjectLimit) {
			return nil
		}
		afterProjectID = projects[len(projects)-1]
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, SkillObservationReconciliationSweepWorkflow, SkillObservationReconciliationSweepParams{
				AfterProjectID: afterProjectID,
			})
		}
	}
	return workflow.NewContinueAsNewError(ctx, SkillObservationReconciliationSweepWorkflow, SkillObservationReconciliationSweepParams{
		AfterProjectID: afterProjectID,
	})
}

func AddSkillObservationReconciliationSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	spec := client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: skillObservationInterval}}}
	action := &client.ScheduleWorkflowAction{
		ID:       skillObservationWorkflowID,
		Workflow: SkillObservationReconciliationSweepWorkflow,
		Args: []any{SkillObservationReconciliationSweepParams{
			AfterProjectID: uuid.Nil,
		}},
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: skillObservationRunTimeout,
	}

	_, err := scheduleClient.Create(ctx, client.ScheduleOptions{
		ID:      skillObservationScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, skillObservationScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
			},
		}); err != nil {
			return fmt.Errorf("update skill observation reconciliation schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create skill observation reconciliation schedule: %w", err)
	}
	return nil
}

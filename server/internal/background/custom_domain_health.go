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
	customDomainHealthPageSize        int32 = 100
	customDomainHealthMaxPages              = 10
	customDomainHealthScheduleID            = "v1:custom-domain-health-schedule"
	customDomainHealthSweepWorkflowID       = customDomainHealthScheduleID + "/scheduled"
	customDomainHealthInterval              = 24 * time.Hour
	customDomainHealthRunTimeout            = 30 * time.Minute
)

type CustomDomainHealthCheckParams struct {
	CustomDomainID uuid.UUID
}

type CustomDomainHealthSweepParams struct {
	AfterID   uuid.UUID
	CheckDate string
}

func scheduledCustomDomainHealthCheckWorkflowID(checkDate string, customDomainID uuid.UUID) string {
	return fmt.Sprintf("v1:custom-domain-health:%s:%s", checkDate, customDomainID.String())
}

func manualCustomDomainHealthCheckWorkflowID(customDomainID uuid.UUID) string {
	return fmt.Sprintf("v1:custom-domain-health:manual:%s:%s", customDomainID.String(), uuid.NewString())
}

func (c *CustomDomainRegistrationClient) ExecuteCustomDomainHealthCheck(ctx context.Context, customDomainID uuid.UUID) (client.WorkflowRun, error) {
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                 manualCustomDomainHealthCheckWorkflowID(customDomainID),
		TaskQueue:          string(c.TemporalEnv.Queue()),
		WorkflowRunTimeout: 5 * time.Minute,
	}, CustomDomainHealthCheckWorkflow, CustomDomainHealthCheckParams{CustomDomainID: customDomainID})
}

func CustomDomainHealthCheckWorkflow(ctx workflow.Context, params CustomDomainHealthCheckParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.CheckCustomDomainHealth, activities.CheckCustomDomainHealthArgs{
		CustomDomainID: params.CustomDomainID,
		CheckedAt:      workflow.Now(ctx).UTC(),
	}).Get(ctx, nil); err != nil {
		return fmt.Errorf("check custom domain health: %w", err)
	}
	return nil
}

func CustomDomainHealthSweepWorkflow(ctx workflow.Context, params CustomDomainHealthSweepParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
		},
	})

	afterID := params.AfterID
	checkDate := params.CheckDate
	if checkDate == "" {
		checkDate = workflow.Now(ctx).UTC().Format(time.DateOnly)
	}
	var a *Activities
	logger := workflow.GetLogger(ctx)
	for range customDomainHealthMaxPages {
		var domainIDs []uuid.UUID
		if err := workflow.ExecuteActivity(ctx, a.ListCustomDomainsForHealthCheck, activities.ListCustomDomainsForHealthCheckArgs{
			AfterID:  afterID,
			PageSize: customDomainHealthPageSize,
		}).Get(ctx, &domainIDs); err != nil {
			return fmt.Errorf("list custom domains for health check: %w", err)
		}

		for _, domainID := range domainIDs {
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:            scheduledCustomDomainHealthCheckWorkflowID(checkDate, domainID),
				WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowRunTimeout:    5 * time.Minute,
				ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
			})
			if err := workflow.ExecuteChildWorkflow(childCtx, CustomDomainHealthCheckWorkflow, CustomDomainHealthCheckParams{
				CustomDomainID: domainID,
			}).GetChildWorkflowExecution().Get(childCtx, nil); err != nil {
				logger.Info("custom domain health check already in flight or failed to start", "custom_domain_id", domainID.String(), "error", err.Error())
			}
		}

		if len(domainIDs) < int(customDomainHealthPageSize) {
			return nil
		}
		afterID = domainIDs[len(domainIDs)-1]
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, CustomDomainHealthSweepWorkflow, CustomDomainHealthSweepParams{AfterID: afterID, CheckDate: checkDate})
		}
	}
	return workflow.NewContinueAsNewError(ctx, CustomDomainHealthSweepWorkflow, CustomDomainHealthSweepParams{AfterID: afterID, CheckDate: checkDate})
}

func AddCustomDomainHealthSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	spec := client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: customDomainHealthInterval}}}
	action := &client.ScheduleWorkflowAction{
		ID:       customDomainHealthSweepWorkflowID,
		Workflow: CustomDomainHealthSweepWorkflow,
		Args: []any{CustomDomainHealthSweepParams{
			AfterID:   uuid.Nil,
			CheckDate: "",
		}},
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: customDomainHealthRunTimeout,
	}

	_, err := scheduleClient.Create(ctx, client.ScheduleOptions{
		ID:      customDomainHealthScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, customDomainHealthScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
			},
		}); err != nil {
			return fmt.Errorf("update custom domain health schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create custom domain health schedule: %w", err)
	}
	return nil
}

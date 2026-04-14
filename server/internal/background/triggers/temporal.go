package triggers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

type TriggerCronWorkflowInput struct {
	TriggerInstanceID string `json:"trigger_instance_id"`
}

type ScheduleWorkflowInput struct {
	TriggerInstanceID string `json:"trigger_instance_id"`
	FiredAt           string `json:"fired_at,omitempty"`
}

type TriggerDispatchWorkflowInput struct {
	Task Task
}

func triggerCronWorkflowScheduleID(id uuid.UUID) string {
	return "v1:trigger-cron-workflow/schedule:" + id.String()
}

func triggerCronWorkflowID(id uuid.UUID) string {
	return "v1:trigger-cron-workflow:" + id.String()
}

func triggerDispatchWorkflowID(eventID string) string {
	return "v1:trigger-dispatch-workflow:" + eventID
}

type ScheduleTriggerCronWorkflowOptions struct {
	InstanceID     uuid.UUID
	InstanceStatus string
	Schedule       string
}

func BuildScheduleOptions(instance triggerrepo.TriggerInstance, schedule string, taskQueue string, workflowName string) client.ScheduleOptions {
	return client.ScheduleOptions{
		ID: triggerCronWorkflowScheduleID(instance.ID),
		Spec: client.ScheduleSpec{
			CronExpressions: []string{schedule},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:       triggerCronWorkflowID(instance.ID),
			Workflow: workflowName,
			Args: []any{ScheduleWorkflowInput{
				TriggerInstanceID: instance.ID.String(),
				FiredAt:           "",
			}},
			TaskQueue:          taskQueue,
			WorkflowRunTimeout: 5 * time.Minute,
		},
		Paused: instance.Status != StatusActive,
	}
}

func ScheduleTriggerCronWorkflow(ctx context.Context, temporalEnv *tenv.Environment, opts ScheduleTriggerCronWorkflowOptions) error {
	if temporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}

	tclient := temporalEnv.Client()
	queue := temporalEnv.Queue()

	handle := tclient.ScheduleClient().GetHandle(ctx, triggerCronWorkflowScheduleID(opts.InstanceID))
	if err := handle.Delete(ctx); err != nil && !isTemporalNotFound(err) {
		return fmt.Errorf("delete existing schedule: %w", err)
	}

	scheduleOpts := BuildScheduleOptions(triggerrepo.TriggerInstance{
		ID:             opts.InstanceID,
		OrganizationID: "",
		ProjectID:      uuid.Nil,
		DefinitionSlug: "",
		Name:           "",
		EnvironmentID: uuid.NullUUID{
			UUID:  uuid.Nil,
			Valid: false,
		},
		TargetKind:    "",
		TargetRef:     "",
		TargetDisplay: "",
		ConfigJson:    nil,
		Status:        opts.InstanceStatus,
		CreatedAt: pgtype.Timestamptz{
			Time:             time.Time{},
			InfinityModifier: 0,
			Valid:            false,
		},
		UpdatedAt: pgtype.Timestamptz{
			Time:             time.Time{},
			InfinityModifier: 0,
			Valid:            false,
		},
		DeletedAt: pgtype.Timestamptz{
			Time:             time.Time{},
			InfinityModifier: 0,
			Valid:            false,
		},
		Deleted: false,
	}, opts.Schedule, string(queue), "TriggerCronWorkflow")

	_, err := tclient.ScheduleClient().Create(ctx, scheduleOpts)
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create schedule: %w", err)
	}

	return nil
}

func DeleteTriggerCronWorkflowSchedule(ctx context.Context, temporalEnv *tenv.Environment, instanceID uuid.UUID) error {
	if temporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}

	handle := temporalEnv.Client().ScheduleClient().GetHandle(ctx, triggerCronWorkflowScheduleID(instanceID))
	if err := handle.Delete(ctx); err != nil && !isTemporalNotFound(err) {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

func ExecuteTriggerDispatchWorkflow(ctx context.Context, temporalEnv *tenv.Environment, input TriggerDispatchWorkflowInput) error {
	if temporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}

	tclient := temporalEnv.Client()
	queue := temporalEnv.Queue()

	_, err := tclient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    triggerDispatchWorkflowID(input.Task.EventID),
		TaskQueue:             string(queue),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, "TriggerDispatchWorkflow", input)
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	switch {
	case errors.As(err, &alreadyStarted):
		return nil
	case err != nil:
		return fmt.Errorf("start trigger dispatch workflow: %w", err)
	default:
		return nil
	}
}

func isTemporalNotFound(err error) bool {
	var notFound *serviceerror.NotFound
	return errors.As(err, &notFound)
}

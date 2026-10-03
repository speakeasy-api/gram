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

func TriggerWakeWorkflowID(id uuid.UUID) string {
	return "v1:trigger-wake-workflow:" + id.String()
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
	handle := temporalEnv.Client().ScheduleClient().GetHandle(ctx, triggerCronWorkflowScheduleID(instanceID))
	if err := handle.Delete(ctx); err != nil && !isTemporalNotFound(err) {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

func ExecuteTriggerDispatchWorkflow(ctx context.Context, temporalEnv *tenv.Environment, input TriggerDispatchWorkflowInput) error {
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

// ExecuteTriggerWakeWorkflow is idempotent: the deterministic per-instance
// workflow ID makes repeated scheduling of the same wake a Temporal-level
// no-op (WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE).
func ExecuteTriggerWakeWorkflow(ctx context.Context, temporalEnv *tenv.Environment, instanceID uuid.UUID, fireAt time.Time) error {
	tclient := temporalEnv.Client()
	queue := temporalEnv.Queue()

	delay := max(time.Until(fireAt), 0)

	_, err := tclient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    TriggerWakeWorkflowID(instanceID),
		TaskQueue:             string(queue),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		StartDelay:            delay,
	}, "TriggerWakeWorkflow", ScheduleWorkflowInput{
		TriggerInstanceID: instanceID.String(),
		FiredAt:           "",
	})
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	switch {
	case errors.As(err, &alreadyStarted):
		return nil
	case err != nil:
		return fmt.Errorf("start trigger wake workflow: %w", err)
	default:
		return nil
	}
}

// CancelTriggerWakeWorkflow: the workflow body honours cancellation by
// exiting before dispatch. NotFound is swallowed so cancelling an already-
// fired or never-started wake is a no-op.
func CancelTriggerWakeWorkflow(ctx context.Context, temporalEnv *tenv.Environment, instanceID uuid.UUID) error {
	if err := temporalEnv.Client().CancelWorkflow(ctx, TriggerWakeWorkflowID(instanceID), ""); err != nil && !isTemporalNotFound(err) {
		return fmt.Errorf("cancel trigger wake workflow: %w", err)
	}
	return nil
}

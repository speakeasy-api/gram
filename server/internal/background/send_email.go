package background

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/email"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const sendEmailWorkflowRunTimeout = 5 * time.Minute

// EmailScheduler implements email.Scheduler by deferring delivery to Temporal.
// Enqueued emails run as one-shot delayed workflows; repeating emails run on a
// Temporal Schedule keyed by recipient and template so re-scheduling updates
// the existing schedule in place.
type EmailScheduler struct {
	TemporalEnv *tenv.Environment
}

var _ email.Scheduler = (*EmailScheduler)(nil)

func (e *EmailScheduler) EnqueueEmail(ctx context.Context, msg email.ScheduledEmail, delay time.Duration) error {
	if delay < 0 {
		delay = 0
	}

	id := fmt.Sprintf("v1:send-email:%s", uuid.NewString())
	_, err := e.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                 id,
		TaskQueue:          string(e.TemporalEnv.Queue()),
		WorkflowRunTimeout: sendEmailWorkflowRunTimeout,
		StartDelay:         delay,
	}, SendEmailWorkflow, msg)
	if err != nil {
		return fmt.Errorf("start send email workflow: %w", err)
	}

	return nil
}

func (e *EmailScheduler) ScheduleRepeatEmail(ctx context.Context, msg email.ScheduledEmail, period time.Duration) error {
	if period <= 0 {
		return fmt.Errorf("repeat email period must be positive")
	}

	scheduleID := repeatEmailScheduleID(msg)
	sc := e.TemporalEnv.Client().ScheduleClient()
	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: period}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 repeatEmailWorkflowID(msg),
		Workflow:           SendEmailWorkflow,
		Args:               []any{msg},
		TaskQueue:          string(e.TemporalEnv.Queue()),
		WorkflowRunTimeout: sendEmailWorkflowRunTimeout,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      scheduleID,
		Spec:    spec,
		Action:  action,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		// A schedule for this recipient+template already exists — update it in
		// place so the caller's latest period and variables take effect.
		if err := sc.GetHandle(ctx, scheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{Schedule: &input.Description.Schedule}, nil
			},
		}); err != nil {
			return fmt.Errorf("update repeat email schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create repeat email schedule: %w", err)
	}

	return nil
}

// SendEmailWorkflow dispatches a single email via the SendEmail activity. It
// backs both one-shot enqueued sends and each tick of a repeating schedule.
func SendEmailWorkflow(ctx workflow.Context, msg email.ScheduledEmail) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    5,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.SendEmail, msg).Get(ctx, nil); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}

// repeatEmailDigest derives a stable identity for a repeating email from its
// recipient and template so re-scheduling targets the same Temporal Schedule.
// Variables are deliberately excluded: a changed greeting should update the
// existing cadence, not spin up a parallel schedule.
func repeatEmailDigest(msg email.ScheduledEmail) string {
	sum := sha256.Sum256([]byte(msg.Recipient + "\x00" + msg.TransactionalID))
	return hex.EncodeToString(sum[:16])
}

func repeatEmailScheduleID(msg email.ScheduledEmail) string {
	return "v1:repeat-email-schedule:" + repeatEmailDigest(msg)
}

func repeatEmailWorkflowID(msg email.ScheduledEmail) string {
	return "v1:repeat-email/" + repeatEmailDigest(msg)
}

package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
	tenvironment "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	trialLifecycleEmailWorkflowIDPrefix              = "v1:trial-lifecycle-email"
	trialLifecycleEmailActivityTimeout               = 5 * time.Minute
	trialLifecycleEmailWorkflowRunTimeout            = 180 * 24 * time.Hour
	trialLifecycleEmailRetryInitialInterval          = 5 * time.Second
	trialLifecycleEmailRetryMaximumInterval          = time.Minute
	trialLifecycleEmailRetryBackoffCoefficient       = 2.0
	trialLifecycleEmailRetryMaximumAttempts    int32 = 5
	trialReminderRetryInitialInterval                = 30 * time.Second
	trialReminderRetryMaximumInterval                = 30 * time.Minute
	trialReminderTimerChunk                          = 30 * 24 * time.Hour
)

func trialLifecycleEmailRetryPolicy() *temporal.RetryPolicy {
	return &temporal.RetryPolicy{
		InitialInterval:    trialLifecycleEmailRetryInitialInterval,
		MaximumInterval:    trialLifecycleEmailRetryMaximumInterval,
		BackoffCoefficient: trialLifecycleEmailRetryBackoffCoefficient,
		MaximumAttempts:    trialLifecycleEmailRetryMaximumAttempts,
	}
}

func trialReminderRetryPolicy() *temporal.RetryPolicy {
	return &temporal.RetryPolicy{
		InitialInterval:    trialReminderRetryInitialInterval,
		MaximumInterval:    trialReminderRetryMaximumInterval,
		BackoffCoefficient: trialLifecycleEmailRetryBackoffCoefficient,
		MaximumAttempts:    0,
	}
}

// TrialLifecycleEmailKind identifies the trial lifecycle change that starts a
// Loops workflow.
type TrialLifecycleEmailKind string

const (
	// TrialStartedEmailKind starts the trial workflow for an organization.
	TrialStartedEmailKind TrialLifecycleEmailKind = "trial-started"
	// AdminAddedEmailKind starts the trial workflow for an added administrator.
	AdminAddedEmailKind TrialLifecycleEmailKind = "admin-added"
	// TrialInactiveEmailKind stops pending trial reminders for an organization.
	TrialInactiveEmailKind TrialLifecycleEmailKind = "trial-inactive"
)

// TrialLifecycleEmailInput identifies the lifecycle change to process in the
// worker. The worker re-reads current state before contacting Loops.
type TrialLifecycleEmailInput struct {
	// Kind identifies the lifecycle change to process.
	Kind TrialLifecycleEmailKind `json:"kind"`

	// OrganizationID identifies the organization whose trial changed.
	OrganizationID string `json:"organization_id"`

	// UserID identifies the administrator affected by an admin-added event.
	UserID string `json:"user_id,omitempty"`

	// ReminderOnly is carried across ContinueAsNew after the immediate lifecycle
	// synchronization has completed.
	ReminderOnly bool `json:"reminder_only,omitempty"`
}

// TemporalTrialEmailNotifier enqueues trial email work without performing the
// database fan-out or calling Loops on the API request path.
type TemporalTrialEmailNotifier struct {
	// TemporalEnv provides the client and task queue used to enqueue workflows.
	TemporalEnv *tenvironment.Environment
}

var _ trialemails.Notifier = (*TemporalTrialEmailNotifier)(nil)

func (n *TemporalTrialEmailNotifier) TrialStarted(ctx context.Context, organizationID string) error {
	return n.enqueue(ctx, TrialLifecycleEmailInput{
		Kind:           TrialStartedEmailKind,
		OrganizationID: organizationID,
		UserID:         "",
		ReminderOnly:   false,
	})
}

func (n *TemporalTrialEmailNotifier) AdminAdded(ctx context.Context, organizationID, userID string) error {
	return n.enqueue(ctx, TrialLifecycleEmailInput{
		Kind:           AdminAddedEmailKind,
		OrganizationID: organizationID,
		UserID:         userID,
		ReminderOnly:   false,
	})
}

func (n *TemporalTrialEmailNotifier) TrialInactive(ctx context.Context, organizationID string) error {
	return n.enqueue(ctx, TrialLifecycleEmailInput{
		Kind:           TrialInactiveEmailKind,
		OrganizationID: organizationID,
		UserID:         "",
		ReminderOnly:   false,
	})
}

func (n *TemporalTrialEmailNotifier) enqueue(ctx context.Context, input TrialLifecycleEmailInput) error {
	if n == nil || n.TemporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}

	_, err := n.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    trialLifecycleEmailWorkflowID(input),
		TaskQueue:             string(n.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    trialLifecycleEmailWorkflowRunTimeout,
	}, TrialLifecycleEmailWorkflow, input)
	if err != nil {
		return fmt.Errorf("enqueue trial lifecycle email workflow: %w", err)
	}
	return nil
}

func trialLifecycleEmailWorkflowID(input TrialLifecycleEmailInput) string {
	return fmt.Sprintf("%s:%s:%s:%s", trialLifecycleEmailWorkflowIDPrefix, input.Kind, input.OrganizationID, input.UserID)
}

func TrialLifecycleEmailWorkflow(ctx workflow.Context, input TrialLifecycleEmailInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: trialLifecycleEmailActivityTimeout,
		RetryPolicy:         trialLifecycleEmailRetryPolicy(),
	})

	var a *Activities
	if !input.ReminderOnly {
		if err := workflow.ExecuteActivity(ctx, a.SendTrialLifecycleEmail, input).Get(ctx, nil); err != nil {
			if input.Kind != TrialStartedEmailKind {
				return fmt.Errorf("send trial lifecycle email: %w", err)
			}
			workflow.GetLogger(ctx).Error("legacy trial lifecycle email failed; continuing to T-3 reminder", "error", err)
		}
	}
	if input.Kind != TrialStartedEmailKind {
		return nil
	}
	input.ReminderOnly = true
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: trialLifecycleEmailActivityTimeout,
		RetryPolicy:         trialReminderRetryPolicy(),
	})

	var state billingnotifications.TrialReminderState
	if err := workflow.ExecuteActivity(ctx, a.ResolveTrialEndingReminder, input.OrganizationID).Get(ctx, &state); err != nil {
		return fmt.Errorf("resolve trial ending reminder: %w", err)
	}
	if !state.Active {
		return nil
	}
	if wait := state.SendAt.Sub(workflow.Now(ctx)); wait > 0 {
		if wait > trialReminderTimerChunk {
			if err := workflow.Sleep(ctx, trialReminderTimerChunk); err != nil {
				return fmt.Errorf("wait to recheck trial ending reminder: %w", err)
			}
			return workflow.NewContinueAsNewError(ctx, TrialLifecycleEmailWorkflow, input)
		}
		if err := workflow.Sleep(ctx, wait); err != nil {
			return fmt.Errorf("wait for trial ending reminder: %w", err)
		}
	}

	var result billingnotifications.SendTrialEndingSoonResult
	if err := workflow.ExecuteActivity(ctx, a.SendTrialEndingSoonEmail, billingnotifications.SendTrialEndingSoonInput{
		OrganizationID: input.OrganizationID,
		TrialCreatedAt: state.TrialCreatedAt,
		TrialEndsAt:    state.TrialEndsAt,
	}).Get(ctx, &result); err != nil {
		return fmt.Errorf("send trial ending soon email: %w", err)
	}
	if result.Reschedule {
		return workflow.NewContinueAsNewError(ctx, TrialLifecycleEmailWorkflow, input)
	}
	return nil
}

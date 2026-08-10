package background

import (
	"context"
	"fmt"
	"time"

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
	trialLifecycleEmailWorkflowRunTimeout            = 30 * time.Minute
	trialLifecycleEmailRetryInitialInterval          = 5 * time.Second
	trialLifecycleEmailRetryMaximumInterval          = time.Minute
	trialLifecycleEmailRetryBackoffCoefficient       = 2.0
	trialLifecycleEmailRetryMaximumAttempts    int32 = 5
)

func trialLifecycleEmailRetryPolicy() *temporal.RetryPolicy {
	return &temporal.RetryPolicy{
		InitialInterval:    trialLifecycleEmailRetryInitialInterval,
		MaximumInterval:    trialLifecycleEmailRetryMaximumInterval,
		BackoffCoefficient: trialLifecycleEmailRetryBackoffCoefficient,
		MaximumAttempts:    trialLifecycleEmailRetryMaximumAttempts,
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
	})
}

func (n *TemporalTrialEmailNotifier) AdminAdded(ctx context.Context, organizationID, userID string) error {
	return n.enqueue(ctx, TrialLifecycleEmailInput{
		Kind:           AdminAddedEmailKind,
		OrganizationID: organizationID,
		UserID:         userID,
	})
}

func (n *TemporalTrialEmailNotifier) TrialInactive(ctx context.Context, organizationID string) error {
	return n.enqueue(ctx, TrialLifecycleEmailInput{
		Kind:           TrialInactiveEmailKind,
		OrganizationID: organizationID,
		UserID:         "",
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
	if err := workflow.ExecuteActivity(ctx, a.SendTrialLifecycleEmail, input).Get(ctx, nil); err != nil {
		return fmt.Errorf("send trial lifecycle email: %w", err)
	}
	return nil
}

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
	trialLifecycleEmailWorkflowIDPrefix = "v1:trial-lifecycle-email"
	trialLifecycleEmailActivityTimeout  = 5 * time.Minute
)

type TrialLifecycleEmailKind string

const (
	TrialStartedEmailKind  TrialLifecycleEmailKind = "trial-started"
	AdminAddedEmailKind    TrialLifecycleEmailKind = "admin-added"
	TrialInactiveEmailKind TrialLifecycleEmailKind = "trial-inactive"
)

// TrialLifecycleEmailInput identifies the lifecycle change to process in the
// worker. The worker re-reads current state before contacting Loops.
type TrialLifecycleEmailInput struct {
	Kind           TrialLifecycleEmailKind `json:"kind"`
	OrganizationID string                  `json:"organization_id"`
	UserID         string                  `json:"user_id,omitempty"`
}

// TemporalTrialEmailNotifier enqueues trial email work without performing the
// database fan-out or calling Loops on the API request path.
type TemporalTrialEmailNotifier struct {
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
		WorkflowRunTimeout:    10 * time.Minute,
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
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			MaximumInterval:    time.Minute,
			BackoffCoefficient: 2,
			MaximumAttempts:    5,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.SendTrialLifecycleEmail, input).Get(ctx, nil); err != nil {
		return fmt.Errorf("send trial lifecycle email: %w", err)
	}
	return nil
}

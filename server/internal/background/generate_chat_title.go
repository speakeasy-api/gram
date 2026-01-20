package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type GenerateChatTitleParams struct {
	ChatID string
	OrgID  string
}

// ChatTitleGenerator schedules async chat title generation.
type ChatTitleGenerator interface {
	ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID string) error
}

// TemporalChatTitleGenerator implements ChatTitleGenerator using Temporal.
type TemporalChatTitleGenerator struct {
	Temporal client.Client
}

func (t *TemporalChatTitleGenerator) ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID string) error {
	_, err := ExecuteGenerateChatTitleWorkflow(ctx, t.Temporal, GenerateChatTitleParams{
		ChatID: chatID,
		OrgID:  orgID,
	})
	return err
}

func ExecuteGenerateChatTitleWorkflow(ctx context.Context, temporalClient client.Client, params GenerateChatTitleParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:generate-chat-title:%s", params.ChatID)
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(TaskQueueMain),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowRunTimeout:    5 * time.Minute,
	}, GenerateChatTitleWorkflow, params)
}

func GenerateChatTitleWorkflow(ctx workflow.Context, params GenerateChatTitleParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
		},
	})

	var a *Activities
	err := workflow.ExecuteActivity(
		ctx,
		a.GenerateChatTitle,
		activities.GenerateChatTitleArgs{
			ChatID: params.ChatID,
			OrgID:  params.OrgID,
		},
	).Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to generate chat title: %w", err)
	}
	return nil
}

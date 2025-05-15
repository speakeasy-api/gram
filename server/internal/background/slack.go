package background

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/internal/thirdparty/slack/types"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

type ProcessSlackWorkflowParams struct {
	Event types.SlackEvent
}

type ProcessSlackEventResult struct {
	Status string
}

func ExecuteProcessSlackEventWorkflow(ctx context.Context, temporalClient client.Client, params ProcessSlackWorkflowParams) (client.WorkflowRun, error) {
	id := params.Event.EventID
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       fmt.Sprintf("v1:slack-event:%s", id),
		TaskQueue:                string(TaskQueueMain),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowRunTimeout:       time.Minute * 3,
	}, SlackEventWorkflow, params)
}

func SlackEventWorkflow(ctx workflow.Context, params ProcessSlackWorkflowParams) (*ProcessSlackEventResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("received slack event", slog.Any("event", params.Event))
	return &ProcessSlackEventResult{
		Status: "success",
	}, nil
}

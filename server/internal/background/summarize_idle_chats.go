package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	summarizeIdleChatsWorkflowID = "v1:summarize-idle-chats"
	summarizeIdleChatsScheduleID = "v1:summarize-idle-chats-schedule"
	// The sweep is the only mechanism that writes background summaries, so it
	// runs well under the 30 minute idle window rather than as an hourly
	// safety net.
	summarizeIdleChatsInterval = 15 * time.Minute
	// summarizeIdleChatsActivityTimeout bounds one run; the run stops starting
	// new summaries summarizeIdleChatsDrain before it so calls in flight finish.
	summarizeIdleChatsActivityTimeout = 12 * time.Minute
	summarizeIdleChatsDrain           = 2 * time.Minute

	summarizeIdleChatsCatchupWindow = summarizeIdleChatsInterval - time.Second
)

// SummarizeIdleChatsWorkflow runs one team memory summary sweep. The activity
// is not retried: the next tick picks up whatever this run did not finish.
func SummarizeIdleChatsWorkflow(ctx workflow.Context) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: summarizeIdleChatsActivityTimeout,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})

	deadline := workflow.Now(ctx).Add(summarizeIdleChatsActivityTimeout - summarizeIdleChatsDrain)
	var result activities.SummarizeIdleChatsResult
	if err := workflow.ExecuteActivity(ctx, a.SummarizeIdleChats, activities.SummarizeIdleChatsArgs{Deadline: deadline}).Get(ctx, &result); err != nil {
		return fmt.Errorf("summarize idle chats: %w", err)
	}
	return nil
}

// AddSummarizeIdleChatsSchedule registers the fleet-wide summary sweep. It
// starts per-chat model calls keyed by database IDs, so only the shared queue
// registers it.
func AddSummarizeIdleChatsSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := createScheduleWithCatchup(ctx, temporalEnv.Client().ScheduleClient(), client.ScheduleOptions{
		CatchupWindow: summarizeIdleChatsCatchupWindow,
		ID:            summarizeIdleChatsScheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: summarizeIdleChatsInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 summarizeIdleChatsWorkflowID,
			Workflow:           SummarizeIdleChatsWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: summarizeIdleChatsActivityTimeout + time.Minute,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create summarize idle chats schedule: %w", err)
	}
	return nil
}

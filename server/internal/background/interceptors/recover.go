package interceptors

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

type Recovery struct {
	interceptor.WorkerInterceptorBase
}

func (*Recovery) InterceptWorkflow(ctx workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &workflowRecovery{
		WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next},
	}
}

func (*Recovery) InterceptActivity(
	ctx context.Context,
	next interceptor.ActivityInboundInterceptor,
) interceptor.ActivityInboundInterceptor {
	return &activityRecovery{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
	}
}

type workflowRecovery struct {
	interceptor.WorkflowInboundInterceptorBase
}

func (w *workflowRecovery) ExecuteWorkflow(ctx workflow.Context, in *interceptor.ExecuteWorkflowInput) (result any, err error) {
	logger := workflow.GetLogger(ctx)
	info := o11y.PullWorkflowExecutionInfo(ctx)

	defer func() {
		if r := recover(); r != nil {
			logger.Error(
				"panic in workflow execution",
				"error", r,
				"attempt", info.Attempt,
				"workflow", info.WorkflowName,
				"run_id", info.RunID,
				"workflow_id", info.WorkflowID,
				"namespace", info.Namespace,
				"task_queue", info.TaskQueue,
			)

			err = fmt.Errorf("panic in workflow execution: %v", r)
			result = nil
		}
	}()

	return w.Next.ExecuteWorkflow(ctx, in)
}

type activityRecovery struct {
	interceptor.ActivityInboundInterceptorBase
}

func (a *activityRecovery) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (result any, err error) {
	logger := activity.GetLogger(ctx)
	info := o11y.PullActivityExecutionInfo(ctx)

	defer func() {
		if r := recover(); r != nil {
			logger.Error(
				"panic in activity execution",
				"error", r,
				"attempt", info.Workflow.Attempt,
				"activity", info.ActivityName,
				"workflow", info.Workflow.WorkflowName,
				"run_id", info.Workflow.RunID,
				"workflow_id", info.Workflow.WorkflowID,
				"activity_id", info.ActivityID,
				"namespace", info.Workflow.Namespace,
				"task_queue", info.Workflow.TaskQueue,
			)

			err = fmt.Errorf("panic in activity execution: %v", r)
			result = nil
		}
	}()

	return a.Next.ExecuteActivity(ctx, in)
}

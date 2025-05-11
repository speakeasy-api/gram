package interceptors

import (
	"context"

	"github.com/speakeasy-api/gram/internal/o11y"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

type InjectExecutionInfo struct {
	interceptor.WorkerInterceptorBase
}

func (*InjectExecutionInfo) InterceptWorkflow(ctx workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &workflowInjectExecutionInfo{
		WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next},
	}
}

func (*InjectExecutionInfo) InterceptActivity(
	ctx context.Context,
	next interceptor.ActivityInboundInterceptor,
) interceptor.ActivityInboundInterceptor {
	return &activityInjectExecutionInfo{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
	}
}

type workflowInjectExecutionInfo struct {
	interceptor.WorkflowInboundInterceptorBase
}

func (w *workflowInjectExecutionInfo) ExecuteWorkflow(ctx workflow.Context, in *interceptor.ExecuteWorkflowInput) (any, error) {
	info := workflow.GetInfo(ctx)

	ctx = o11y.PushWorkflowExecutionInfo(ctx, &o11y.WorkflowExecutionInfo{
		Namespace:    info.Namespace,
		TaskQueue:    info.TaskQueueName,
		WorkflowName: info.WorkflowType.Name,
		WorkflowID:   info.WorkflowExecution.ID,
		RunID:        info.WorkflowExecution.RunID,
		Attempt:      int64(info.Attempt),
	})

	return w.Next.ExecuteWorkflow(ctx, in)
}

type activityInjectExecutionInfo struct {
	interceptor.ActivityInboundInterceptorBase
}

func (a *activityInjectExecutionInfo) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (any, error) {
	info := activity.GetInfo(ctx)

	ctx = o11y.PushActivityExecutionInfo(ctx, &o11y.ActivityExecutionInfo{
		Workflow: o11y.WorkflowExecutionInfo{
			Namespace:    info.WorkflowNamespace,
			TaskQueue:    info.TaskQueue,
			WorkflowName: info.WorkflowType.Name,
			WorkflowID:   info.WorkflowExecution.ID,
			RunID:        info.WorkflowExecution.RunID,
			Attempt:      int64(info.Attempt),
		},
		ActivityID:   info.ActivityID,
		ActivityName: info.ActivityType.Name,
	})

	return a.Next.ExecuteActivity(ctx, in)
}

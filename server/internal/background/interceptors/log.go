package interceptors

import (
	"context"

	"github.com/speakeasy-api/gram/internal/o11y"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

type Logging struct {
	interceptor.WorkerInterceptorBase
}

func (l *Logging) InterceptWorkflow(ctx workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &workflowLogExecution{
		WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next},
	}
}

func (l *Logging) InterceptActivity(
	ctx context.Context,
	next interceptor.ActivityInboundInterceptor,
) interceptor.ActivityInboundInterceptor {
	return &activityLogExecution{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
	}
}

type workflowLogExecution struct {
	interceptor.WorkflowInboundInterceptorBase
}

func (w *workflowLogExecution) ExecuteWorkflow(ctx workflow.Context, in *interceptor.ExecuteWorkflowInput) (any, error) {
	info := o11y.PullWorkflowExecutionInfo(ctx)
	if info == nil {
		return w.Next.ExecuteWorkflow(ctx, in)
	}

	logger := workflow.GetLogger(ctx)

	logger.Debug("workflow started")

	result, err := w.Next.ExecuteWorkflow(ctx, in)
	if err == nil {
		logger.Info("workflow finished")
	} else {
		logger.Error("workflow failed", "error", err.Error())
	}

	return result, err
}

type activityLogExecution struct {
	interceptor.ActivityInboundInterceptorBase
}

func (a *activityLogExecution) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (any, error) {
	info := o11y.PullActivityExecutionInfo(ctx)
	if info == nil {
		return a.Next.ExecuteActivity(ctx, in)
	}

	logger := activity.GetLogger(ctx)

	logger.Debug("activity started")

	result, err := a.Next.ExecuteActivity(ctx, in)
	if err == nil {
		logger.Info("activity finished")
	} else {
		logger.Error("activity failed", "error", err.Error())
	}

	return result, err
}

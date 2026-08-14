package interceptors

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	sdklog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
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
	logWorkflowResult(logger, err)

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
	logActivityResult(logger, err)

	return result, err
}

// logWorkflowResult downgrades Temporal control-flow and expected customer-side
// errors to Info so they stay out of failure alerts and log noise.
func logWorkflowResult(logger sdklog.Logger, err error) {
	switch {
	case err == nil:
		logger.Info("workflow finished")
	case workflow.IsContinueAsNewError(err):
		logger.Info("workflow continuing as new")
	case temporal.IsCanceledError(err) || errors.Is(err, context.Canceled):
		logger.Info("workflow canceled")
	case temporal.IsTimeoutError(err) || errors.Is(err, context.DeadlineExceeded):
		logger.Info("workflow timed out")
	case temporal.IsTerminatedError(err):
		logger.Info("workflow terminated")
	case isBenignApplicationError(err):
		logger.Info("workflow finished with expected error", "error", err.Error())
	default:
		logger.Error("workflow failed", "error", err.Error())
	}
}

// logActivityResult downgrades Temporal control-flow and expected customer-side
// errors to Info so they stay out of failure alerts.
func logActivityResult(logger sdklog.Logger, err error) {
	switch {
	case err == nil:
		logger.Info("activity finished")
	case temporal.IsCanceledError(err) || errors.Is(err, context.Canceled):
		logger.Info("activity canceled")
	case temporal.IsTimeoutError(err) || errors.Is(err, context.DeadlineExceeded):
		logger.Info("activity timed out")
	case isBenignApplicationError(err):
		logger.Info("activity finished with expected error", "error", err.Error())
	default:
		logger.Error("activity failed", "error", err.Error())
	}
}

// isBenignApplicationError reports whether err is (or wraps) an ApplicationError
// marked CategoryBenign. The SDK's own check type-asserts and misses wrappers;
// errors.As is used so ActivityError/fmt.Errorf still match for logging.
func isBenignApplicationError(err error) bool {
	var appErr *temporal.ApplicationError
	return errors.As(err, &appErr) && appErr.Category() == temporal.ApplicationErrorCategoryBenign
}

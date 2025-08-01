package o11y

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type ctxKey string

const (
	appInfoKey               ctxKey = "app"
	workflowExecutionInfoKey ctxKey = "workflow"
	activityExecutionInfoKey ctxKey = "activity"
)

type AppInfo struct {
	Name    string
	Command string
	GitSHA  string
}

func PushAppInfo(ctx context.Context, appInfo *AppInfo) context.Context {
	return context.WithValue(ctx, appInfoKey, appInfo)
}

func PullAppInfo(ctx context.Context) *AppInfo {
	if val, ok := ctx.Value(appInfoKey).(*AppInfo); ok {
		return val
	}

	return &AppInfo{
		Name:    "unset",
		Command: "unset",
		GitSHA:  "unset",
	}
}

type WorkflowExecutionInfo struct {
	Namespace    string
	TaskQueue    string
	WorkflowName string
	WorkflowID   string
	RunID        string
	Attempt      int64
}

type ActivityExecutionInfo struct {
	Workflow     WorkflowExecutionInfo
	ActivityID   string
	ActivityName string
}

func PushWorkflowExecutionInfo(ctx workflow.Context, info *WorkflowExecutionInfo) workflow.Context {
	return workflow.WithValue(ctx, workflowExecutionInfoKey, info)
}

func PullWorkflowExecutionInfo(ctx workflow.Context) *WorkflowExecutionInfo {
	if val, ok := ctx.Value(workflowExecutionInfoKey).(*WorkflowExecutionInfo); ok {
		return val
	}

	return nil
}

func PushActivityExecutionInfo(ctx context.Context, info *ActivityExecutionInfo) context.Context {
	return context.WithValue(ctx, activityExecutionInfoKey, info)
}

func PullActivityExecutionInfo(ctx context.Context) *ActivityExecutionInfo {
	if val, ok := ctx.Value(activityExecutionInfoKey).(*ActivityExecutionInfo); ok {
		return val
	}

	return nil
}

func EnrichToolCallContext(ctx context.Context, logger *slog.Logger, orgSlug, projectSlug string) (context.Context, *slog.Logger) {
	logger = logger.With(
		attr.SlogOrganizationSlug(orgSlug),
		attr.SlogProjectSlug(projectSlug),
	)

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationSlug(orgSlug),
		attr.ProjectSlug(projectSlug),
	)

	orgMember, err := baggage.NewMember(string(attr.OrganizationSlugKey), orgSlug)
	if err != nil {
		logger.WarnContext(ctx, "failed to create organization slug baggage member", attr.SlogError(err))
	}
	projMember, err := baggage.NewMember(string(attr.ProjectSlugKey), projectSlug)
	if err != nil {
		logger.WarnContext(ctx, "failed to create project slug baggage member", attr.SlogError(err))
	}
	bag, err := baggage.New(orgMember, projMember)
	if err != nil {
		logger.WarnContext(ctx, "failed to create baggage", attr.SlogError(err))
	}

	ctx = baggage.ContextWithBaggage(ctx, bag)

	return ctx, logger
}

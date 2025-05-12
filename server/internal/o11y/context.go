package o11y

import (
	"context"

	"go.temporal.io/sdk/workflow"
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

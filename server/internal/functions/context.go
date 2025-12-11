package functions

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type contextKey string

const runnerAuthContextKey contextKey = "runner-auth"

type RunnerAuthContext struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionID   uuid.UUID
}

func (r *RunnerAuthContext) Validate() error {
	if r == nil {
		return fmt.Errorf("nil runner auth context")
	}

	if r.ProjectID == uuid.Nil {
		return fmt.Errorf("invalid project ID")
	}

	if r.DeploymentID == uuid.Nil {
		return fmt.Errorf("invalid deployment ID")
	}

	if r.FunctionID == uuid.Nil {
		return fmt.Errorf("invalid function ID")
	}

	return nil
}

func PullRunnerAuthContext(ctx context.Context) *RunnerAuthContext {
	if ra, ok := ctx.Value(runnerAuthContextKey).(*RunnerAuthContext); ok {
		return ra
	}

	return nil
}

func PushRunnerAuthContext(ctx context.Context, ra *RunnerAuthContext) context.Context {
	if ra != nil {
		return context.WithValue(ctx, runnerAuthContextKey, ra)
	}

	return ctx
}

package functions

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type contextKey string

const runnerAuthContextKey contextKey = "runner-auth"

type runnerAuthContext struct {
	projectID    uuid.UUID
	deploymentID uuid.UUID
	functionID   uuid.UUID
}

func (r *runnerAuthContext) Validate() error {
	if r == nil {
		return fmt.Errorf("nil runner auth context")
	}

	if r.projectID == uuid.Nil {
		return fmt.Errorf("invalid project ID")
	}

	if r.deploymentID == uuid.Nil {
		return fmt.Errorf("invalid deployment ID")
	}

	if r.functionID == uuid.Nil {
		return fmt.Errorf("invalid function ID")
	}

	return nil
}

func pullRunnerAuthContext(ctx context.Context) *runnerAuthContext {
	if ra, ok := ctx.Value(runnerAuthContextKey).(*runnerAuthContext); ok {
		return ra
	}

	return nil
}

func pushRunnerAuthContext(ctx context.Context, ra *runnerAuthContext) context.Context {
	if ra != nil {
		return context.WithValue(ctx, runnerAuthContextKey, ra)
	}

	return ctx
}

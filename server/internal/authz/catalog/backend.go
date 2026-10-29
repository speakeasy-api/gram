package catalog

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Backend evaluates checks for a Client. It is selected once, in wiring.
type Backend interface {
	evaluate(ctx context.Context, checks []authz.Check) ([]authz.CheckResult, error)
	enforce(ctx context.Context, checks []authz.Check) error
	reject(ctx context.Context, err error) error
}

// engineBackend evaluates with the engine's prepared grants and policies, so
// the Client sees exactly what Require and Evaluate see.
type engineBackend struct {
	engine *authz.Engine
}

// NewEngineBackend adapts the existing engine and stored grants.
func NewEngineBackend(engine *authz.Engine) Backend {
	return engineBackend{engine: engine}
}

func (b engineBackend) evaluate(ctx context.Context, checks []authz.Check) ([]authz.CheckResult, error) {
	return b.engine.EvaluateEach(ctx, checks...)
}

// enforce records the challenge exactly as any other Require does.
func (b engineBackend) enforce(ctx context.Context, checks []authz.Check) error {
	return b.engine.Require(ctx, checks...)
}

func (b engineBackend) reject(ctx context.Context, err error) error {
	var shareable *oops.ShareableError
	if errors.As(err, &shareable) {
		return err
	}
	return b.engine.MapError(ctx, err)
}

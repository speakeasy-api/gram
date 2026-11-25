package functions

import (
	"context"
	"errors"
	"net/http"
)

const (
	// ProviderLocal runs function runners as processes on the local machine.
	ProviderLocal = "local"

	// ProviderFlyIO deploys function runners as Fly.io apps and stores their
	// code in a Tigris bucket.
	ProviderFlyIO = "flyio"

	// ProviderDisabled turns Functions off for the deployment: no runner is
	// ever deployed or called, and no Fly.io or Tigris client is built.
	ProviderDisabled = "disabled"
)

// ErrFunctionsDisabled reports that the deployment runs with the disabled
// Functions provider, so the requested Functions work cannot run.
var ErrFunctionsDisabled = errors.New("functions are not available on this deployment")

// Disabled is the Orchestrator for deployments that run with the disabled
// Functions provider. Every method returns ErrFunctionsDisabled without
// contacting any provider.
type Disabled struct{}

var _ Orchestrator = Disabled{}

// Deploy implements Deployer.
func (Disabled) Deploy(context.Context, RunnerDeployRequest) (*RunnerDeployResult, error) {
	return nil, ErrFunctionsDisabled
}

// Reap implements Deployer. A runner recorded before the deployment switched
// to the disabled provider cannot be torn down without a provider client, so
// the reaper stops when it sees ErrFunctionsDisabled and leaves the row as is.
func (Disabled) Reap(context.Context, ReapRequest) error {
	return ErrFunctionsDisabled
}

// ToolCall implements ToolCaller.
func (Disabled) ToolCall(context.Context, RunnerToolCallRequest) (*http.Request, error) {
	return nil, ErrFunctionsDisabled
}

// ReadResource implements ToolCaller.
func (Disabled) ReadResource(context.Context, RunnerResourceReadRequest) (*http.Request, error) {
	return nil, ErrFunctionsDisabled
}

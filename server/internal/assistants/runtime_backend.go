package assistants

import (
	"context"
	"fmt"
	"net/url"
)

const (
	runtimeBackendLocal             = "local"
	runtimeBackendFlyIO             = "flyio"
	runtimeBackendLegacyFirecracker = "firecracker"
)

type RuntimeBackend interface {
	Backend() string
	SupportsBackend(backend string) bool
	Ensure(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendEnsureResult, error)
	Configure(ctx context.Context, runtime assistantRuntimeRecord, config runtimeStartupConfig) error
	RunTurn(ctx context.Context, runtime assistantRuntimeRecord, idempotencyKey string, authToken string, history []runtimeMessage, prompt string) error
	ServerURL(ctx context.Context, runtime assistantRuntimeRecord, raw *url.URL) (*url.URL, error)
	Stop(ctx context.Context, runtime assistantRuntimeRecord) error
}

type RuntimeBackendEnsureResult struct {
	ColdStart           bool
	NeedsConfigure      bool
	BackendMetadataJSON []byte
}

func validateRuntimeBackend(runtime RuntimeBackend, backend string) error {
	if runtime == nil {
		return fmt.Errorf("assistant runtime backend is not configured")
	}
	if runtime.SupportsBackend(backend) {
		return nil
	}
	return fmt.Errorf("assistant runtime backend %q is not supported by configured provider %q", backend, runtime.Backend())
}

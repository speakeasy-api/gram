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
	RunTurn(ctx context.Context, runtime assistantRuntimeRecord, idempotencyKey string, authToken string, prompt string) error
	ServerURL(ctx context.Context, runtime assistantRuntimeRecord, raw *url.URL) (*url.URL, error)
	Status(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendStatus, error)
	Stop(ctx context.Context, runtime assistantRuntimeRecord) error
}

type RuntimeBackendEnsureResult struct {
	ColdStart           bool
	NeedsConfigure      bool
	BackendMetadataJSON []byte
}

// RuntimeBackendStatus mirrors the runner's `/state` response. IdleSeconds is
// nil while a turn is in flight; the runner sets it to None synchronously on
// /turn enqueue so this signal does not lag the request that started work.
type RuntimeBackendStatus struct {
	Configured  bool
	IdleSeconds *uint64
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

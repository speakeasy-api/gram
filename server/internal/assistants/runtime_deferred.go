package assistants

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// errRuntimeNotConfigured is returned by deferredRuntimeBackend for any
// runtime operation. It signals the operator picked a provider whose
// concrete implementation has not been wired up in this binary yet
// (e.g., flyio before the runtime PR lands), so threads cannot execute
// even though the CRUD surface is available.
var errRuntimeNotConfigured = errors.New("assistant runtime is not configured for this provider")

// deferredRuntimeBackend is the placeholder returned by NewRuntimeBackend
// for providers that are accepted by config validation but lack a concrete
// implementation in the current binary. It satisfies RuntimeBackend so the
// assistants service can still mount its endpoints, but every operation
// that would touch a real runtime returns errRuntimeNotConfigured.
type deferredRuntimeBackend struct {
	backend string
}

func newDeferredRuntimeBackend(backend string) *deferredRuntimeBackend {
	return &deferredRuntimeBackend{backend: backend}
}

func (d *deferredRuntimeBackend) Backend() string {
	return d.backend
}

func (d *deferredRuntimeBackend) SupportsBackend(backend string) bool {
	return backend == d.backend
}

func (d *deferredRuntimeBackend) Ensure(_ context.Context, _ assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	return RuntimeBackendEnsureResult{ColdStart: false, NeedsConfigure: false, BackendMetadataJSON: nil}, fmt.Errorf("ensure assistant runtime: %w", errRuntimeNotConfigured)
}

func (d *deferredRuntimeBackend) Configure(_ context.Context, _ assistantRuntimeRecord, _ runtimeStartupConfig) error {
	return fmt.Errorf("configure assistant runtime: %w", errRuntimeNotConfigured)
}

func (d *deferredRuntimeBackend) RunTurn(_ context.Context, _ assistantRuntimeRecord, _ string, _ string, _ []runtimeMessage, _ string) error {
	return fmt.Errorf("run assistant runtime turn: %w", errRuntimeNotConfigured)
}

func (d *deferredRuntimeBackend) ServerURL(_ context.Context, _ assistantRuntimeRecord, _ *url.URL) (*url.URL, error) {
	return nil, fmt.Errorf("resolve assistant runtime server url: %w", errRuntimeNotConfigured)
}

func (d *deferredRuntimeBackend) Stop(_ context.Context, _ assistantRuntimeRecord) error {
	return nil
}

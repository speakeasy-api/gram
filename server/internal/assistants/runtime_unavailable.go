package assistants

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
)

// NewUnavailableRuntimeBackend preserves an unconfigured legacy target without
// redirecting its persisted runtimes to a preview backend. Every operation fails
// explicitly; it neither reports resources missing nor acknowledges cleanup.
func NewUnavailableRuntimeBackend(provider string, serverURL *url.URL) RuntimeBackend {
	return &unavailableRuntimeBackend{provider: provider, serverURL: serverURL}
}

type unavailableRuntimeBackend struct {
	provider  string
	serverURL *url.URL
}

func (b *unavailableRuntimeBackend) Backend() string { return b.provider }
func (b *unavailableRuntimeBackend) SupportsBackend(provider string) bool {
	return provider == b.provider
}
func (b *unavailableRuntimeBackend) ServerURL() *url.URL { return b.serverURL }
func (b *unavailableRuntimeBackend) ImageRef() string    { return "" }
func (b *unavailableRuntimeBackend) unavailable() error {
	return fmt.Errorf("legacy assistant runtime provider %q is not configured in this ATE preview", b.provider)
}
func (b *unavailableRuntimeBackend) Ensure(context.Context, assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	return RuntimeBackendEnsureResult{}, b.unavailable()
}
func (b *unavailableRuntimeBackend) RecycleImage(context.Context, assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	return RuntimeBackendRecycleResult{}, b.unavailable()
}
func (b *unavailableRuntimeBackend) RunTurn(context.Context, assistantRuntimeRecord, runTurnRequest) error {
	return b.unavailable()
}
func (b *unavailableRuntimeBackend) InterruptTurn(context.Context, assistantRuntimeRecord, uuid.UUID) (bool, error) {
	return false, b.unavailable()
}
func (b *unavailableRuntimeBackend) Status(context.Context, assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	return RuntimeBackendStatus{}, b.unavailable()
}
func (b *unavailableRuntimeBackend) Stop(context.Context, assistantRuntimeRecord) error {
	return b.unavailable()
}
func (b *unavailableRuntimeBackend) Reap(context.Context, assistantRuntimeRecord) error {
	return b.unavailable()
}
func (b *unavailableRuntimeBackend) ReapStoppedMachine(context.Context, assistantRuntimeRecord) error {
	return b.unavailable()
}

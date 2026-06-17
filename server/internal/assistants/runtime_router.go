package assistants

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
)

// runtimeRouter fans RuntimeBackend calls out to the backend named by each
// runtime row's Backend column, while presenting the configured target backend
// for everything that concerns a *new* runtime (Backend(), ServerURL(),
// ImageRef()). This lets the server run Fly and GKE side by side: rows keep
// reaching whichever backend created them, and freshly reserved rows are
// stamped with — and admitted onto — the target.
type runtimeRouter struct {
	backends map[string]RuntimeBackend
	target   string
}

func newRuntimeRouter(target string, backends map[string]RuntimeBackend) (*runtimeRouter, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("assistant runtime router requires at least one backend")
	}
	if _, ok := backends[target]; !ok {
		return nil, fmt.Errorf("assistant runtime target backend %q is not configured", target)
	}
	return &runtimeRouter{backends: backends, target: target}, nil
}

// route resolves the backend that owns a runtime row. A row referencing a
// backend the process no longer runs (e.g. flyio rows after a deployment that
// dropped Fly config) is a configuration error surfaced to the caller, not a
// silent no-op — Stop/Reap of such a row would otherwise leak its resources.
func (r *runtimeRouter) route(backend string) (RuntimeBackend, error) {
	b, ok := r.backends[backend]
	if !ok {
		return nil, fmt.Errorf("assistant runtime backend %q is not configured", backend)
	}
	return b, nil
}

func (r *runtimeRouter) Backend() string { return r.target }

func (r *runtimeRouter) SupportsBackend(backend string) bool {
	_, ok := r.backends[backend]
	return ok
}

// ServerURL and ImageRef describe where a new runtime launches, so they resolve
// against the target backend. Both are stable for the process lifetime.
func (r *runtimeRouter) ServerURL() *url.URL { return r.backends[r.target].ServerURL() }
func (r *runtimeRouter) ImageRef() string    { return r.backends[r.target].ImageRef() }

func (r *runtimeRouter) Ensure(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	b, err := r.route(runtime.Backend)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	result, err := b.Ensure(ctx, runtime)
	if err != nil {
		return result, fmt.Errorf("ensure %s runtime: %w", runtime.Backend, err)
	}
	return result, nil
}

func (r *runtimeRouter) RecycleImage(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	b, err := r.route(runtime.Backend)
	if err != nil {
		return RuntimeBackendRecycleResult{}, err
	}
	result, err := b.RecycleImage(ctx, runtime)
	if err != nil {
		return result, fmt.Errorf("recycle %s runtime image: %w", runtime.Backend, err)
	}
	return result, nil
}

func (r *runtimeRouter) RunTurn(ctx context.Context, runtime assistantRuntimeRecord, threadID uuid.UUID, idempotencyKey string, authToken string, prompt string) error {
	b, err := r.route(runtime.Backend)
	if err != nil {
		return err
	}
	// Wrap with %w so the classifyTurnError sentinels the service matches on
	// (ErrRuntimeUnhealthy, ErrCompletionFailed, ErrHistoryCorrupted) survive.
	if err := b.RunTurn(ctx, runtime, threadID, idempotencyKey, authToken, prompt); err != nil {
		return fmt.Errorf("run turn on %s runtime: %w", runtime.Backend, err)
	}
	return nil
}

func (r *runtimeRouter) Status(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	b, err := r.route(runtime.Backend)
	if err != nil {
		return RuntimeBackendStatus{}, err
	}
	status, err := b.Status(ctx, runtime)
	if err != nil {
		return status, fmt.Errorf("status of %s runtime: %w", runtime.Backend, err)
	}
	return status, nil
}

func (r *runtimeRouter) Stop(ctx context.Context, runtime assistantRuntimeRecord) error {
	b, err := r.route(runtime.Backend)
	if err != nil {
		return err
	}
	if err := b.Stop(ctx, runtime); err != nil {
		return fmt.Errorf("stop %s runtime: %w", runtime.Backend, err)
	}
	return nil
}

func (r *runtimeRouter) Reap(ctx context.Context, runtime assistantRuntimeRecord) error {
	b, err := r.route(runtime.Backend)
	if err != nil {
		return err
	}
	if err := b.Reap(ctx, runtime); err != nil {
		return fmt.Errorf("reap %s runtime: %w", runtime.Backend, err)
	}
	return nil
}

var _ RuntimeBackend = (*runtimeRouter)(nil)

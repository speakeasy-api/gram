package assistants

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
)

const (
	runtimeBackendFlyIO = "flyio"
)

type RuntimeBackend interface {
	Backend() string
	SupportsBackend(backend string) bool
	Ensure(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendEnsureResult, error)
	// RunTurn delivers a turn for `threadID` to the runner backing
	// `runtime`. The call lands on /threads/{threadID}/turn so the
	// runner can dispatch to the right per-thread tokio task.
	RunTurn(ctx context.Context, runtime assistantRuntimeRecord, threadID uuid.UUID, idempotencyKey string, authToken string, prompt string) error
	ServerURL(ctx context.Context, runtime assistantRuntimeRecord, raw *url.URL) (*url.URL, error)
	Status(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendStatus, error)
	// Stop halts the active runtime so it can be re-admitted later. Backends
	// may keep persisted state (e.g. Fly app + IP) intact for warm reuse.
	Stop(ctx context.Context, runtime assistantRuntimeRecord) error
	// Reap permanently tears down all backend resources tied to the runtime
	// (e.g. deletes the Fly app). Idempotent: must succeed when the resource
	// is already gone. Distinct from Stop, which may preserve state for reuse.
	Reap(ctx context.Context, runtime assistantRuntimeRecord) error
}

type RuntimeBackendEnsureResult struct {
	ColdStart           bool
	BackendMetadataJSON []byte
}

// RuntimeBackendStatus collapses the runner's per-thread state to the single
// idle signal the manager polls. IdleSeconds is `&0` while any thread has a
// turn in flight (the runner clears that thread's idle clock synchronously
// on /turn enqueue), the minimum idle_seconds across threads when all are
// idle, and `nil` when the runner reports no threads — fully idle, safe to
// recycle or expire.
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

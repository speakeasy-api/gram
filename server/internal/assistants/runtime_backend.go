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
	// ServerURL returns the public base URL the runner uses to reach the
	// management API. The bootstrap response embeds it in MCP and
	// completions URLs handed to the runner, so it must be reachable from
	// the runtime VM (not the host-facing --server-url, which may be
	// loopback during local development).
	ServerURL() *url.URL
	// ImageRef returns the runtime image reference the backend launches
	// machines with, in the "<repo>:<tag>" form. Stable for the lifetime
	// of the process — the tag is stamped at build time.
	ImageRef() string
	Ensure(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendEnsureResult, error)
	// RecycleImage rolls the runtime's existing machine onto the configured
	// runtime image when it is running a stale one, without launching
	// anything new: missing apps/machines are skipped, not created. Gated on
	// the runner's idle clock so an in-flight turn is never interrupted — a
	// busy machine is skipped and the next admission's Ensure picks the
	// upgrade up lazily.
	RecycleImage(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendRecycleResult, error)
	// RunTurn delivers a turn for `threadID` to the runner backing
	// `runtime`. The call lands on /threads/{threadID}/turn so the
	// runner can dispatch to the right per-thread tokio task.
	RunTurn(ctx context.Context, runtime assistantRuntimeRecord, threadID uuid.UUID, idempotencyKey string, authToken string, prompt string) error
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

// RuntimeBackendRecycleResult reports one RecycleImage attempt. Recycled is
// false for every skip (image already current, machine busy or gone) — those
// are expected outcomes, not errors. BackendMetadataJSON is set only when a
// recycle happened and carries the post-recycle machine identity for
// persistence.
type RuntimeBackendRecycleResult struct {
	Recycled            bool
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

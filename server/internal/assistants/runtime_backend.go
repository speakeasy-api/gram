package assistants

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
)

const (
	runtimeBackendFlyIO = "flyio"
)

var ErrWorkspaceGrowthUnsupported = errors.New("assistant runtime workspace growth is not supported")

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
	// ReusesIdleRuntimes reports whether a stopped runtime's resources are
	// kept for warm restart on the next admission (true) or torn down so the
	// next admission always provisions a fresh one (false). It selects how a
	// deploy rolls the fleet onto a new image: reusing backends (Fly) keep the
	// machine and need an in-place RecycleImage; non-reusing backends (GKE)
	// drop the idle runtime so re-admission adopts a fresh warm-pool pod
	// already running the new image — no in-place swap exists or is needed.
	ReusesIdleRuntimes() bool
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
	// mcpServers carries the assistant's current MCP set so the runner
	// can reconcile newly attached or detached servers into a live
	// thread without re-running the full thread bootstrap.
	RunTurn(ctx context.Context, runtime assistantRuntimeRecord, threadID uuid.UUID, idempotencyKey string, authToken string, prompt string, mcpServers []runtimeMCPServer) error
	// GrowWorkspace advances the runtime's workspace storage request by one
	// backend-configured increment. Callers never select an absolute size; the
	// backend enforces its own maximum.
	GrowWorkspace(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendGrowWorkspaceResult, error)
	Status(ctx context.Context, runtime assistantRuntimeRecord) (RuntimeBackendStatus, error)
	// Stop halts the active runtime so it can be re-admitted later. Backends
	// may keep persisted state (e.g. Fly app + IP) intact for warm reuse.
	Stop(ctx context.Context, runtime assistantRuntimeRecord) error
	// Reap permanently tears down all backend resources tied to the runtime
	// (e.g. deletes the Fly app). Idempotent: must succeed when the resource
	// is already gone. Distinct from Stop, which may preserve state for reuse.
	Reap(ctx context.Context, runtime assistantRuntimeRecord) error
	// ReapStoppedMachine tears down only this thread's machine slot, leaving
	// the surrounding app (and any sibling threads' machines) untouched. Used
	// by the per-thread janitor so the next admit for this thread can cold-
	// launch into the same app and keep its IP and secrets. Idempotent.
	ReapStoppedMachine(ctx context.Context, runtime assistantRuntimeRecord) error
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

type RuntimeBackendGrowWorkspaceResult struct {
	CurrentBytes   int64 `json:"current_bytes"`
	RequestedBytes int64 `json:"requested_bytes"`
	Expanded       bool  `json:"expanded"`
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

package codemode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

const (
	// executionLeaseTTL outlives the hard execution deadline and cleanup.
	executionLeaseTTL = MaxWallTime + 15*time.Second
	// cancellationPoll bounds cross-replica cancellation latency without replayable jobs.
	cancellationPoll = 200 * time.Millisecond
	// ownershipTimeout bounds Redis checks and best-effort resource release.
	ownershipTimeout = time.Second
)

// Scope is built by Gram from authenticated request state, never by the runner.
type Scope struct {
	// Organization identifies the admitted organization.
	Organization string `json:"organization"`
	// Project identifies the gateway's owning project.
	Project uuid.UUID `json:"project"`
	// Gateway identifies the stored gateway.
	Gateway uuid.UUID `json:"gateway"`
	// Caller binds all authenticated identity dimensions without ambiguous concatenation.
	Caller []string `json:"caller"`
	// Session isolates MCP clients sharing one credential.
	Session string `json:"session"`
	// Request preserves the JSON-RPC ID's type as well as its value.
	Request json.RawMessage `json:"request"`
}

// Executor coordinates request ownership and sandbox execution.
type Executor interface {
	// Enabled reports whether a runtime is configured on this replica.
	Enabled() bool
	// Execute admits and executes a program once.
	Execute(context.Context, Scope, string, HostFactory) (*ExecutionResult, error)
	// Cancel targets only the exact admitted caller, session and request.
	Cancel(context.Context, Scope) error
}

// HostFactory resolves an execution's tool scope after admission and under its deadline.
type HostFactory func(context.Context) (Host, error)

// Provider acquires an isolated code-purpose runtime for a server-owned scope.
type Provider interface {
	// Acquire returns an exclusive lease or bounded admission failure.
	Acquire(context.Context, Scope) (Lease, error)
}

// Lease holds a runtime until its execution and callbacks have ended.
type Lease interface {
	// Client supplies the authenticated multiplexed runner transport.
	Client() *RunnerClient
	// Release relinquishes ownership, including cleanup after cancellation.
	Release(context.Context) error
}

// OwnershipStore supports atomic ownership without storing code, arguments or credentials.
type OwnershipStore interface {
	cache.Cache
	cache.ConditionalCache
	cache.CompareAndSwapCache
}

// Coordinator uses short Redis ownership records so cancellation works across replicas.
// Polling is bounded by the 30-second execution lifetime; no execution is queued or replayed.
type Coordinator struct {
	provider Provider
	owners   OwnershipStore
}

// NewCoordinator requires distributed ownership even for a locally hosted runner.
func NewCoordinator(provider Provider, owners OwnershipStore) (*Coordinator, error) {
	if provider == nil || owners == nil {
		return nil, fmt.Errorf("code mode requires a runtime provider and ownership store")
	}
	return &Coordinator{provider: provider, owners: owners}, nil
}

// Enabled implements Executor.
func (c *Coordinator) Enabled() bool { return true }

func scopeKey(scope Scope) (string, error) {
	if scope.Organization == "" || scope.Project == uuid.Nil || scope.Gateway == uuid.Nil || scope.Session == "" || len(scope.Request) == 0 || !json.Valid(scope.Request) || string(scope.Request) == "null" {
		return "", fmt.Errorf("incomplete execution scope")
	}
	encoded, err := json.Marshal(scope)
	if err != nil {
		return "", fmt.Errorf("encode execution scope: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return "code-mode:execution:" + hex.EncodeToString(hash[:]), nil
}

// Execute admits one active request and fails closed if ownership is lost.
func (c *Coordinator) Execute(ctx context.Context, scope Scope, code string, factory HostFactory) (*ExecutionResult, error) {
	key, err := scopeKey(scope)
	if err != nil {
		return nil, err
	}
	if len(code) == 0 || len(code) > MaxSourceBytes {
		return nil, fmt.Errorf("program exceeds source limit")
	}
	ctx, cancel := context.WithTimeout(ctx, MaxWallTime)
	defer cancel()
	id := uuid.New()
	owner := id.String()
	acquired, err := c.owners.SetIfAbsent(ctx, key, owner, executionLeaseTTL)
	if err != nil || !acquired {
		return nil, fmt.Errorf("execution admission unavailable or request already active")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), ownershipTimeout)
		defer stop()
		_, _ = c.owners.CompareAndDelete(cleanup, key, owner)
		_, _ = c.owners.CompareAndDelete(cleanup, key, "cancel:"+owner)
	}()
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(cancellationPoll)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check, stop := context.WithTimeout(ctx, ownershipTimeout)
				var current string
				err := c.owners.Get(check, key, &current)
				stop()
				if err != nil || current != owner {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-monitorDone }()
	lease, err := c.provider.Acquire(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("acquire code runtime: %w", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), ownershipTimeout)
		defer stop()
		_ = lease.Release(cleanup)
	}()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("execution cancelled before submission")
	}
	return lease.Client().Run(ctx, id, code, factory), nil
}

// Cancel marks the current owner atomically; a stale cancel cannot stop its replacement.
func (c *Coordinator) Cancel(ctx context.Context, scope Scope) error {
	key, err := scopeKey(scope)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, ownershipTimeout)
	defer cancel()
	var owner string
	if err := c.owners.Get(ctx, key, &owner); err != nil {
		if errors.Is(err, redisCache.ErrCacheMiss) {
			return nil
		}
		return fmt.Errorf("read execution owner: %w", err)
	}
	if _, err := uuid.Parse(owner); err != nil {
		return nil
	}
	if _, err := c.owners.CompareAndSwap(ctx, key, owner, "cancel:"+owner, executionLeaseTTL); err != nil {
		return fmt.Errorf("cancel code execution: %w", err)
	}
	return nil
}

// StaticProvider connects to a dedicated local runner; it is never selected implicitly.
type StaticProvider struct {
	// Runner is configured by the server operator, never by a request.
	Runner *RunnerClient
}

// Acquire shares the local transport while each execution gets a fresh Monty process.
func (p *StaticProvider) Acquire(ctx context.Context, _ Scope) (Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire local runner: %w", err)
	}
	if p.Runner == nil {
		return nil, fmt.Errorf("local code runner unavailable")
	}
	return &staticLease{runner: p.Runner}, nil
}

type staticLease struct{ runner *RunnerClient }

func (l *staticLease) Client() *RunnerClient         { return l.runner }
func (l *staticLease) Release(context.Context) error { return nil }

// Disabled refuses code mode when no compatible runtime is configured.
type Disabled struct{}

// Enabled implements Executor.
func (Disabled) Enabled() bool { return false }

// Execute implements Executor.
func (Disabled) Execute(context.Context, Scope, string, HostFactory) (*ExecutionResult, error) {
	return nil, fmt.Errorf("code mode is unavailable")
}

// Cancel implements Executor.
func (Disabled) Cancel(context.Context, Scope) error { return nil }

// Shutdown releases runtime resources after the HTTP server drains executions.
func (c *Coordinator) Shutdown(ctx context.Context) error {
	if provider, ok := c.provider.(interface{ Shutdown(context.Context) error }); ok {
		if err := provider.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown code provider: %w", err)
		}
	}
	return nil
}

// Shutdown closes the operator-owned local transport after HTTP draining.
func (p *StaticProvider) Shutdown(context.Context) error {
	if p.Runner == nil {
		return nil
	}
	return p.Runner.Close()
}

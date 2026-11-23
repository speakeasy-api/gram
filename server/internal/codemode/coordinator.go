package codemode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"golang.org/x/sync/semaphore"
)

const (
	// DefaultOrganizationExecutions is how many executions one organization
	// may run at once on a replica: four shared runner pods at twelve streams
	// each. It reserves the rest of the replica for other tenants however
	// many caller identities an organization creates.
	DefaultOrganizationExecutions = 4 * RunnerStreams
	// DefaultCallerExecutions bounds one caller's concurrent executions, so
	// one agent fanning out cannot take its whole organization's share.
	DefaultCallerExecutions = 4
	// DefaultAdmissionWait is how long a request waits, first come first
	// served, for a caller, organization or runner slot before it is refused
	// as retryable. Short enough that a client's own timeout never fires first.
	DefaultAdmissionWait = 3 * time.Second
	// executionLeaseTTL outlives the hard execution deadline and cleanup.
	executionLeaseTTL = MaxWallTime + 15*time.Second
	// cancellationPoll bounds cross-replica cancellation latency without replayable jobs.
	cancellationPoll = 200 * time.Millisecond
	// ownershipTimeout bounds Redis checks and best-effort resource release.
	ownershipTimeout = time.Second
	// ownershipFailureLimit consecutive unreadable polls fail an execution closed.
	ownershipFailureLimit = 5
)

// Scope is built by Speakeasy from authenticated request state, never by the runner.
type Scope struct {
	// Organization identifies the admitted organization.
	Organization string `json:"organization"`
	// Project identifies the gateway's owning project; uuid.Nil for the
	// organization gateway, which has none.
	Project uuid.UUID `json:"project"`
	// OrganizationWide marks the organization gateway, whose members span
	// projects. Exactly one of it and Project is set.
	OrganizationWide bool `json:"organization_wide,omitempty"`
	// Gateway identifies the stored gateway, or the organization gateway's
	// derived ID.
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
	// Available reports whether the configured runtime can start runners
	// now. Serving falls back to Progressive discovery while it is false.
	Available() bool
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

// Limits bounds concurrent executions per replica. Zero values use the defaults.
type Limits struct {
	// OrganizationExecutions caps one organization's concurrent executions.
	OrganizationExecutions int

	// CallerExecutions caps one caller identity's concurrent executions.
	CallerExecutions int

	// AdmissionWait bounds how long a request queues for a free slot.
	AdmissionWait time.Duration
}

// Coordinator uses short Redis ownership records so cancellation works across replicas.
// Admission waits briefly, first come first served, for a free slot; an
// execution is never replayed.
type Coordinator struct {
	provider      Provider
	owners        OwnershipStore
	wait          time.Duration
	organizations *keyedLimiter
	callers       *keyedLimiter
}

// NewCoordinator requires distributed ownership even for a locally hosted runner.
func NewCoordinator(provider Provider, owners OwnershipStore, limits Limits) (*Coordinator, error) {
	if provider == nil || owners == nil {
		return nil, fmt.Errorf("code mode requires a runtime provider and ownership store")
	}
	if limits.OrganizationExecutions <= 0 {
		limits.OrganizationExecutions = DefaultOrganizationExecutions
	}
	if limits.CallerExecutions <= 0 {
		limits.CallerExecutions = DefaultCallerExecutions
	}
	if limits.AdmissionWait <= 0 {
		limits.AdmissionWait = DefaultAdmissionWait
	}
	return &Coordinator{provider: provider, owners: owners, wait: limits.AdmissionWait, organizations: newKeyedLimiter(limits.OrganizationExecutions), callers: newKeyedLimiter(limits.CallerExecutions)}, nil
}

// Enabled implements Executor.
func (c *Coordinator) Enabled() bool { return true }

// Available implements Executor. A provider without an availability check
// is always able to run.
func (c *Coordinator) Available() bool {
	if provider, ok := c.provider.(interface{ Available() bool }); ok {
		return provider.Available()
	}
	return true
}

func scopeKey(scope Scope) (string, error) {
	if scope.Organization == "" || (scope.Project == uuid.Nil) != scope.OrganizationWide || scope.Gateway == uuid.Nil || scope.Session == "" || len(scope.Request) == 0 || !json.Valid(scope.Request) || string(scope.Request) == "null" {
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
	started := time.Now()
	// The wall time covers admission too, so a queued request cannot hold
	// its slots past MaxWallTime.
	ctx, cancel := context.WithTimeout(ctx, MaxWallTime)
	defer cancel()
	release, err := c.admit(ctx, scope)
	if err != nil {
		return nil, err
	}
	defer release()
	id := uuid.New()
	owner := id.String()
	acquired, err := c.owners.SetIfAbsent(ctx, key, owner, executionLeaseTTL)
	if err != nil {
		return nil, fmt.Errorf("%w: record execution ownership: %w", ErrRuntimeUnavailable, err)
	}
	if !acquired {
		return nil, ErrAlreadyActive
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), ownershipTimeout)
		defer stop()
		_, _ = c.owners.CompareAndDelete(cleanup, key, owner)
		_, _ = c.owners.CompareAndDelete(cleanup, key, "cancel:"+owner)
	}()
	// Poll ownership only after the provider admits capacity, so refused requests cost no Redis polling.
	lease, err := c.provider.Acquire(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("acquire code runtime: %w", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), ownershipTimeout)
		defer stop()
		_ = lease.Release(cleanup)
	}()
	check, stop := context.WithTimeout(ctx, ownershipTimeout)
	var current string
	err = c.owners.Get(check, key, &current)
	stop()
	switch {
	case err == nil && current == owner:
	case err == nil, errors.Is(err, redisCache.ErrCacheMiss):
		return nil, ErrCancelledBeforeSubmit
	default:
		return nil, fmt.Errorf("%w: read execution ownership: %w", ErrRuntimeUnavailable, err)
	}
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		c.watchOwnership(ctx, key, owner, cancellationPoll)
		cancel()
	}()
	defer func() { cancel(); <-monitorDone }()
	if ctx.Err() != nil {
		return nil, ErrCancelledBeforeSubmit
	}
	result := lease.Client().Run(ctx, id, code, factory)
	// Run reports only the time after submission, so waits for the runner's
	// own capacity count as admission.
	result.Timing.Admission = time.Since(started) - result.Timing.Run
	return result, nil
}

// admit waits up to the admission bound for a caller slot and then an
// organization slot, always in that order so two requests cannot hold each
// other's next slot. Slots cover the whole admitted lifetime, including
// provider acquisition and cleanup.
func (c *Coordinator) admit(ctx context.Context, scope Scope) (func(), error) {
	caller, err := callerKey(scope)
	if err != nil {
		return nil, err
	}
	wait, stop := context.WithTimeout(ctx, c.wait)
	defer stop()
	releaseCaller, err := c.callers.acquire(wait, caller)
	if err != nil {
		return nil, admissionError(ctx, "caller", err)
	}
	releaseOrganization, err := c.organizations.acquire(wait, scope.Organization)
	if err != nil {
		releaseCaller()
		return nil, admissionError(ctx, "organization", err)
	}
	return func() {
		releaseOrganization()
		releaseCaller()
	}, nil
}

// admissionError keeps a caller's own cancellation distinct from a full limit.
func admissionError(ctx context.Context, limit string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("wait for %s execution slot: %w", limit, ctx.Err())
	}
	return fmt.Errorf("%w: %s concurrency limit reached: %w", ErrCapacity, limit, err)
}

// callerKey binds a caller's limit to its organization and every identity
// dimension, without ambiguous concatenation.
func callerKey(scope Scope) (string, error) {
	if scope.Organization == "" || (scope.Project == uuid.Nil) != scope.OrganizationWide || len(scope.Caller) == 0 {
		return "", fmt.Errorf("code runtime requires project ownership and a caller")
	}
	encoded, err := json.Marshal([]any{scope.Organization, scope.Caller})
	if err != nil {
		return "", fmt.Errorf("encode code runtime caller: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// keyedLimiter holds one first-come-first-served semaphore per key and
// forgets a key once nothing holds or waits on it.
type keyedLimiter struct {
	size int64
	mu   sync.Mutex
	keys map[string]*keyedSlot
}

type keyedSlot struct {
	sem   *semaphore.Weighted
	users int
}

func newKeyedLimiter(size int) *keyedLimiter {
	return &keyedLimiter{size: int64(size), mu: sync.Mutex{}, keys: map[string]*keyedSlot{}}
}

func (l *keyedLimiter) acquire(ctx context.Context, key string) (func(), error) {
	l.mu.Lock()
	slot := l.keys[key]
	if slot == nil {
		slot = &keyedSlot{sem: semaphore.NewWeighted(l.size), users: 0}
		l.keys[key] = slot
	}
	slot.users++
	l.mu.Unlock()
	if err := slot.sem.Acquire(ctx, 1); err != nil {
		l.forget(key, slot)
		return nil, fmt.Errorf("acquire execution slot: %w", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			slot.sem.Release(1)
			l.forget(key, slot)
		})
	}, nil
}

func (l *keyedLimiter) forget(key string, slot *keyedSlot) {
	l.mu.Lock()
	defer l.mu.Unlock()
	slot.users--
	if slot.users == 0 {
		delete(l.keys, key)
	}
}

func (l *keyedLimiter) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.keys)
}

// watchOwnership returns once the execution is cancelled, another owner
// holds the key, or Redis stays unreadable for ownershipFailureLimit polls.
// A single failed read is not a cancellation; it would stop programs mid-call.
func (c *Coordinator) watchOwnership(ctx context.Context, key, owner string, poll time.Duration) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check, stop := context.WithTimeout(ctx, ownershipTimeout)
			var current string
			err := c.owners.Get(check, key, &current)
			stop()
			switch {
			case err == nil && current == owner:
				failures = 0
			case err == nil, errors.Is(err, redisCache.ErrCacheMiss):
				return
			default:
				failures++
				if failures >= ownershipFailureLimit {
					return
				}
			}
		}
	}
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
		return nil, fmt.Errorf("%w: local code runner is not configured", ErrRuntimeUnavailable)
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

// Available implements Executor.
func (Disabled) Available() bool { return false }

// Execute implements Executor.
func (Disabled) Execute(context.Context, Scope, string, HostFactory) (*ExecutionResult, error) {
	return nil, ErrRuntimeUnavailable
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

package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// scriptedOwners answers ownership reads from a script, then keeps
// answering with its last entry.
type scriptedOwners struct {
	OwnershipStore
	mu     sync.Mutex
	script []ownerRead
	reads  int
}

type ownerRead struct {
	owner string
	err   error
}

func (s *scriptedOwners) Get(_ context.Context, _ string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	read := s.script[min(s.reads, len(s.script)-1)]
	s.reads++
	if read.err != nil {
		return read.err
	}
	owner, ok := value.(*string)
	if !ok {
		return fmt.Errorf("scripted owner read into %T", value)
	}
	*owner = read.owner
	return nil
}

func (s *scriptedOwners) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

func TestOwnershipWatchToleratesTransientReadErrors(t *testing.T) {
	t.Parallel()
	blip := errors.New("i/o timeout")
	owned := ownerRead{owner: "owner", err: nil}
	failed := ownerRead{owner: "", err: blip}
	cases := map[string]struct {
		script    []ownerRead
		stops     bool
		minReads  int
		stopAfter int
	}{
		"recovers after a short outage":  {script: []ownerRead{failed, failed, failed, failed, owned, failed, failed, failed, failed, owned}, stops: false, minReads: 12, stopAfter: 0},
		"stops after a sustained outage": {script: []ownerRead{owned, failed}, stops: true, minReads: 0, stopAfter: 1 + ownershipFailureLimit},
		"stops on a missing record":      {script: []ownerRead{owned, {owner: "", err: redisCache.ErrCacheMiss}}, stops: true, minReads: 0, stopAfter: 2},
		"stops when another owner holds": {script: []ownerRead{owned, {owner: "cancel:owner", err: nil}}, stops: true, minReads: 0, stopAfter: 2},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			owners := &scriptedOwners{OwnershipStore: nil, mu: sync.Mutex{}, script: tc.script, reads: 0}
			coordinator := &Coordinator{provider: nil, owners: owners}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				coordinator.watchOwnership(ctx, "key", "owner", time.Millisecond)
			}()
			if tc.stops {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("watch did not stop")
				}
				require.Equal(t, tc.stopAfter, owners.readCount())
				return
			}
			require.Eventually(t, func() bool { return owners.readCount() >= tc.minReads }, 5*time.Second, time.Millisecond)
			select {
			case <-done:
				t.Fatal("a transient read error stopped the execution")
			default:
			}
		})
	}
}

func TestCoordinatorLimitsCallerAndOrganizationConcurrency(t *testing.T) {
	t.Parallel()
	coordinator, err := NewCoordinator(&StaticProvider{}, &scriptedOwners{})
	require.NoError(t, err)
	scope := Scope{Organization: "test-org", Project: uuid.New(), Caller: []string{"caller-0"}}
	var releases []func()
	for i := range maxOrganizationExecutions {
		scope.Caller = []string{fmt.Sprintf("caller-%d", i/maxCallerExecutions)}
		release, err := coordinator.admit(scope)
		require.NoError(t, err)
		releases = append(releases, release)
		if i == maxCallerExecutions-1 {
			_, err := coordinator.admit(scope)
			require.ErrorContains(t, err, "concurrency limit")
		}
	}
	scope.Caller = []string{"another-caller"}
	_, err = coordinator.admit(scope)
	require.ErrorContains(t, err, "concurrency limit")
	other := scope
	other.Organization = "another-org"
	release, err := coordinator.admit(other)
	require.NoError(t, err, "another tenant retains capacity")
	release()
	for _, release := range releases {
		release()
	}
	release, err = coordinator.admit(scope)
	require.NoError(t, err, "completed executions release capacity")
	release()
	require.Empty(t, coordinator.organizations)
	require.Empty(t, coordinator.callers)
}

type executionCleanupOwners struct {
	OwnershipStore
	deleted int
}

func (*executionCleanupOwners) SetIfAbsent(context.Context, string, any, time.Duration) (bool, error) {
	return true, nil
}

func (s *executionCleanupOwners) CompareAndDelete(ctx context.Context, _ string, _ any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("cleanup cancelled: %w", err)
	}
	s.deleted++
	return true, nil
}

type executionFailureProvider struct {
	cancel context.CancelFunc
}

func (p executionFailureProvider) Acquire(ctx context.Context, _ Scope) (Lease, error) {
	if p.cancel != nil {
		p.cancel()
		<-ctx.Done()
		return nil, fmt.Errorf("acquisition cancelled: %w", ctx.Err())
	}
	return nil, fmt.Errorf("runtime unavailable")
}

func TestCoordinatorExecuteReleasesAdmissionOnErrorAndCancellation(t *testing.T) {
	t.Parallel()
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%t", cancelled), func(t *testing.T) {
			t.Parallel()
			owners := &executionCleanupOwners{}
			provider := &executionFailureProvider{}
			coordinator, err := NewCoordinator(provider, owners)
			require.NoError(t, err)
			scope := Scope{Organization: "test-org", Project: uuid.New(), Gateway: uuid.New(), Caller: []string{"caller"}, Session: "session", Request: json.RawMessage(`1`)}
			for range maxCallerExecutions + 1 {
				ctx, cancel := context.WithCancel(t.Context())
				if cancelled {
					provider.cancel = cancel
				}
				_, err := coordinator.Execute(ctx, scope, "1", nil)
				cancel()
				if cancelled {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.ErrorContains(t, err, "runtime unavailable")
				}
				require.Empty(t, coordinator.organizations)
				require.Empty(t, coordinator.callers)
			}
			require.Equal(t, 2*(maxCallerExecutions+1), owners.deleted, "cancellation still cleans distributed ownership")
		})
	}
}

package codemode

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
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
	*value.(*string) = read.owner
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

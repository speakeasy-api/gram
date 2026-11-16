package tunnelrouting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/stretchr/testify/require"
)

type lookupStore struct {
	route.Store
	candidates func(context.Context, string) ([]string, error)
}

func (s lookupStore) Candidates(ctx context.Context, id string) ([]string, error) {
	return s.candidates(ctx, id)
}

func TestLookupRoutesImmediate(t *testing.T) {
	t.Parallel()
	calls := 0
	store := lookupStore{candidates: func(ctx context.Context, id string) ([]string, error) {
		calls++
		require.Equal(t, "tunnel-1", id)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(time.Second), deadline, 100*time.Millisecond)
		return []string{"gateway"}, nil
	}}
	routes, err := LookupRoutes(t.Context(), store, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"gateway"}, routes)
	require.Equal(t, 1, calls)
}

func TestLookupRoutesLatePublication(t *testing.T) {
	t.Parallel()
	calls := 0
	store := lookupStore{candidates: func(context.Context, string) ([]string, error) {
		calls++
		if calls < 3 {
			return nil, nil
		}
		return []string{"gateway"}, nil
	}}
	routes, err := LookupRoutes(t.Context(), store, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"gateway"}, routes)
	require.Equal(t, 3, calls)
}

func TestLookupRoutesExhaustion(t *testing.T) {
	t.Parallel()
	calls := 0
	store := lookupStore{candidates: func(context.Context, string) ([]string, error) {
		calls++
		return nil, nil
	}}
	start := time.Now()
	routes, err := LookupRoutes(t.Context(), store, "tunnel-1")
	require.NoError(t, err)
	require.Empty(t, routes)
	require.GreaterOrEqual(t, time.Since(start), time.Second)
	require.Less(t, time.Since(start), 2*time.Second)
	require.Greater(t, calls, 1)
	require.LessOrEqual(t, calls, 21)
}

func TestLookupRoutesStoreReadBudget(t *testing.T) {
	t.Parallel()
	calls := 0
	store := lookupStore{candidates: func(ctx context.Context, _ string) ([]string, error) {
		calls++
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	start := time.Now()
	routes, err := LookupRoutes(t.Context(), store, "tunnel-1")
	require.NoError(t, err)
	require.Empty(t, routes)
	require.Less(t, time.Since(start), 2*time.Second)
	require.Equal(t, 1, calls)
}

func TestLookupRoutesParentCancellation(t *testing.T) {
	t.Parallel()
	for _, blockedRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "waiting", true: "reading"}[blockedRead], func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			store := lookupStore{candidates: func(ctx context.Context, _ string) ([]string, error) {
				if blockedRead {
					cancel()
					<-ctx.Done()
					return nil, ctx.Err()
				}
				// Cancel after returning an empty lookup, during backoff.
				time.AfterFunc(10*time.Millisecond, cancel)
				return nil, nil
			}}
			routes, err := LookupRoutes(ctx, store, "tunnel-1")
			require.ErrorIs(t, err, context.Canceled)
			require.Empty(t, routes)
		})
	}
}

func TestLookupRoutesParentDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	store := lookupStore{candidates: func(ctx context.Context, _ string) ([]string, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	routes, err := LookupRoutes(ctx, store, "tunnel-1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, routes)
}

func TestLookupRoutesStoreError(t *testing.T) {
	t.Parallel()
	for _, failAt := range []int{1, 2} {
		t.Run(map[int]string{1: "initial", 2: "after_empty"}[failAt], func(t *testing.T) {
			t.Parallel()
			failure := errors.New("store unavailable")
			calls := 0
			store := lookupStore{candidates: func(context.Context, string) ([]string, error) {
				calls++
				if calls == failAt {
					return nil, failure
				}
				return nil, nil
			}}
			routes, err := LookupRoutes(t.Context(), store, "tunnel-1")
			require.ErrorIs(t, err, failure)
			require.Empty(t, routes)
			require.Equal(t, failAt, calls)
		})
	}
}

func TestLookupRoutesPreservesStoreErrorAtDeadline(t *testing.T) {
	t.Parallel()
	failure := errors.New("store unavailable")
	store := lookupStore{candidates: func(ctx context.Context, _ string) ([]string, error) {
		<-ctx.Done()
		return nil, failure
	}}
	routes, err := LookupRoutes(t.Context(), store, "tunnel-1")
	require.ErrorIs(t, err, failure)
	require.Empty(t, routes)
}

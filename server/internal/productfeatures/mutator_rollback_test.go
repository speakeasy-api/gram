package productfeatures

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMutationCacheContextIgnoresRequestCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cacheCtx, cacheCancel := mutationCacheContext(ctx)
	defer cacheCancel()
	require.NoError(t, cacheCtx.Err())
	deadline, ok := cacheCtx.Deadline()
	require.True(t, ok)
	require.LessOrEqual(t, time.Until(deadline), mutationCacheTimeout)
}

func TestRollbackTransactionIgnoresRequestCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(t.Context(), rollbackContextKey{}, "value")
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	called := false
	err := rollbackTransaction(ctx, func(rollbackCtx context.Context) error {
		called = true
		require.NoError(t, rollbackCtx.Err())
		require.Equal(t, "value", rollbackCtx.Value(rollbackContextKey{}))
		return nil
	})

	require.NoError(t, err)
	require.True(t, called)
}

type rollbackContextKey struct{}

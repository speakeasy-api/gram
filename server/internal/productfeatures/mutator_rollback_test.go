package productfeatures

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

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

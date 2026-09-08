package testenv

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMemoryCache_GetAndDeleteSingleUse pins the GETDEL semantics the
// auth-code redemption tests depend on: many concurrent redeemers, exactly
// one winner.
func TestMemoryCache_GetAndDeleteSingleUse(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := NewMemoryCache()
	require.NoError(t, c.Set(ctx, "code", "value", 0))

	const redeemers = 32
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range redeemers {
		wg.Go(func() {
			<-start
			var got string
			if err := c.GetAndDelete(ctx, "code", &got); err == nil {
				wins.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()

	require.Equal(t, int32(1), wins.Load())
}

package cache_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

func TestNoopCacheDoesNotAdvertiseRenewableLeases(t *testing.T) {
	t.Parallel()

	_, ok := any(cache.NoopCache).(cache.RenewableLeaseCache)
	require.False(t, ok)
}

package remotesessions_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestRecheckLease(t *testing.T) {
	t.Parallel()
	for _, interval := range []time.Duration{time.Nanosecond, time.Second, time.Minute, 20 * time.Minute} {
		require.Equal(t, 5*time.Minute, remotesessions.RecheckLease(interval))
	}
	require.Equal(t, 6*time.Hour, remotesessions.RecheckLease(24*time.Hour))
}

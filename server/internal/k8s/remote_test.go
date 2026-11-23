package k8s

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoteClientRejectsInvalidRateLimits(t *testing.T) {
	t.Parallel()
	for name, options := range map[string]RemoteClientOptions{
		"nan":           {QPS: float32(math.NaN()), Burst: 1},
		"infinite":      {QPS: float32(math.Inf(1)), Burst: 1},
		"negative":      {QPS: -1, Burst: 1},
		"missing burst": {QPS: 1, Burst: 0},
		"missing qps":   {QPS: 0, Burst: 1},
	} {
		_, err := NewRemoteDynamicClient(t.Context(), "cluster.example.com", []byte("ca"), options)
		require.ErrorContains(t, err, "QPS and burst", name)
	}
}

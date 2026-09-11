package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

func TestProbeProxyBuilderDisablesTrafficMetrics(t *testing.T) {
	t.Parallel()

	built, err := probeProxyBuilder(func(context.Context) (*proxy.Proxy, error) {
		return &proxy.Proxy{Metrics: &proxy.Metrics{}}, nil
	})(t.Context())
	require.NoError(t, err)
	require.Nil(t, built.Metrics)
}

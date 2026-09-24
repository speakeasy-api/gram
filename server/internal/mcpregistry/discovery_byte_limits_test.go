package mcpregistry

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestDiscoveryFilterUTF8ByteLimits(t *testing.T) {
	t.Parallel()
	ctx, service, _ := newTestService(t)
	for _, value := range []string{strings.Repeat("a", 1024), strings.Repeat("é", 512)} {
		_, err := service.Discover(ctx, DiscoveryOptions{Search: value})
		require.NoError(t, err)
		_, err = service.Discover(ctx, DiscoveryOptions{Version: value})
		require.NoError(t, err)
	}
	for _, value := range []string{strings.Repeat("a", 1025), strings.Repeat("é", 513)} {
		_, err := service.Discover(ctx, DiscoveryOptions{Search: value})
		require.ErrorIs(t, err, ErrInvalidListOptions)
		_, err = service.Discover(ctx, DiscoveryOptions{Version: value})
		require.ErrorIs(t, err, ErrInvalidListOptions)
	}
}

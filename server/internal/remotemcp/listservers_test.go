package remotemcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
)

func TestListServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Create two servers
	for _, url := range []string{"https://mcp1.example.com", "https://mcp2.example.com"} {
		_, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			URL:              url,
			TransportType:    "streamable-http",
		})
		require.NoError(t, err)
	}

	result, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.RemoteMcpServers), 2)
}

func TestListServers_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.RemoteMcpServers)
}

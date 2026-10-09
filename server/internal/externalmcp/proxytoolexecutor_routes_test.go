package externalmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProxyToolExecutorRoutesOnlyItsOwnSlugs(t *testing.T) {
	t.Parallel()

	proxy := "proxy"
	executor := externalmcp.BuildProxyToolExecutor(testenv.NewLogger(t), nil, []*types.Tool{{
		ExternalMcpToolDefinition: &types.ExternalMCPToolDefinition{
			Type:    &proxy,
			Slug:    "devices",
			ToolUrn: "tools:externalmcp:devices:proxy",
		},
	}})

	require.True(t, executor.Routes("devices--read_data"))
	require.True(t, executor.Routes("devices--with--delimiter"))
	require.False(t, executor.Routes("other--read_data"))
	require.False(t, executor.Routes("devices"))
	require.False(t, executor.Routes("read_data"))
}

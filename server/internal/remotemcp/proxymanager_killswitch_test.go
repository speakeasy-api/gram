package remotemcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestProxyManagerAttachesKillswitchToEveryPrivateBackend(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	manager := NewProxyManager(
		logger,
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil,
	)

	canonicalServerID := uuid.NewString()
	remote := manager.Build(
		logger,
		&remotemcprepo.RemoteMcpServer{ID: uuid.New(), Url: "https://example.com/mcp"},
		canonicalServerID,
		nil,
		mcpservers.VisibilityPrivate,
		"organization-id",
		"project-id",
		"",
		"",
		nil,
	)
	tunnel := manager.BuildTarget(
		logger,
		proxy.ServerIdentity{TunneledMCPServerID: uuid.NewString(), McpServerID: canonicalServerID},
		"https://tunnel.example.com/mcp",
		nil,
		mcpservers.VisibilityPrivate,
		"organization-id",
		"project-id",
		"",
		"",
		nil,
	)
	public := manager.BuildTarget(
		logger,
		proxy.ServerIdentity{RemoteMCPServerID: uuid.NewString(), McpServerID: canonicalServerID},
		"https://public.example.com/mcp",
		nil,
		mcpservers.VisibilityPublic,
		"organization-id",
		"project-id",
		"",
		"",
		nil,
	)

	for _, built := range []*proxy.Proxy{remote, tunnel} {
		names := toolsCallInterceptorNames(built)
		require.GreaterOrEqual(t, len(names), 5)
		require.Equal(t, []string{
			"tools-call-otel-counter",
			"tools-call-killswitch",
			"tools-call-usage-limits",
			"tools-call-strip-toolset-id",
			"tools-call-clickhouse-log",
		}, names[:5])
	}
	require.NotContains(t, toolsCallInterceptorNames(public), "tools-call-killswitch")
}

func toolsCallInterceptorNames(p *proxy.Proxy) []string {
	names := make([]string, 0, len(p.ToolsCallRequestInterceptors))
	for _, interceptor := range p.ToolsCallRequestInterceptors {
		names = append(names, interceptor.Name())
	}
	return names
}

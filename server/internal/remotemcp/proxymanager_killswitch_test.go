package remotemcp

import (
	"slices"
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

	checkpoint := &fakeKillswitchCheckpoint{}
	manager.killswitchCheckpoint = checkpoint

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
		preForward := toolsCallPreForwardInterceptorNames(built)
		otelPosition := slices.Index(preForward, "tools-call-otel-counter")
		killswitchPosition := slices.Index(preForward, "tools-call-killswitch")
		require.NotEqual(t, -1, otelPosition)
		require.NotEqual(t, -1, killswitchPosition)
		require.Equal(t, 1, countInterceptorName(preForward, "tools-call-otel-counter"))
		require.Equal(t, 1, countInterceptorName(preForward, "tools-call-killswitch"))
		require.Less(t, otelPosition, killswitchPosition, "the OTel census must run before enforcement")

		downstream := toolsCallInterceptorNames(built)
		require.NotEmpty(t, downstream, "enforcement must run before downstream tools/call work")
		require.NotContains(t, downstream, "tools-call-otel-counter")
		require.NotContains(t, downstream, "tools-call-killswitch")
	}
	publicInterceptors := append([]proxy.ToolsCallRequestInterceptor(nil), public.ToolsCallPreForwardInterceptors...)
	publicInterceptors = append(publicInterceptors, public.ToolsCallRequestInterceptors...)
	require.NotContains(t, append(toolsCallPreForwardInterceptorNames(public), toolsCallInterceptorNames(public)...), "tools-call-killswitch")
	publicCounters := 0
	for _, interceptor := range publicInterceptors {
		counter, ok := interceptor.(*ToolsCallOTELCounterInterceptor)
		if !ok {
			continue
		}
		publicCounters++
		require.Nil(t, counter.identityCoverage, "public calls must not record private-proxy identity coverage")
		require.NoError(t, counter.InterceptToolsCallRequest(t.Context(), newToolsCallRequestForCounter(t, "public_tool")))
	}
	require.Equal(t, 1, publicCounters)
	require.Zero(t, checkpoint.calls, "public calls must not trigger private identity/resource resolution")
}

func countInterceptorName(names []string, target string) int {
	count := 0
	for _, name := range names {
		if name == target {
			count++
		}
	}
	return count
}

func toolsCallPreForwardInterceptorNames(p *proxy.Proxy) []string {
	names := make([]string, 0, len(p.ToolsCallPreForwardInterceptors))
	for _, interceptor := range p.ToolsCallPreForwardInterceptors {
		names = append(names, interceptor.Name())
	}
	return names
}

func toolsCallInterceptorNames(p *proxy.Proxy) []string {
	names := make([]string, 0, len(p.ToolsCallRequestInterceptors))
	for _, interceptor := range p.ToolsCallRequestInterceptors {
		names = append(names, interceptor.Name())
	}
	return names
}

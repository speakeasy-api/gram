package remotemcp

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/metrics"
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
		userRequest := userRequestInterceptorNames(built)
		require.NotEqual(t, -1, slices.Index(userRequest, "tools-call-otel-counter"))
		require.Equal(t, 1, countInterceptorName(userRequest, "tools-call-otel-counter"))
		require.NotContains(t, userRequest, "tools-call-identity-coverage", "private enforcement reuses its authoritative derivation for coverage")

		preForward := toolsCallPreForwardInterceptorNames(built)
		require.NotEqual(t, -1, slices.Index(preForward, "tools-call-killswitch"))
		require.Equal(t, 1, countInterceptorName(preForward, "tools-call-killswitch"))
		require.NotContains(t, preForward, "tools-call-otel-counter", "the generic request census already runs before typed pre-forward enforcement")

		downstream := toolsCallInterceptorNames(built)
		require.NotEmpty(t, downstream, "enforcement must run before downstream tools/call work")
		require.NotContains(t, downstream, "tools-call-otel-counter")
		require.NotContains(t, downstream, "tools-call-killswitch")
	}
	require.NotContains(t, append(toolsCallPreForwardInterceptorNames(public), toolsCallInterceptorNames(public)...), "tools-call-killswitch")
	require.Contains(t, userRequestInterceptorNames(public), "tools-call-identity-coverage", "public calls retain the standalone census")
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

func userRequestInterceptorNames(p *proxy.Proxy) []string {
	names := make([]string, 0, len(p.UserRequestObservationInterceptors))
	for _, interceptor := range p.UserRequestObservationInterceptors {
		names = append(names, interceptor.Name())
	}
	return names
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

func TestProxyManagerAttachesTunnelMetricsOnlyToTunnelTargets(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	manager := NewProxyManager(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	manager.TunnelMetrics = metrics.New()
	source, server := uuid.NewString(), uuid.NewString()
	build := func(source string) *proxy.Proxy {
		return manager.BuildTarget(logger, proxy.ServerIdentity{TunneledMCPServerID: source, McpServerID: server}, "https://example.com/mcp", nil, mcpservers.VisibilityPrivate, "example-org", "example-project", "", "", nil)
	}
	require.Nil(t, build("").RequestObserver)
	observed := build(source)
	require.NotNil(t, observed.RequestObserver)
	observed.RequestObserver("tools/list", "claude", "attempt", time.Second)
	var rows []metrics.Snapshot
	manager.TunnelMetrics.Flush(t.Context(), func(_ context.Context, s metrics.Snapshot) error {
		if s.Kind == "requests" {
			rows = append(rows, s)
		}
		return nil
	})
	require.Len(t, rows, 1)
	require.Equal(t, source, rows[0].SourceID)
	require.Equal(t, server, rows[0].ServerID)
	require.Equal(t, "tools/list", rows[0].Method)
	require.Equal(t, "claude", rows[0].ClientFamily)
	require.EqualValues(t, 1, rows[0].Attempts)
}

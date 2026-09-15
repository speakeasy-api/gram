package remotemcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProxyBuildOption_ToolsCallIdentityCoverage(t *testing.T) {
	t.Parallel()

	manager := &ProxyManager{}
	build := func(options ...BuildOption) *proxy.Proxy {
		return manager.BuildTarget(
			testenv.NewLogger(t),
			proxy.ServerIdentity{},
			"https://example.com/mcp",
			nil,
			"public",
			"org-test",
			"project-test",
			"",
			"",
			nil,
			options...,
		)
	}
	hasUserInterceptor := func(p *proxy.Proxy, name string) bool {
		for _, interceptor := range p.UserRequestObservationInterceptors {
			if interceptor != nil && interceptor.Name() == name {
				return true
			}
		}
		return false
	}
	hasTypedInterceptor := func(p *proxy.Proxy, name string) bool {
		for _, interceptor := range p.ToolsCallRequestInterceptors {
			if interceptor != nil && interceptor.Name() == name {
				return true
			}
		}
		return false
	}

	defaultProxy := build()
	require.True(t, hasUserInterceptor(defaultProxy, "tools-call-identity-coverage"), "client-facing proxies census tools/call by default")
	require.True(t, hasUserInterceptor(defaultProxy, "tools-call-otel-counter"), "attempt accounting runs before typed params decode")
	require.False(t, hasTypedInterceptor(defaultProxy, "tools-call-otel-counter"), "typed params decode must not gate attempt accounting")

	withoutCoverage := build(WithoutToolsCallIdentityCoverage())
	require.False(t, hasUserInterceptor(withoutCoverage, "tools-call-identity-coverage"), "synthetic meta-member exchanges opt out")
	require.True(t, hasUserInterceptor(withoutCoverage, "tools-call-otel-counter"), "identity census opt-out must not disable attempt accounting")
}

// Package interceptors holds per-vendor policies applied to remote-MCP proxy
// traffic. Each policy is a [proxy.UserRequestInterceptor] that self-selects
// via Match, keyed on the upstream URL known only at request time. The
// per-type / Name() / injected-slice shape follows remotemcp/proxy and
// remotesessions/interceptors — there is no package-global registry; the set
// is assembled per request in remotemcp.ProxyManager.BuildTarget.
package interceptors

import (
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// UpstreamPolicy is a user-request interceptor that applies only to certain
// upstream MCP servers.
type UpstreamPolicy interface {
	proxy.UserRequestInterceptor

	// Match reports whether this policy applies to the upstream URL. It
	// receives the full URL so an implementation can key on any property
	// (host, scheme, path, …), not just the host.
	Match(upstreamURL string) bool
}

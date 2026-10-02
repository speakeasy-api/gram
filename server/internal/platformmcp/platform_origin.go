package platformmcp

import (
	"context"
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// requestPlatformBaseURL returns the first-party platform origin a request
// arrived on: the server URL or an extra platform host (GRAM_PLATFORM_HOSTS),
// as stamped by the custom-domains middleware. It returns nil for any other
// surface or when no origin is stamped, so custom domains, private networks,
// and internal callers keep the configured base URL. The origin never comes
// from the raw Host header, only from the middleware's allow-list.
func requestPlatformBaseURL(ctx context.Context) *url.URL {
	origin, ok := requestorigin.FromContext(ctx)
	if !ok || origin.Surface != requestorigin.SurfacePlatform || origin.BaseURL == "" {
		return nil
	}
	base, err := url.Parse(origin.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil
	}
	return base
}

// platformBaseURL returns the request's platform origin, or fallback when the
// request did not arrive on a platform host.
func platformBaseURL(ctx context.Context, fallback *url.URL) *url.URL {
	if base := requestPlatformBaseURL(ctx); base != nil {
		return base
	}
	return fallback
}

// requestDashboardURL returns the dashboard URL on the extra platform host a
// request arrived on, so links to dashboard pages keep the user's host-only
// session. The canonical host, custom domains, private networks, local dev and
// origin-less requests keep dashboardURL. A nil dashboardURL stays nil.
func requestDashboardURL(ctx context.Context, dashboardURL, serverURL *url.URL) *url.URL {
	if dashboardURL == nil || serverURL == nil {
		return dashboardURL
	}
	base := requestorigin.PlatformHostBaseURL(ctx, serverURL.String(), "")
	if base == "" {
		return dashboardURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return dashboardURL
	}
	return u
}

// platformResource is the Platform MCP resource identifier on base. It is the
// RFC 9728 protected resource, the RFC 8414 issuer, and the RFC 8707 access
// token audience for that origin.
func platformResource(base *url.URL) string {
	return base.JoinPath("platform-mcp").String()
}

// platformProtectedResourceMetadataURL is the RFC 9728 metadata document for
// the Platform MCP resource on base.
func platformProtectedResourceMetadataURL(base *url.URL) string {
	return base.JoinPath(".well-known", "oauth-protected-resource", "platform-mcp").String()
}

package platformmcp

import (
	"context"
	"net/url"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/orghost"
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

// organizationBaseURLs returns the server and dashboard base URLs of links an
// organization's members are given, such as MCP endpoint addresses. The
// dashboard moves an organization's browsers onto the platform host stored as
// its default host, so the addresses the dashboard shows use that host and
// these links do too. Requests on a custom domain or a private network keep
// the configured URLs, because their callers may not reach a platform host.
// Without a resolver or a stored platform host, links follow the request's
// platform origin and then the configured URLs.
// The stored dashboard URL is nil when the stored host is the server host and
// no dashboard URL is configured.
func organizationBaseURLs(ctx context.Context, orgHosts *orghost.Resolver, defaultHost pgtype.Text, serverURL, dashboardURL *url.URL) (*url.URL, *url.URL) {
	if orgHosts != nil && storedHostApplies(ctx) {
		if storedServerURL, storedDashboardURL, ok := orgHosts.StoredPlatformBaseURLs(defaultHost); ok {
			return storedServerURL, storedDashboardURL
		}
	}
	return platformBaseURL(ctx, serverURL), requestDashboardURL(ctx, dashboardURL, serverURL)
}

// storedHostApplies reports whether links built for this request may use an
// organization's stored default host: a platform request or one with no
// origin. Custom-domain and private-network requests keep the configured URLs.
func storedHostApplies(ctx context.Context) bool {
	origin, ok := requestorigin.FromContext(ctx)
	return !ok || origin.Surface == requestorigin.SurfacePlatform
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

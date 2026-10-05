package auth

import (
	"context"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// loginPath is the dashboard login entry point on every platform host.
const loginPath = "/rpc/auth.login"

// orgHostMove names the platform host an organization's browser sessions
// belong on.
type orgHostMove struct {
	// serverURL is the base URL of server routes on that host.
	serverURL *url.URL

	// siteURL is the base URL of the dashboard on that host.
	siteURL *url.URL
}

// organizationHostMove reports where to move a browser that is using an
// organization whose stored default_host is defaultHost. ok is true only when
// the request arrived on a platform host and the stored value names a
// different configured platform host. Session cookies are host-only, so the
// browser has to sign in again there.
//
// A NULL or no-longer-valid stored value never moves the browser: NULL means
// the legacy host, and users of those organizations keep using whichever
// platform host they signed in on. Custom domains and private network ingress
// never move either. Because the target is always a configured platform host
// other than the current one, a move cannot loop or leave the platform.
func (s *Service) organizationHostMove(ctx context.Context, defaultHost pgtype.Text) (orgHostMove, bool) {
	var none orgHostMove
	if s.cfg.OrgHosts == nil {
		return none, false
	}
	origin, ok := requestorigin.FromContext(ctx)
	if !ok || origin.Surface != requestorigin.SurfacePlatform || origin.BaseURL == "" {
		return none, false
	}
	serverURL, siteURL, ok := s.cfg.OrgHosts.StoredPlatformHost(defaultHost)
	if !ok {
		return none, false
	}
	if sameHost(origin.BaseURL, serverURL.Host) {
		return none, false
	}
	return orgHostMove{serverURL: serverURL, siteURL: siteURL}, true
}

// sameHost reports whether baseURL names host. Unparseable values count as the
// same host, so a malformed configuration leaves the browser where it is.
func sameHost(baseURL, host string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return true
	}
	current, err := requestorigin.CanonicalHost(parsed.Host)
	if err != nil {
		return true
	}
	target, err := requestorigin.CanonicalHost(host)
	if err != nil {
		return true
	}
	return current == target
}

// loginURL is the login entry point on the move's host, carrying destination,
// a rooted path from safeRedirectPath, as the post-login redirect.
func (m orgHostMove) loginURL(destination string) string {
	query := url.Values{}
	query.Set("redirect", destination)
	return strings.TrimRight(m.serverURL.String(), "/") + loginPath + "?" + query.Encode()
}

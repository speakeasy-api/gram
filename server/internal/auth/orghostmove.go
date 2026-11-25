package auth

import (
	"context"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// transferInPath moves a session onto every platform host.
const transferInPath = "/rpc/auth.transferIn"

// orgHostMove names the platform host an organization's browser sessions
// belong on.
type orgHostMove struct {
	// serverURL is the base URL of server routes on that host.
	serverURL *url.URL

	// siteURL is the base URL of the dashboard on that host.
	siteURL *url.URL

	// sourceHost is the platform host the request arrived on, which holds
	// the session to hand over.
	sourceHost string
}

// organizationHostMove reports where to move a browser that is using an
// organization whose stored default_host is defaultHost. ok is true only when
// the request arrived on a platform host and the stored value names a
// different configured platform host. Session cookies are host-only, so the
// session is handed to that host with a session transfer.
//
// A NULL or no-longer-valid stored value never moves the browser: NULL means
// the legacy host, and users of those organizations keep using whichever
// platform host they signed in on. Custom domains and private network ingress
// never move either. Because the target is always a configured platform host
// other than the current one, a move cannot loop or leave the platform. The
// target must also be https, or http only when the request itself arrived
// over http (local development), so a move never downgrades a session.
func (s *Service) organizationHostMove(ctx context.Context, defaultHost pgtype.Text) (orgHostMove, bool) {
	var none orgHostMove
	if s.cfg.OrgHosts == nil {
		return none, false
	}
	current, ok := currentPlatformURL(ctx)
	if !ok {
		return none, false
	}
	serverURL, siteURL, ok := s.cfg.OrgHosts.StoredPlatformHost(defaultHost)
	if !ok {
		return none, false
	}
	if !allowedScheme(serverURL.Scheme, current.Scheme) || !allowedScheme(siteURL.Scheme, current.Scheme) {
		return none, false
	}
	if sameHost(current.Host, serverURL.Host) {
		return none, false
	}
	return orgHostMove{serverURL: serverURL, siteURL: siteURL, sourceHost: current.Host}, true
}

// currentPlatformURL is the base URL of the platform host the request arrived
// on. ok is false for custom domains, private network ingress, and requests
// with no usable origin.
func currentPlatformURL(ctx context.Context) (*url.URL, bool) {
	origin, ok := requestorigin.FromContext(ctx)
	if !ok || origin.Surface != requestorigin.SurfacePlatform {
		return nil, false
	}
	current, err := url.Parse(origin.BaseURL)
	if err != nil || current.Host == "" {
		return nil, false
	}
	return current, true
}

// allowedScheme reports whether a move from a request over current may target
// a URL over target: https always, http only from http.
func allowedScheme(target, current string) bool {
	return target == "https" || (target == "http" && current == "http")
}

// sameHost reports whether two hosts are the same. Unparseable values count as
// the same host, so a malformed configuration leaves the browser where it is.
func sameHost(currentHost, targetHost string) bool {
	current, err := requestorigin.CanonicalHost(currentHost)
	if err != nil {
		return true
	}
	target, err := requestorigin.CanonicalHost(targetHost)
	if err != nil {
		return true
	}
	return current == target
}

// transferURL starts a session transfer (TransferIn's start mode) on the
// move's host from the request's host, landing on destination, a rooted path
// from safeRedirectPath.
func (m orgHostMove) transferURL(destination string) string {
	query := url.Values{}
	query.Set("source_host", m.sourceHost)
	query.Set("redirect", destination)
	return strings.TrimRight(m.serverURL.String(), "/") + transferInPath + "?" + query.Encode()
}

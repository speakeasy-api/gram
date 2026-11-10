package mcp

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

const (
	// clientIDNamesEndpointDescription is the invalid_client description for
	// a client_id that names the endpoint itself. The usual cause is a person
	// pasting the server URL into their MCP client's OAuth Client ID setting,
	// so the description tells them where to look.
	clientIDNamesEndpointDescription = "client_id is this MCP server's URL, not an OAuth client ID. Check the OAuth Client ID setting in your MCP client."

	// clientIDNamesEndpointReason is the failure reason logged when a
	// client_id names the endpoint itself, so these rejections can be counted
	// apart from client metadata document fetch failures.
	clientIDNamesEndpointReason = "client_id_names_endpoint"
)

// clientIDNamesEndpoint reports whether a URL-shaped client_id names one of
// the endpoint's own URLs rather than a client metadata document: the
// endpoint root or anything under it, the issuer or anything under it, or the
// endpoint's protected resource or authorization server metadata URL. None of
// these can serve a client metadata document, so fetching one only sends a
// request back to this server that is bound to fail.
//
// baseURL is the origin of the endpoint's resource for the request. An
// endpoint reachable on several hosts matches only the URLs built from it.
func (s *Service) clientIDNamesEndpoint(endpoint *ResolvedMcpEndpoint, baseURL, clientID string) (bool, error) {
	if !cimd.IsClientIDURL(clientID) {
		return false, nil
	}
	presented, err := url.Parse(clientID)
	if err != nil {
		// The CIMD resolver rejects an unparseable client_id with its own
		// error, so there is nothing for this check to add.
		return false, nil
	}

	root, err := endpoint.RootURL(baseURL)
	if err != nil {
		return false, fmt.Errorf("build endpoint root URL: %w", err)
	}
	issuer, err := s.issuerURL(endpoint, baseURL)
	if err != nil {
		return false, fmt.Errorf("build issuer URL: %w", err)
	}
	protectedResource, err := endpoint.ProtectedResourceURL(baseURL)
	if err != nil {
		return false, fmt.Errorf("build protected resource metadata URL: %w", err)
	}
	issuerURL, err := url.Parse(issuer)
	if err != nil {
		return false, fmt.Errorf("parse issuer URL: %w", err)
	}
	// RFC 8414 §3.1 inserts the well-known segment between the issuer's host
	// and its path.
	authorizationServerMetadata := &url.URL{
		Scheme: issuerURL.Scheme,
		Host:   issuerURL.Host,
		Path:   wellknown.OAuthAuthorizationServerPath + strings.TrimSuffix(issuerURL.Path, "/"),
	}

	for _, prefix := range []string{root, issuer} {
		if urlNamesLocation(presented, prefix, true) {
			return true, nil
		}
	}
	for _, exact := range []string{protectedResource, authorizationServerMetadata.String()} {
		if urlNamesLocation(presented, exact, false) {
			return true, nil
		}
	}
	return false, nil
}

// urlNamesLocation reports whether presented addresses the same resource as
// location: the same scheme, the same host and effective port compared
// case-insensitively, and the same path after resolving dot segments and
// dropping a trailing slash. With subpaths, any path under location's path
// matches too, compared by whole segments so `/mcp/a` does not cover
// `/mcp/ab`. Userinfo, query, and fragment are ignored, since a fetch of
// presented would reach location regardless of them.
func urlNamesLocation(presented *url.URL, location string, subpaths bool) bool {
	want, err := url.Parse(location)
	if err != nil {
		return false
	}
	if !strings.EqualFold(presented.Scheme, want.Scheme) {
		return false
	}
	if !strings.EqualFold(normalizedHostPort(presented), normalizedHostPort(want)) {
		return false
	}

	got := cleanURLPath(presented.Path)
	base := cleanURLPath(want.Path)
	if got == base {
		return true
	}
	return subpaths && strings.HasPrefix(got, strings.TrimSuffix(base, "/")+"/")
}

// normalizedHostPort is u's host with any trailing root dot removed and its
// scheme's default port made explicit, so `example.com`, `example.com.`, and
// `example.com:443` compare equal over https.
func normalizedHostPort(u *url.URL) string {
	host := strings.TrimSuffix(u.Hostname(), ".")
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return host + ":" + port
}

// cleanURLPath resolves dot segments and drops a trailing slash, treating an
// empty path as the root.
func cleanURLPath(p string) string {
	if p == "" {
		return "/"
	}
	return path.Clean(p)
}

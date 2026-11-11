package consentpage

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

// HTTP authorities must fit CSP's host-source syntax, without directive delimiters
// or wildcards that could broaden the callback's registered origin.
var cspHTTPAuthority = regexp.MustCompile(`^(?:[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.?|\[[0-9A-Fa-f:.]+\])(?::[0-9]+)?$`)

// FormActionSources permits same-origin forms and the registered OAuth callback.
// Callers must match the callback against the client's registration first.
func FormActionSources(redirectURI string) string {
	sources := "'self'"
	if oauthwire.ValidateRedirectURI(redirectURI) != nil || strings.Contains(redirectURI, "#") {
		return sources
	}
	uri, err := url.Parse(redirectURI)
	if err != nil || uri.User != nil {
		return sources
	}
	if uri.Scheme == "http" || uri.Scheme == "https" {
		if uri.Opaque != "" || uri.Hostname() == "" || !cspHTTPAuthority.MatchString(uri.Host) {
			return sources
		}
		return sources + " " + uri.Scheme + "://" + uri.Host
	}
	// Native clients use a registered custom scheme rather than an HTTP origin.
	return sources + " " + uri.Scheme + ":"
}

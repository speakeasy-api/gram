package consentpage

import (
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

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
		if uri.Opaque != "" || uri.Host == "" {
			return sources
		}
		return sources + " " + uri.Scheme + "://" + uri.Host
	}
	// Native clients use a registered custom scheme rather than an HTTP origin.
	return sources + " " + uri.Scheme + ":"
}

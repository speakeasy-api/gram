package remotesessions

import (
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// remoteSessionProviderEndpoints are a provider's endpoint URLs; a nil or
// empty one is not validated.
type remoteSessionProviderEndpoints struct {
	authorizationEndpoint *string
	tokenEndpoint         *string
	revocationEndpoint    *string
	registrationEndpoint  *string
	jwksURI               *string
	userinfoEndpoint      *string
	introspectionEndpoint *string
}

// validateRemoteSessionProviderURLs validates every URL of a provider being
// created; the issuer is required.
func validateRemoteSessionProviderURLs(issuer string, endpoints remoteSessionProviderEndpoints) error {
	if err := validateRemoteSessionProviderIssuer(issuer); err != nil {
		return err
	}
	return validateRemoteSessionProviderEndpoints(endpoints)
}

// validateRemoteSessionProviderURLChanges validates only the URLs an update
// sets, so a provider stored before a URL rule tightened can still be edited
// without first fixing URLs the update leaves alone.
func validateRemoteSessionProviderURLChanges(issuer *string, endpoints remoteSessionProviderEndpoints) error {
	if issuer != nil {
		if err := validateRemoteSessionProviderIssuer(strings.TrimSpace(*issuer)); err != nil {
			return err
		}
	}
	return validateRemoteSessionProviderEndpoints(endpoints)
}

func validateRemoteSessionProviderIssuer(issuer string) error {
	if _, err := issuerurl.Parse(issuer); err != nil {
		return fmt.Errorf("invalid issuer: %w", err)
	}
	if !urls.IsAbsoluteHTTPSOrLoopback(issuer) {
		return fmt.Errorf("issuer must be an absolute https URL, or http on loopback")
	}
	return nil
}

func validateRemoteSessionProviderEndpoints(provider remoteSessionProviderEndpoints) error {
	for _, endpoint := range []struct {
		name string
		url  *string
	}{
		{name: "authorization_endpoint", url: provider.authorizationEndpoint},
		{name: "token_endpoint", url: provider.tokenEndpoint},
		{name: "revocation_endpoint", url: provider.revocationEndpoint},
		{name: "registration_endpoint", url: provider.registrationEndpoint},
		{name: "jwks_uri", url: provider.jwksURI},
		{name: "userinfo_endpoint", url: provider.userinfoEndpoint},
		{name: "introspection_endpoint", url: provider.introspectionEndpoint},
	} {
		if endpoint.url == nil || *endpoint.url == "" {
			continue
		}
		if !urls.IsAbsoluteHTTPSOrLoopback(*endpoint.url) {
			return fmt.Errorf("%s must be an absolute https URL, or http on loopback", endpoint.name)
		}
		// RFC 6749 §3.1: an endpoint URI must not include a fragment. Unlike an
		// issuer identifier it may carry a query, so only "#" is rejected here.
		// Tested on the raw string for the reason issuerurl.Parse documents:
		// net/url reports no fragment at all for a bare "#", so the delimiter is
		// the dependable signal and an encoded one stays percent-encoded.
		if strings.Contains(*endpoint.url, "#") {
			return fmt.Errorf("%s must not carry a fragment", endpoint.name)
		}
	}

	return nil
}

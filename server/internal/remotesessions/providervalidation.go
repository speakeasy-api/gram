package remotesessions

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

type remoteSessionProviderURLs struct {
	issuer                string
	authorizationEndpoint *string
	tokenEndpoint         *string
	revocationEndpoint    *string
	registrationEndpoint  *string
	jwksURI               *string
	userinfoEndpoint      *string
	introspectionEndpoint *string
}

func validateRemoteSessionProviderURLs(provider remoteSessionProviderURLs) error {
	if _, err := issuerurl.Parse(provider.issuer); err != nil {
		return fmt.Errorf("invalid issuer: %w", err)
	}
	if !urls.IsAbsoluteHTTPSOrLoopback(provider.issuer) {
		return fmt.Errorf("issuer must be an absolute https URL, or http on loopback")
	}

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
		if endpoint.url != nil && *endpoint.url != "" && !urls.IsAbsoluteHTTPSOrLoopback(*endpoint.url) {
			return fmt.Errorf("%s must be an absolute https URL, or http on loopback", endpoint.name)
		}
	}

	return nil
}

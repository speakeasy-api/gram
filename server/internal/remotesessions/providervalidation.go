package remotesessions

import (
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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

func effectiveRemoteSessionProviderURLs(
	current repo.RemoteSessionIssuer,
	issuer *string,
	authorizationEndpoint *string,
	tokenEndpoint *string,
	revocationEndpoint *string,
	registrationEndpoint *string,
	jwksURI *string,
	userinfoEndpoint *string,
	introspectionEndpoint *string,
) remoteSessionProviderURLs {
	value := current.Issuer
	if issuer != nil {
		value = strings.TrimSpace(*issuer)
	}

	return remoteSessionProviderURLs{
		issuer:                value,
		authorizationEndpoint: effectiveRemoteSessionProviderURL(authorizationEndpoint, current.AuthorizationEndpoint.String, current.AuthorizationEndpoint.Valid),
		tokenEndpoint:         effectiveRemoteSessionProviderURL(tokenEndpoint, current.TokenEndpoint.String, current.TokenEndpoint.Valid),
		revocationEndpoint:    effectiveRemoteSessionProviderURL(revocationEndpoint, current.RevocationEndpoint.String, current.RevocationEndpoint.Valid),
		registrationEndpoint:  effectiveRemoteSessionProviderURL(registrationEndpoint, current.RegistrationEndpoint.String, current.RegistrationEndpoint.Valid),
		jwksURI:               effectiveRemoteSessionProviderURL(jwksURI, current.JwksUri.String, current.JwksUri.Valid),
		userinfoEndpoint:      effectiveRemoteSessionProviderURL(userinfoEndpoint, current.UserinfoEndpoint.String, current.UserinfoEndpoint.Valid),
		introspectionEndpoint: effectiveRemoteSessionProviderURL(introspectionEndpoint, current.IntrospectionEndpoint.String, current.IntrospectionEndpoint.Valid),
	}
}

func effectiveRemoteSessionProviderURL(update *string, current string, currentValid bool) *string {
	if update != nil {
		return update
	}
	if !currentValid {
		return nil
	}
	return &current
}

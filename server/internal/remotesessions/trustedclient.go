package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

var requiredIdentityProviderScopes = [...]string{"openid", "email"}

var errTrustedIdentityProviderClientIneligible = errors.New("trusted identity-provider client is ineligible")

// ValidateTrustedIdentityProviderClient checks the stored configuration Gram
// will use as an OAuth client of a trusted identity provider. It deliberately
// validates the client's explicit upstream scope allowlist, not the downstream
// scopes requested by an MCP server.
func ValidateTrustedIdentityProviderClient(client repo.RemoteSessionClient, issuer repo.RemoteSessionIssuer) error {
	effectiveScopes := client.Scope
	if len(issuer.ScopeOverride) > 0 {
		effectiveScopes = issuer.ScopeOverride
	}

	for _, required := range requiredIdentityProviderScopes {
		if !slices.Contains(effectiveScopes, required) {
			return fmt.Errorf("effective client scope must include %q", required)
		}
		if len(issuer.ScopesSupported) > 0 && !slices.Contains(issuer.ScopesSupported, required) {
			return fmt.Errorf("issuer metadata does not advertise required scope %q", required)
		}
	}
	if slices.Contains(effectiveScopes, "offline_access") && len(issuer.ScopesSupported) > 0 && !slices.Contains(issuer.ScopesSupported, "offline_access") {
		return fmt.Errorf("issuer metadata does not advertise requested scope %q", "offline_access")
	}

	secret := ""
	if client.ClientSecretEncrypted.Valid {
		secret = "configured"
	}
	method, err := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String, secret)
	if err != nil {
		return fmt.Errorf("invalid token endpoint authentication: %w", err)
	}
	if method == TokenEndpointAuthMethodNone {
		return fmt.Errorf("token endpoint authentication method %q is not eligible for trusted identity-provider login", method)
	}
	if method == TokenEndpointAuthMethodPrivateKeyJWT && !client.JsonWebKeySetID.Valid {
		return fmt.Errorf("private_key_jwt requires an attached JSON Web Key Set")
	}
	if len(issuer.TokenEndpointAuthMethodsSupported) > 0 && !slices.Contains(issuer.TokenEndpointAuthMethodsSupported, string(method)) {
		return fmt.Errorf("issuer metadata does not advertise token endpoint authentication method %q", method)
	}

	return nil
}

func validateTrustedIdentityProviderIssuerClients(ctx context.Context, q *repo.Queries, issuer repo.RemoteSessionIssuer) error {
	clients, err := q.ListTrustedRemoteSessionClientsByIssuerID(ctx, issuer.ID)
	if err != nil {
		return fmt.Errorf("list trusted identity-provider clients: %w", err)
	}
	for _, client := range clients {
		if err := ValidateTrustedIdentityProviderClient(client, issuer); err != nil {
			return fmt.Errorf("%w: client %s: %w", errTrustedIdentityProviderClientIneligible, client.ID, err)
		}
	}
	return nil
}

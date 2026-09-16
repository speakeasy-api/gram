package remotesessions

import (
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

var requiredIdentityProviderScopes = [...]string{"openid", "email"}

// ValidateTrustedIdentityProviderClient checks the stored configuration Gram
// will use as an OAuth client of a trusted identity provider. It deliberately
// validates the client's explicit upstream scope allowlist, not the downstream
// scopes requested by an MCP server.
func ValidateTrustedIdentityProviderClient(client repo.RemoteSessionClient, issuer repo.RemoteSessionIssuer) error {
	for _, required := range requiredIdentityProviderScopes {
		if !slices.Contains(client.Scope, required) {
			return fmt.Errorf("client scope must include %q", required)
		}
		if len(issuer.ScopesSupported) > 0 && !slices.Contains(issuer.ScopesSupported, required) {
			return fmt.Errorf("issuer metadata does not advertise required scope %q", required)
		}
	}

	secret := ""
	if client.ClientSecretEncrypted.Valid {
		secret = "configured"
	}
	method, err := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String, secret)
	if err != nil {
		return fmt.Errorf("invalid token endpoint authentication: %w", err)
	}
	if method == TokenEndpointAuthMethodPrivateKeyJWT && !client.JsonWebKeySetID.Valid {
		return fmt.Errorf("private_key_jwt requires an attached JSON Web Key Set")
	}
	if len(issuer.TokenEndpointAuthMethodsSupported) > 0 && !slices.Contains(issuer.TokenEndpointAuthMethodsSupported, string(method)) {
		return fmt.Errorf("issuer metadata does not advertise token endpoint authentication method %q", method)
	}

	return nil
}

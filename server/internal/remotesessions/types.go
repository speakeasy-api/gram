package remotesessions

import "fmt"

type TokenEndpointAuthMethod string

const (
	TokenEndpointAuthMethodBasic TokenEndpointAuthMethod = "client_secret_basic"
	TokenEndpointAuthMethodPost  TokenEndpointAuthMethod = "client_secret_post"
	TokenEndpointAuthMethodNone  TokenEndpointAuthMethod = "none"
	// TokenEndpointAuthMethodPrivateKeyJWT is not yet selectable: it is absent
	// from tokenEndpointAuthMethodEnum in the Goa design, so no client can store
	// it and ResolveTokenEndpointAuthMethod below deliberately does not handle
	// it. It exists so the rules that guard the method — a key set is required
	// to declare it, and cannot be detached while it is declared — land before
	// AIM-156 makes it selectable. AIM-156 adds both the enum value and the
	// resolution arm; until then this constant is only ever compared against.
	TokenEndpointAuthMethodPrivateKeyJWT TokenEndpointAuthMethod = "private_key_jwt" //nolint:gosec // G101 false positive: an RFC 7591 token_endpoint_auth_method name, not a credential.
)

// ResolveTokenEndpointAuthMethod maps a client's stored
// token_endpoint_auth_method and its decrypted client secret to the effective
// method for token-endpoint requests. A client that explicitly declares a
// confidential method must carry a secret: failing fast here surfaces the
// misconfiguration instead of sending an unauthenticated request the upstream
// rejects with an opaque 401. A client with no recognized stored method is
// confidential (Basic) when it has a secret and public when it does not; CIMD
// clients store "none" explicitly and never carry a secret (enforced by the
// remote_session_clients client_id_metadata_uri CHECK constraint), so it is
// the absent secret, not method=none, that keeps legacy NULL-method public
// clients off Basic auth.
func ResolveTokenEndpointAuthMethod(stored string, clientSecret string) (TokenEndpointAuthMethod, error) {
	switch TokenEndpointAuthMethod(stored) {
	case TokenEndpointAuthMethodBasic, TokenEndpointAuthMethodPost:
		if clientSecret == "" {
			return "", fmt.Errorf("client declares %s but has no client secret", stored)
		}
		return TokenEndpointAuthMethod(stored), nil
	case TokenEndpointAuthMethodNone:
		return TokenEndpointAuthMethodNone, nil
	default:
		if clientSecret == "" {
			return TokenEndpointAuthMethodNone, nil
		}
		return TokenEndpointAuthMethodBasic, nil
	}
}

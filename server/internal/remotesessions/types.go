package remotesessions

import "fmt"

type TokenEndpointAuthMethod string

const (
	TokenEndpointAuthMethodBasic         TokenEndpointAuthMethod = "client_secret_basic"
	TokenEndpointAuthMethodPost          TokenEndpointAuthMethod = "client_secret_post"
	TokenEndpointAuthMethodNone          TokenEndpointAuthMethod = "none"
	TokenEndpointAuthMethodPrivateKeyJWT TokenEndpointAuthMethod = "private_key_jwt" //nolint:gosec // G101 false positive: an RFC 7591 token_endpoint_auth_method name, not a credential.
)

type TokenEndpointAuthAudienceFormat string

const (
	TokenEndpointAuthAudienceIssuer        TokenEndpointAuthAudienceFormat = "issuer"
	TokenEndpointAuthAudienceTokenEndpoint TokenEndpointAuthAudienceFormat = "token_endpoint"
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
	case TokenEndpointAuthMethodPrivateKeyJWT:
		return TokenEndpointAuthMethodPrivateKeyJWT, nil
	default:
		if stored != "" {
			return "", fmt.Errorf("unknown token endpoint auth method %q", stored)
		}
		if clientSecret == "" {
			return TokenEndpointAuthMethodNone, nil
		}
		return TokenEndpointAuthMethodBasic, nil
	}
}

func ResolveTokenEndpointAuthAudience(format, issuer, tokenEndpoint string) (string, error) {
	switch TokenEndpointAuthAudienceFormat(format) {
	case "", TokenEndpointAuthAudienceIssuer:
		if issuer == "" {
			return "", fmt.Errorf("issuer identifier is empty")
		}
		return issuer, nil
	case TokenEndpointAuthAudienceTokenEndpoint:
		if tokenEndpoint == "" {
			return "", fmt.Errorf("token endpoint is empty")
		}
		return tokenEndpoint, nil
	default:
		return "", fmt.Errorf("unknown token endpoint auth audience format %q", format)
	}
}

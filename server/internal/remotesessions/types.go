package remotesessions

type TokenEndpointAuthMethod string

const (
	TokenEndpointAuthMethodBasic TokenEndpointAuthMethod = "client_secret_basic"
	TokenEndpointAuthMethodPost  TokenEndpointAuthMethod = "client_secret_post"
	TokenEndpointAuthMethodNone  TokenEndpointAuthMethod = "none"
)

func ResolveTokenEndpointAuthMethod(stored string) TokenEndpointAuthMethod {
	switch TokenEndpointAuthMethod(stored) {
	case TokenEndpointAuthMethodPost:
		return TokenEndpointAuthMethodPost
	case TokenEndpointAuthMethodNone:
		return TokenEndpointAuthMethodNone
	default:
		return TokenEndpointAuthMethodBasic
	}
}

package remotesessions

type TokenEndpointAuthMethod string

const (
	TokenEndpointAuthMethodBasic TokenEndpointAuthMethod = "client_secret_basic"
	TokenEndpointAuthMethodPost  TokenEndpointAuthMethod = "client_secret_post"
)

func ResolveTokenEndpointAuthMethod(stored string) TokenEndpointAuthMethod {
	if stored == string(TokenEndpointAuthMethodPost) {
		return TokenEndpointAuthMethodPost
	}
	return TokenEndpointAuthMethodBasic
}

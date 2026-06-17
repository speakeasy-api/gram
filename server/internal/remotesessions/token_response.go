package remotesessions

import "strings"

// tokenResponse is the slice of the upstream /token reply we care about.
// RFC 6749 fields plus the optional refresh_expires_in some providers
// (e.g. Keycloak) include.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	Scope            string `json:"scope"`
}

func (t tokenResponse) Scopes() []string {
	if t.Scope == "" {
		return nil
	}
	return strings.Split(t.Scope, " ")
}

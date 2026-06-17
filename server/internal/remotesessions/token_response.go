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

// Scopes splits the space-delimited scope value per RFC 6749 §3.3, tolerating
// repeated or leading/trailing whitespace: strings.Fields collapses whitespace
// runs and drops empties, so no blank scope is ever persisted.
func (t tokenResponse) Scopes() []string {
	return strings.Fields(t.Scope)
}

package remotesessions

import (
	"strings"
	"time"
)

// tokenResponse is the slice of the upstream /token reply we care about.
// RFC 6749 fields, draft-ietf-oauth-refresh-token-expiration fields, and two
// common non-standard refresh lifetime aliases.
type tokenResponse struct {
	AccessToken            string `json:"access_token"`
	RefreshToken           string `json:"refresh_token"`
	TokenType              string `json:"token_type"`
	ExpiresIn              int    `json:"expires_in"`
	RefreshTokenTimeout    *int64 `json:"refresh_token_timeout"`
	AuthorizationExpiresIn *int64 `json:"authorization_expires_in"`
	RefreshExpiresIn       int64  `json:"refresh_expires_in"`
	RefreshTokenExpiresIn  int64  `json:"refresh_token_expires_in"`
	Scope                  string `json:"scope"`
}

// Scopes splits the space-delimited scope value per RFC 6749 §3.3, tolerating
// repeated or leading/trailing whitespace: strings.Fields collapses whitespace
// runs and drops empties, so no blank scope is ever persisted.
func (t tokenResponse) Scopes() []string {
	return strings.Fields(t.Scope)
}

// RefreshTokenTimeoutSeconds normalizes the standard sliding idle timeout and
// two common provider aliases. The boolean distinguishes an omitted value from
// an explicit zero-second timeout.
func (t tokenResponse) RefreshTokenTimeoutSeconds() (int64, bool) {
	if t.RefreshTokenTimeout != nil {
		return *t.RefreshTokenTimeout, true
	}
	if t.RefreshExpiresIn > 0 {
		return t.RefreshExpiresIn, true
	}
	if t.RefreshTokenExpiresIn > 0 {
		return t.RefreshTokenExpiresIn, true
	}
	return 0, false
}

// AuthorizationLifetimeSeconds is the remaining absolute lifetime of the
// upstream authorization, when the provider reports it.
func (t tokenResponse) AuthorizationLifetimeSeconds() (int64, bool) {
	if t.AuthorizationExpiresIn == nil {
		return 0, false
	}
	return *t.AuthorizationExpiresIn, true
}

func expirationDeadline(now time.Time, seconds int64, reported bool) *time.Time {
	if !reported {
		return nil
	}
	deadline := now.Add(time.Duration(seconds) * time.Second)
	return &deadline
}

// OAuthError is the shared wire-error shape used by the issuer-gated OAuth
// surface (RFC 6749 / RFC 7591 / RFC 7009). The structure is identical across
// endpoints — error code + human-readable description — so the request-type
// Validate methods can return a uniform error and let each handler decide
// how to write it (redirect for /authorize, JSON for /token + /register,
// status 200 for /revoke).

package usersessions

// OAuthError carries an OAuth wire error.
type OAuthError struct {
	Code        string
	Description string
}

func (e *OAuthError) Error() string { return e.Code + ": " + e.Description }

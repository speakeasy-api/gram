package oautherr

// RFC 6750 (Bearer Token Usage) §3.1 defines the error codes a protected
// resource returns in the WWW-Authenticate challenge.
const (
	// CodeInsufficientScope is RFC 6750 §3.1: the request requires higher
	// privileges than provided by the access token.
	CodeInsufficientScope = "insufficient_scope"

	// CodeInvalidToken is RFC 6750 §3.1: the access token provided is expired,
	// revoked, malformed, or invalid for other reasons; the client may request
	// a new access token and retry.
	CodeInvalidToken = "invalid_token"
)

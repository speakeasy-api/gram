package oautherr

// RFC 6749 (The OAuth 2.0 Authorization Framework). §4.1.2.1 defines the
// authorization endpoint error response and §5.2 the token endpoint error
// response.
const (
	// CodeAccessDenied is RFC 6749 §4.1.2.1, reused by RFC 8628 §3.5: the
	// resource owner or authorization server denied the request.
	CodeAccessDenied = "access_denied"

	// CodeInvalidClient is RFC 6749 §5.2: client authentication failed
	// (unknown client, no client authentication included, or unsupported
	// authentication method); answered with HTTP 401 when the client attempted
	// to authenticate via the Authorization request header.
	CodeInvalidClient = "invalid_client"

	// CodeInvalidGrant is RFC 6749 §5.2: the provided authorization grant
	// (authorization code, resource owner credentials) or refresh token is
	// invalid, expired, revoked, does not match the redirection URI used in the
	// authorization request, or was issued to another client.
	CodeInvalidGrant = "invalid_grant"

	// CodeInvalidRequest is RFC 6749 §4.1.2.1 (authorization endpoint) and
	// §5.2 (token endpoint), reused by RFC 6750 §3.1 (protected resource) and
	// RFC 8628 §3.5 (device access token): the request is missing a required
	// parameter, includes an invalid or unsupported parameter value, repeats a
	// parameter, or is otherwise malformed.
	CodeInvalidRequest = "invalid_request"

	// CodeInvalidScope is RFC 6749 §4.1.2.1 and §5.2: the requested scope is
	// invalid, unknown, malformed, or exceeds the scope granted by the resource
	// owner.
	CodeInvalidScope = "invalid_scope"

	// CodeServerError is RFC 6749 §4.1.2.1: the authorization server
	// encountered an unexpected condition that prevented it from fulfilling the
	// request (the redirect-delivered equivalent of HTTP 500).
	CodeServerError = "server_error"

	// CodeTemporarilyUnavailable is RFC 6749 §4.1.2.1: the authorization server
	// is currently unable to handle the request due to temporary overloading or
	// maintenance (the redirect-delivered equivalent of HTTP 503).
	CodeTemporarilyUnavailable = "temporarily_unavailable"

	// CodeUnauthorizedClient is RFC 6749 §4.1.2.1 and §5.2: the authenticated
	// client is not authorized to request an authorization code or to use the
	// presented authorization grant type.
	CodeUnauthorizedClient = "unauthorized_client"

	// CodeUnsupportedGrantType is RFC 6749 §5.2: the authorization grant type
	// is not supported by the authorization server.
	CodeUnsupportedGrantType = "unsupported_grant_type"

	// CodeUnsupportedResponseType is RFC 6749 §4.1.2.1: the authorization
	// server does not support obtaining an authorization code (or token) using
	// the requested response_type.
	CodeUnsupportedResponseType = "unsupported_response_type"
)

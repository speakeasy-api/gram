package oautherr

// RFC 8628 (Device Authorization Grant) §3.5 defines the token endpoint error
// codes a polling device client receives.
const (
	// CodeAuthorizationPending is RFC 8628 §3.5: the authorization request is
	// still pending because the end user has not yet completed the
	// user-interaction steps; the client should poll again after the interval.
	CodeAuthorizationPending = "authorization_pending"

	// CodeExpiredToken is RFC 8628 §3.5: the device_code has expired and the
	// device authorization session has concluded; the client may start a new
	// device authorization request.
	CodeExpiredToken = "expired_token"

	// CodeSlowDown is RFC 8628 §3.5: a variant of authorization_pending
	// signalling that the client is polling too frequently and must increase
	// its polling interval by 5 seconds for this and all subsequent requests.
	CodeSlowDown = "slow_down"
)

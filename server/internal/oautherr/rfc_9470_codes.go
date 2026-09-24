package oautherr

// RFC 9470 (Step Up Authentication Challenge Protocol) §3 defines the
// protected resource challenge code.
const (
	// CodeInsufficientUserAuthentication is RFC 9470 §3: the authentication
	// event associated with the access token does not meet the authentication
	// requirements of the protected resource.
	CodeInsufficientUserAuthentication = "insufficient_user_authentication"
)

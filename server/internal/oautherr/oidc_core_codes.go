package oautherr

// OpenID Connect Core 1.0 section 3.1.2.6 error codes. Some providers also send
// them from the token endpoint when a refresh needs the person to sign in.
const (
	// OIDCCodeInteractionRequired means the provider needs the person to interact.
	OIDCCodeInteractionRequired = "interaction_required"

	// OIDCCodeLoginRequired means the provider needs the person to sign in.
	OIDCCodeLoginRequired = "login_required"

	// OIDCCodeConsentRequired means the provider needs the person's consent.
	OIDCCodeConsentRequired = "consent_required"

	// OIDCCodeAccountSelectionRequired means the person must choose an account.
	OIDCCodeAccountSelectionRequired = "account_selection_required"

	// OIDCCodeInvalidRequestURI means the request_uri is invalid.
	OIDCCodeInvalidRequestURI = "invalid_request_uri"

	// OIDCCodeInvalidRequestObject means the request object is invalid.
	OIDCCodeInvalidRequestObject = "invalid_request_object"

	// OIDCCodeRequestNotSupported means the provider doesn't support the request parameter.
	OIDCCodeRequestNotSupported = "request_not_supported"

	// OIDCCodeRequestURINotSupported means the provider doesn't support the request_uri parameter.
	OIDCCodeRequestURINotSupported = "request_uri_not_supported"

	// OIDCCodeRegistrationNotSupported means the provider doesn't support the registration parameter.
	OIDCCodeRegistrationNotSupported = "registration_not_supported"
)

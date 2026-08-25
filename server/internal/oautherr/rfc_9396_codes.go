package oautherr

// RFC 9396 (Rich Authorization Requests) §5 defines the error code for the
// authorization_details parameter at both the authorization and token
// endpoints.
const (
	// CodeInvalidAuthorizationDetails is RFC 9396 §5: an authorization details
	// object has an unknown type, contains unknown fields for a known type,
	// has fields of the wrong type or with invalid values, or is missing
	// required fields.
	CodeInvalidAuthorizationDetails = "invalid_authorization_details"
)

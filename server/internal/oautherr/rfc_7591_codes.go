package oautherr

// RFC 7591 (Dynamic Client Registration) §3.2.2 defines the registration
// endpoint error codes.
const (
	// CodeInvalidClientMetadata is RFC 7591 §3.2.2: the value of one of the
	// client metadata fields is invalid and the server has rejected the
	// request.
	CodeInvalidClientMetadata = "invalid_client_metadata"

	// CodeInvalidRedirectURI is RFC 7591 §3.2.2: the value of one or more
	// redirection URIs is invalid.
	CodeInvalidRedirectURI = "invalid_redirect_uri"

	// CodeInvalidSoftwareStatement is RFC 7591 §3.2.2: the software statement
	// presented is invalid.
	CodeInvalidSoftwareStatement = "invalid_software_statement"

	// CodeUnapprovedSoftwareStatement is RFC 7591 §3.2.2: the software
	// statement presented is not approved for use by this authorization
	// server.
	CodeUnapprovedSoftwareStatement = "unapproved_software_statement"
)

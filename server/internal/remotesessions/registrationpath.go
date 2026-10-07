package remotesessions

// RegistrationPath is how a client is obtained from an authorization server
// without credentials entered by hand.
type RegistrationPath string

const (
	// RegistrationPathManual means neither automatic path applies, so a
	// client must be configured by hand.
	RegistrationPathManual RegistrationPath = "manual"

	// RegistrationPathDCR is RFC 7591 dynamic client registration.
	RegistrationPathDCR RegistrationPath = "dynamic_client_registration"

	// RegistrationPathCIMD is a Gram-hosted Client ID Metadata Document.
	RegistrationPathCIMD RegistrationPath = "client_id_metadata_document"
)

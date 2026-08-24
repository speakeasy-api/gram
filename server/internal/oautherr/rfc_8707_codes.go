package oautherr

// RFC 8707 (Resource Indicators) §2 defines the error code for the resource
// parameter.
const (
	// CodeInvalidTarget is RFC 8707 §2: the requested resource is invalid,
	// missing, unknown, or malformed.
	CodeInvalidTarget = "invalid_target"
)

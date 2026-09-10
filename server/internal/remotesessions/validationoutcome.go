package remotesessions

// ValidationOutcome is the closed set remote_sessions.validation_status stores.
type ValidationOutcome string

const (
	// ValidationOutcomeValid: the upstream accepted the credential and listed its tools.
	ValidationOutcomeValid ValidationOutcome = "valid"

	// ValidationOutcomeRejectedByMember: the upstream answered 401 or 403.
	ValidationOutcomeRejectedByMember ValidationOutcome = "rejected_by_member"

	// ValidationOutcomeUnknown: no verdict (transport failure, timeout, non-auth status); never replaces valid.
	ValidationOutcomeUnknown ValidationOutcome = "unknown"

	// ValidationOutcomeRevoked is reserved for provider-side revocation; nothing writes it yet.
	ValidationOutcomeRevoked ValidationOutcome = "revoked"
)

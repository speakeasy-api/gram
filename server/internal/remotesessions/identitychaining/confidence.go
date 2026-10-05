package identitychaining

// Confidence says how strongly the evidence supports an outcome's reason.
type Confidence string

const (
	// ConfidenceVerified: Gram checked the fact itself.
	ConfidenceVerified Confidence = "verified"

	// ConfidenceInferred: a specific provider error code implies it.
	ConfidenceInferred Confidence = "inferred"

	// ConfidenceUnknown: the provider's answer is ambiguous.
	ConfidenceUnknown Confidence = "unknown"
)

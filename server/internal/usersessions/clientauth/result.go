package clientauth

import "time"

// Result describes an accepted assertion, for the caller's telemetry.
type Result struct {
	// Audience is which of the accepted audience values the client sent.
	// Logged because the client cannot discover our preference, so only
	// production traffic can show what implementations actually do.
	Audience AudienceKind

	// ExpiresAt is the assertion's exp. Logged for the same reason as
	// Audience: the lifetime ceiling was chosen from what implementations
	// are documented to emit, and observed lifetimes are the evidence for
	// keeping or tightening it.
	ExpiresAt time.Time
}

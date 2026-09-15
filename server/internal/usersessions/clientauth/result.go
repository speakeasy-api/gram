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

	// ReusedAssertion reports that this exact assertion had already been
	// presented inside its validity window, and was accepted anyway.
	//
	// Only ever true on a profile whose replay identifier is derived from
	// the assertion's own bytes, where a repeat means the same token
	// arrived twice rather than a second token reusing an identifier. Some
	// platforms serve one cached token for most of its lifetime, so this is
	// expected traffic rather than an attack — but it is the evidence for
	// whether that tolerance can later be tightened, which is why it is
	// reported rather than swallowed.
	ReusedAssertion bool
}

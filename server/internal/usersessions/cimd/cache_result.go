package cimd

import "time"

// CacheResult carries the effect of one Resolve call.
type CacheResult struct {
	// Outcome selects which of the remaining fields are meaningful; callers
	// must switch on it rather than testing fields for emptiness.
	Outcome CacheOutcome

	// Document is the freshly fetched and validated document. It is non-nil
	// if and only if Outcome is CacheOutcomeRefreshed — on the other two
	// outcomes the caller's own stored row is the authoritative copy and
	// nothing was parsed.
	Document *Document

	// ETag is the validator to persist. Empty means the upstream offers no
	// validator and the next refresh must be unconditional; the caller
	// should store NULL rather than an empty string.
	ETag string

	// TTL is the lifetime to add to the stored expiry, already clamped to
	// the resolver's bounds. It is zero on CacheOutcomeCached, where the
	// existing expiry stands.
	TTL time.Duration
}

package jwks

import (
	"encoding/json"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// Result carries the effect of one Resolve call.
type Result struct {
	// Outcome selects what the caller must persist and whether the upstream
	// was consulted.
	Outcome CacheOutcome

	// KeySet is the parsed, screened verification key set — the keys
	// selectKey chooses from. Populated on every successful outcome.
	KeySet jose.JSONWebKeySet

	// Document is the raw bytes backing KeySet: the fetched body on
	// CacheOutcomeRefreshed, the caller's stored bytes otherwise. Persist it
	// on CacheOutcomeRefreshed.
	Document json.RawMessage

	// ETag is the validator to persist. Empty means the upstream offers no
	// validator and the next refresh must be unconditional.
	ETag string

	// TTL is the freshness lifetime to persist, already clamped to this
	// package's bounds. It is zero on CacheOutcomeCached and
	// CacheOutcomeInline, where nothing about the stored freshness changes.
	TTL time.Duration
}

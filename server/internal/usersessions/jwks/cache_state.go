package jwks

import (
	"encoding/json"
	"time"
)

// CacheState is the stored key set state a caller has persisted for one key
// source. The zero value means "nothing cached" and forces an unconditional
// fetch.
type CacheState struct {
	// Document is the raw JWK Set bytes as last fetched. It is re-parsed and
	// re-screened on every use rather than stored pre-parsed, so a
	// tightening of the screening rules applies to cached material
	// immediately instead of waiting out the TTL.
	Document json.RawMessage

	// ETag is the stored validator, replayed verbatim in If-None-Match.
	// Empty means the next refresh is unconditional.
	ETag string

	// ExpiresAt bounds the entry's freshness. While it is in the future the
	// document is served without any upstream request.
	ExpiresAt time.Time

	// RefreshedAt is when the upstream was last actually consulted (a 200 or
	// a 304), as opposed to served from this state. The KeyResolver's
	// unknown-kid path reads it as a negative-cache signal: a set the
	// upstream confirmed moments ago does not contain the kid, so probing
	// again would only spend refresh budget.
	RefreshedAt time.Time
}

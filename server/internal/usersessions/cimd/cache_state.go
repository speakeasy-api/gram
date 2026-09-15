package cimd

import "time"

// CacheState is the cached metadata the caller has persisted for a client_id.
// The zero value means "nothing cached" and forces an unconditional fetch.
type CacheState struct {
	// ExpiresAt is the stored client_id_metadata_cache_expires_at. While it
	// is in the future the document is served from the caller's row with no
	// upstream request at all.
	ExpiresAt time.Time

	// ETag is the stored client_id_metadata_etag, replayed verbatim in
	// If-None-Match. Empty means the refresh is unconditional.
	ETag string
}

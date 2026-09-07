package cimd

import (
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/httpcache"
)

const (
	// defaultCacheTTL applies when the document response carries no usable
	// freshness header. Draft -02 §5.2 lets the server pick; an hour keeps a
	// rotated document from lingering while still collapsing the repeated
	// authorize legs a single client makes in a session.
	defaultCacheTTL = time.Hour

	// minCacheTTL and maxCacheTTL bound whatever the upstream asks for. The
	// floor stops a hostile or misconfigured host from forcing a fetch on
	// every authorize (the request is unauthenticated, so the fetch is
	// attacker-triggerable); the ceiling stops it from pinning a document —
	// and therefore a redirect_uris set — indefinitely. -02 §5.2 explicitly
	// permits both bounds, and the 24h ceiling matches the MCP
	// specification's recommended maximum.
	minCacheTTL = 5 * time.Minute
	maxCacheTTL = 24 * time.Hour

	// maxETagLength caps the validator persisted in
	// client_id_metadata_etag and echoed back in If-None-Match. The value is
	// chosen by the document host, and an unbounded ETag is a write
	// amplification primitive against a TEXT column.
	maxETagLength = 256
)

// cacheTTL derives the lifetime of a freshly fetched document from its
// response headers via the shared RFC 9111 freshness parsing in httpcache,
// bounded by this package's TTL constants. The shared policy's deliberate
// no-store / no-cache deviation is correct here: a Client ID Metadata
// Document is public by definition (-02 §4.1 bans every secret from it), so
// the directives carry no confidentiality meaning, and -02 §5.2 permits the
// server-chosen bounds.
func cacheTTL(header http.Header, now time.Time) time.Duration {
	policy := httpcache.FreshnessPolicy{
		Default: defaultCacheTTL,
		Min:     minCacheTTL,
		Max:     maxCacheTTL,
	}
	return policy.TTL(header, now)
}

// sanitizeETag returns the entity tag to persist and replay in If-None-Match,
// or "" when the response carries nothing usable, capped at maxETagLength.
func sanitizeETag(raw string) string {
	return httpcache.SanitizeETag(raw, maxETagLength)
}

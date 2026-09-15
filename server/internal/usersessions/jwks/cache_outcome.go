package jwks

// CacheOutcome reports which branch of the cache policy a Resolve call took,
// and therefore what the caller must persist.
type CacheOutcome string

const (
	// CacheOutcomeInline means the source was an inline key set: nothing was
	// fetched and there is nothing to persist.
	CacheOutcomeInline CacheOutcome = "inline"

	// CacheOutcomeCached means the stored document was still fresh: no
	// request was made and the caller must write nothing.
	CacheOutcomeCached CacheOutcome = "cached"

	// CacheOutcomeNotModified means the upstream answered 304 to a
	// conditional request: the stored document stands, and the caller must
	// persist the refreshed ETag, TTL, and refresh instant.
	CacheOutcomeNotModified CacheOutcome = "not_modified"

	// CacheOutcomeRefreshed means a new document was fetched and screened:
	// the caller must replace the stored document along with the ETag, TTL,
	// and refresh instant.
	CacheOutcomeRefreshed CacheOutcome = "refreshed"
)

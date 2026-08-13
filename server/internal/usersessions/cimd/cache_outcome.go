package cimd

// CacheOutcome reports which branch of the cache policy a Resolve call took,
// and therefore what the caller must persist.
type CacheOutcome string

const (
	// CacheOutcomeCached means the stored document was still fresh: no request
	// was made and the caller must write nothing.
	CacheOutcomeCached CacheOutcome = "cached"

	// CacheOutcomeNotModified means the upstream answered 304 to a conditional
	// request: the caller's stored client_name and redirect_uris remain
	// correct, and it must persist only the refreshed ETag and TTL.
	CacheOutcomeNotModified CacheOutcome = "not_modified"

	// CacheOutcomeRefreshed means a new document was fetched and validated: the
	// caller must replace the stored metadata along with the ETag and TTL.
	CacheOutcomeRefreshed CacheOutcome = "refreshed"
)

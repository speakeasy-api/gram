package jwks

import (
	"errors"
	"time"
)

const (
	// maxKeySetBytes is the read cap on a fetched key set body. Deliberately
	// far above the cimd document cap: real-world key sets embed x5c
	// certificate chains (Microsoft Entra ID's runs tens of kilobytes), so a
	// mirrored 5 KB cap would be an interop bug against major identity
	// providers. Hitting the cap is a fetch failure.
	maxKeySetBytes = 256 * 1024

	// fetchTimeout bounds a single key set fetch, applied per attempt on top
	// of the caller's context.
	fetchTimeout = 10 * time.Second

	// defaultCacheTTL applies when the key set response carries no usable
	// freshness header.
	defaultCacheTTL = time.Hour

	// minCacheTTL and maxCacheTTL bound whatever the upstream asks for. The
	// floor stops a hostile or misconfigured host from forcing a fetch per
	// verification (assertions arrive on an unauthenticated surface, so the
	// fetch is attacker-triggerable); the ceiling stops a stale key set from
	// being pinned indefinitely.
	minCacheTTL = 5 * time.Minute
	maxCacheTTL = 24 * time.Hour

	// maxETagLength caps the validator persisted for a key source and echoed
	// back in If-None-Match.
	maxETagLength = 256
)

// ErrKeySetTooLarge marks the one fetch failure that arrives with a
// successful HTTP status: the response began, but the body ran past
// maxKeySetBytes.
var ErrKeySetTooLarge = errors.New("key set exceeds size limit")

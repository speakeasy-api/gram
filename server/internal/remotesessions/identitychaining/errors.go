package identitychaining

import "errors"

// errStale marks a publish that current provenance no longer admits.
var errStale = errors.New("identity chaining provenance changed during acquisition")

// errConsentNotStored means a consent credential was not stored because a
// grant's or another provider's credential holds the legacy one-per-subject
// slot. The exchange still succeeded; the grant mints on first use.
var errConsentNotStored = errors.New("identity chaining consent credential not stored")

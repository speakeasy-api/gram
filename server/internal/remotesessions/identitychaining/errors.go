package identitychaining

import "errors"

// errStale marks a publish that current provenance no longer admits.
var errStale = errors.New("identity chaining provenance changed during acquisition")

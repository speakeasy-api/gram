package background

import "errors"

// ErrTemporalUnavailable reports that a Temporal-backed operation cannot run
// because no Temporal environment is configured in this process.
var ErrTemporalUnavailable = errors.New("temporal environment is not configured")

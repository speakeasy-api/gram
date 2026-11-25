package temporal

import "errors"

// ErrNotConfigured reports that a Temporal-backed operation cannot run because
// no Temporal environment is configured in this process. Serving tiers that
// intentionally run without Temporal (for example the MCP and OAuth tier)
// return this error instead of dereferencing a nil environment.
var ErrNotConfigured = errors.New("temporal environment is not configured")

// Package telemetryerrs holds telemetry errors that non-telemetry packages
// need to recognize (e.g. the assistant logs platform tool detects the "logs
// disabled" gate to surface an actionable hint to the model). Kept in a leaf
// package so callers can import it without picking up the full telemetry
// service and its auth/sessions transitive deps.
package telemetryerrs

import "errors"

// ErrLogsDisabled is returned (wrapped in oops.E) by every telemetry handler
// gated on the org-level logging feature flag. Use errors.Is to detect.
var ErrLogsDisabled = errors.New("logs are not enabled for this organization")
